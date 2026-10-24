package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func managementStored(t *testing.T, s *Store, id string) storedUser {
	t.Helper()
	var u storedUser
	must(t, s.db.View(func(tx *bolt.Tx) error { var e error; u, e = get[storedUser](tx, "users", id); return e }))
	return u
}
func managementFinancialSnapshot(t *testing.T, s *Store) string {
	t.Helper()
	all := map[string]map[string]string{}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, name := range []string{"ledger", "orders", "requests", "commissions", "settings", "keys"} {
			all[name] = map[string]string{}
			if e := tx.Bucket([]byte(name)).ForEach(func(k, v []byte) error { all[name][string(k)] = string(v); return nil }); e != nil {
				return e
			}
		}
		return nil
	}))
	raw, e := json.Marshal(all)
	must(t, e)
	return string(raw)
}
func TestMemberPasswordResetAuthorizationValidationAndRevocation(t *testing.T) {
	s, adminUser := setup(t)
	u := register(t, s, "reset_member", "")
	other := register(t, s, "reset_other", "")
	_, token1, e := s.Login(u.Username, testPassword)
	must(t, e)
	_, token2, e := s.Login(u.Username, testPassword)
	must(t, e)
	_, otherToken, e := s.Login(other.Username, testPassword)
	must(t, e)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		v, e := get[storedUser](tx, "users", u.ID)
		if e != nil {
			return e
		}
		v.WithdrawalHash = "preserved-pin-hash"
		v.WithdrawalSalt = "preserved-pin-salt"
		v.WithdrawalPasswordSet = true
		if e = put(tx, "users", u.ID, v); e != nil {
			return e
		}
		if _, e = tx.CreateBucketIfNotExists([]byte(loginChallengeBucket)); e != nil {
			return e
		}
		return put(tx, loginChallengeBucket, "old-challenge", loginSecurityPending{UserID: u.ID, ExpiresAt: time.Now().Add(time.Minute)})
	}))
	before := managementStored(t, s, u.ID)
	financial := managementFinancialSnapshot(t, s)
	state, e := s.MemberPasswordState(adminUser.ID, u.ID)
	must(t, e)
	valid := MemberPasswordInput{Revision: state.Revision, Password: "New-pass-12345", Confirmation: "New-pass-12345", Note: "客户申请重设登录密码"}
	if _, e = s.MemberPasswordState(u.ID, other.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("member preview allowed", e)
	}
	if e = s.ResetMemberPassword(u.ID, other.ID, valid); !errors.Is(e, ErrForbidden) {
		t.Fatal("member reset allowed", e)
	}
	if _, e = s.MemberPasswordState(adminUser.ID, adminUser.ID); e == nil {
		t.Fatal("admin password target allowed")
	}
	for _, change := range []func(*MemberPasswordInput){
		func(v *MemberPasswordInput) { v.Password = "lowercase-only"; v.Confirmation = v.Password }, func(v *MemberPasswordInput) { v.Password = "UPPERCASE-ONLY"; v.Confirmation = v.Password },
		func(v *MemberPasswordInput) { v.Password = "ShortAa"; v.Confirmation = v.Password }, func(v *MemberPasswordInput) { v.Confirmation = "Different-password" },
		func(v *MemberPasswordInput) { v.Note = " " }, func(v *MemberPasswordInput) { v.Revision = "" }, func(v *MemberPasswordInput) { v.Revision = "stale" },
	} {
		in := valid
		change(&in)
		if e = s.ResetMemberPassword(adminUser.ID, u.ID, in); e == nil {
			t.Fatal("invalid reset accepted")
		}
		if managementStored(t, s, u.ID) != before {
			t.Fatal("failed reset changed member")
		}
	}
	must(t, s.ResetMemberPassword(adminUser.ID, u.ID, valid))
	after := managementStored(t, s, u.ID)
	if after.Hash == before.Hash || after.PasswordSalt == before.PasswordSalt || after.Hash == valid.Password {
		t.Fatal("password not securely replaced")
	}
	restored := after
	restored.Hash = before.Hash
	restored.PasswordSalt = before.PasswordSalt
	if restored != before {
		t.Fatal("reset changed other member fields")
	}
	for _, token := range []string{token1, token2} {
		if _, e = s.Authenticate(token); e == nil {
			t.Fatal("old session retained")
		}
	}
	if _, e = s.Authenticate(otherToken); e != nil {
		t.Fatal("unrelated session revoked", e)
	}
	if _, _, e = s.Login(u.Username, testPassword); e == nil {
		t.Fatal("old password valid")
	}
	if _, _, e = s.Login(u.Username, valid.Password); e != nil {
		t.Fatal("new password invalid", e)
	}
	if e = s.ResetMemberPassword(adminUser.ID, u.ID, valid); e == nil {
		t.Fatal("stale reset replayed")
	}
	if financial != managementFinancialSnapshot(t, s) {
		t.Fatal("financial data changed")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		pending, e := get[loginSecurityPending](tx, loginChallengeBucket, "old-challenge")
		if e != nil {
			return e
		}
		if !pending.Invalidated {
			t.Fatal("old challenge retained")
		}
		audits, e := list[Audit](tx, "audit")
		if e != nil {
			return e
		}
		found := false
		for _, a := range audits {
			if a.Action == "user.password.reset" {
				found = true
				if a.Actor != adminUser.ID || a.Target != u.ID || a.Detail != valid.Note {
					t.Fatal("audit incorrect")
				}
			}
			raw, _ := json.Marshal(a)
			if strings.Contains(string(raw), valid.Password) || strings.Contains(string(raw), after.Hash) {
				t.Fatal("audit leaks password")
			}
		}
		if !found {
			t.Fatal("missing audit")
		}
		return nil
	}))
}
func TestMemberPasswordResetConcurrentAndDisabled(t *testing.T) {
	s, a := setup(t)
	u := register(t, s, "reset_race", "")
	state, e := s.MemberPasswordState(a.ID, u.ID)
	must(t, e)
	in := MemberPasswordInput{state.Revision, "New-pass-12345", "New-pass-12345", "并发操作测试"}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.ResetMemberPassword(a.ID, u.ID, in) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("concurrent stale reset accepted", successes.Load())
	}
	must(t, s.SetStatus(a.ID, u.ID, "disabled"))
	state, e = s.MemberPasswordState(a.ID, u.ID)
	must(t, e)
	in.Revision = state.Revision
	in.Password = "Other-pass-12345"
	in.Confirmation = in.Password
	must(t, s.ResetMemberPassword(a.ID, u.ID, in))
	if managementStored(t, s, u.ID).Status != "disabled" {
		t.Fatal("reset reactivated member")
	}
	if _, _, e = s.Login(u.Username, in.Password); !errors.Is(e, ErrForbidden) {
		t.Fatal("disabled member logged in", e)
	}
}
func TestDisableDescendantsAllDepthsIsolationAndStalePreview(t *testing.T) {
	s, a := setup(t)
	root := register(t, s, "team_root", "")
	sibling := register(t, s, "outside_team", "")
	child := register(t, s, "team_child", root.InviteCode)
	last := child
	team := []User{child}
	for i := 0; i < 4; i++ {
		last = register(t, s, fmt.Sprintf("team_depth_%d", i), last.InviteCode)
		team = append(team, last)
	}
	must(t, s.SetStatus(a.ID, child.ID, "disabled"))
	_, rootToken, e := s.Login(root.Username, testPassword)
	must(t, e)
	_, siblingToken, e := s.Login(sibling.Username, testPassword)
	must(t, e)
	_, teamToken, e := s.Login(last.Username, testPassword)
	must(t, e)
	// Add more than a management page and the account's 200-record display bound without costly test password hashing.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		template, e := get[storedUser](tx, "users", last.ID)
		if e != nil {
			return e
		}
		for i := 0; i < 205; i++ {
			v := template
			v.ID = fmt.Sprintf("bulk-member-%03d", i)
			v.Username = v.ID
			v.InviterID = last.ID
			if e = put(tx, "users", v.ID, v); e != nil {
				return e
			}
		}
		return nil
	}))
	financial := managementFinancialSnapshot(t, s)
	rootBefore := managementStored(t, s, root.ID)
	state, e := s.Descendants(a.ID, root.ID)
	must(t, e)
	if state.Total != 210 || state.Active != 209 || state.Disabled != 1 {
		t.Fatal("missing descendants", state)
	}
	if _, e = s.Descendants(root.ID, root.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("member preview allowed", e)
	}
	if _, e = s.DisableDescendants(root.ID, root.ID, DisableDescendantsInput{state.Revision, "越权操作"}); !errors.Is(e, ErrForbidden) {
		t.Fatal("member bulk allowed", e)
	}
	newcomer := register(t, s, "new_team_member", root.InviteCode)
	if _, e = s.DisableDescendants(a.ID, root.ID, DisableDescendantsInput{state.Revision, "人数过期确认"}); e == nil {
		t.Fatal("stale team accepted")
	}
	if managementStored(t, s, last.ID).Status != "active" {
		t.Fatal("stale request partially applied")
	}
	state, e = s.Descendants(a.ID, root.ID)
	must(t, e)
	if _, e = s.DisableDescendants(a.ID, root.ID, DisableDescendantsInput{state.Revision, " "}); e == nil {
		t.Fatal("missing reason accepted")
	}
	result, e := s.DisableDescendants(a.ID, root.ID, DisableDescendantsInput{state.Revision, "核对后停用全部下级"})
	must(t, e)
	if result.Active != 210 {
		t.Fatal("wrong changed count", result)
	}
	after, e := s.Descendants(a.ID, root.ID)
	must(t, e)
	if after.Active != 0 || after.Disabled != 211 {
		t.Fatal("not all descendants disabled", after)
	}
	for _, u := range append(team, newcomer) {
		if managementStored(t, s, u.ID).Status != "disabled" {
			t.Fatal("descendant left active")
		}
	}
	if managementStored(t, s, root.ID) != rootBefore || managementStored(t, s, sibling.ID).Status != "active" {
		t.Fatal("root or sibling changed")
	}
	if _, e = s.Authenticate(rootToken); e != nil {
		t.Fatal("root session revoked", e)
	}
	if _, e = s.Authenticate(siblingToken); e != nil {
		t.Fatal("sibling revoked", e)
	}
	if _, e = s.Authenticate(teamToken); e == nil {
		t.Fatal("child session usable")
	}
	must(t, s.SetStatus(a.ID, last.ID, "active"))
	if _, e = s.Authenticate(teamToken); e == nil {
		t.Fatal("old session survived restore")
	}
	if financial != managementFinancialSnapshot(t, s) {
		t.Fatal("bulk changed financial data")
	}
}
func TestDisableDescendantsRollbackAndCycle(t *testing.T) {
	s, a := setup(t)
	root := register(t, s, "rollback_root", "")
	child := register(t, s, "rollback_child", root.InviteCode)
	state, e := s.Descendants(a.ID, root.ID)
	must(t, e)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("logins")).Put([]byte("malformed"), []byte("bad-json"))
	}))
	if _, e = s.DisableDescendants(a.ID, root.ID, DisableDescendantsInput{state.Revision, "事务回滚测试"}); e == nil {
		t.Fatal("malformed session ignored")
	}
	if managementStored(t, s, child.ID).Status != "active" {
		t.Fatal("failure partially disabled team")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", root.ID)
		if e != nil {
			return e
		}
		u.InviterID = child.ID
		return put(tx, "users", u.ID, u)
	}))
	if _, e = s.Descendants(a.ID, root.ID); e == nil {
		t.Fatal("invitation cycle accepted")
	}
}
func TestMemberManagementHTTPAuthorizationAndPrivacy(t *testing.T) {
	s, a := setup(t)
	root := register(t, s, "http_team", "")
	register(t, s, "http_child", root.InviteCode)
	_, adminToken, e := s.Login(a.Username, testPassword)
	must(t, e)
	_, memberToken, e := s.Login(root.Username, testPassword)
	must(t, e)
	mux := http.NewServeMux()
	h := HTTP{Store: s}
	h.Bind(mux)
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, suffix := range []string{"password", "descendants", "disable-descendants"} {
		method := "GET"
		body := ""
		if suffix == "disable-descendants" {
			method = "POST"
			body = "{}"
		}
		path := "/api/v1/admin/users/" + root.ID + "/" + suffix
		for _, tc := range []struct {
			token  string
			status int
		}{{"", 401}, {memberToken, 403}} {
			w := call(method, path, body, tc.token)
			if w.Code != tc.status {
				t.Fatal("unauthorized management", suffix, w.Code, w.Body.String())
			}
		}
	}
	for _, suffix := range []string{"password", "descendants"} {
		w := call("GET", "/api/v1/admin/users/"+root.ID+"/"+suffix, "", adminToken)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		assertNoSecuritySecrets(t, w.Body.Bytes(), testPassword, managementStored(t, s, root.ID).Hash)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("management data cached")
		}
	}
	state, e := s.MemberPasswordState(a.ID, root.ID)
	must(t, e)
	body, _ := json.Marshal(MemberPasswordInput{state.Revision, "HTTP-pass-12345", "HTTP-pass-12345", "会员请求修改"})
	if w := call("POST", "/api/v1/admin/users/"+root.ID+"/password", string(body), adminToken); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state2, e := s.Descendants(a.ID, root.ID)
	must(t, e)
	body, _ = json.Marshal(DisableDescendantsInput{state2.Revision, "核对会员关系后停用"})
	if w := call("POST", "/api/v1/admin/users/"+root.ID+"/disable-descendants", string(body), adminToken); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("POST", "/api/v1/admin/users/"+a.ID+"/password", "{}", adminToken); w.Code == 200 {
		t.Fatal("admin reset accepted")
	}
}

func TestDisableDescendantsConcurrentAndAdministratorProtection(t *testing.T) {
	s, a := setup(t)
	root := register(t, s, "protected_root", "")
	child := register(t, s, "protected_child", root.InviteCode)
	protected := register(t, s, "protected_admin", root.InviteCode)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", protected.ID)
		if e != nil {
			return e
		}
		u.Role = "admin"
		return put(tx, "users", u.ID, u)
	}))
	state, e := s.Descendants(a.ID, root.ID)
	must(t, e)
	if state.Total != 1 || state.Active != 1 {
		t.Fatal("administrator included", state)
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.DisableDescendants(a.ID, root.ID, DisableDescendantsInput{state.Revision, "并发停用核对"}); e == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 || managementStored(t, s, child.ID).Status != "disabled" || managementStored(t, s, protected.ID).Status != "active" {
		t.Fatal("concurrent disable or admin protection failed")
	}
	if _, e = s.Descendants(a.ID, protected.ID); e == nil {
		t.Fatal("administrator root allowed")
	}
	if _, e = s.MemberPasswordState(a.ID, "missing-member"); !errors.Is(e, ErrNotFound) {
		t.Fatal("missing target not rejected", e)
	}
	leaf, e := s.Descendants(a.ID, child.ID)
	must(t, e)
	if leaf.Total != 0 {
		t.Fatal("leaf has descendants")
	}
	if _, e = s.DisableDescendants(a.ID, child.ID, DisableDescendantsInput{leaf.Revision, "无下级测试"}); e == nil {
		t.Fatal("empty team accepted")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", protected.ID)
		if e != nil {
			return e
		}
		u.Status = "disabled"
		return put(tx, "users", u.ID, u)
	}))
	if _, e = s.Descendants(protected.ID, root.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("disabled administrator allowed", e)
	}
	if _, e = s.MemberPasswordState(protected.ID, root.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("disabled administrator password preview allowed", e)
	}
}
