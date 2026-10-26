package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func deletionInput(t *testing.T, s *Store, actor, id string) MemberDeletionInput {
	t.Helper()
	v, err := s.MemberDeletionState(actor, id)
	must(t, err)
	return MemberDeletionInput{Revision: v.Revision, Username: v.Username, Note: "管理员核实后处理账号", Key: randomID()}
}

func TestMemberDeletionPreservesHistoryTreeAndCredentials(t *testing.T) {
	s, root := setup(t)
	u := register(t, s, "deletion_member", root.InviteCode)
	child := register(t, s, "deletion_child", u.InviteCode)
	var tokens []string
	var childToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 2; i++ {
			token, e := newLogin(tx, u.ID)
			if e != nil {
				return e
			}
			tokens = append(tokens, token)
		}
		var e error
		childToken, e = newLogin(tx, child.ID)
		if e != nil {
			return e
		}
		if _, e = tx.CreateBucketIfNotExists([]byte(loginChallengeBucket)); e != nil {
			return e
		}
		if e = put(tx, loginChallengeBucket, "delete-challenge", loginSecurityPending{UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}); e != nil {
			return e
		}
		if e = put(tx, "ledger", "history-ledger", Ledger{ID: "history-ledger", UserID: u.ID, Kind: "deposit_approved", Amount: 100, Note: "历史记录"}); e != nil {
			return e
		}
		if e = put(tx, "orders", "history-order", Order{ID: "history-order", UserID: u.ID, Username: u.Username, Status: "settled", Cost: 100, InviterID: root.ID}); e != nil {
			return e
		}
		if e = put(tx, "requests", "history-request", WalletRequest{ID: "history-request", UserID: u.ID, Username: u.Username, Kind: "deposit", Status: "approved", Amount: 100}); e != nil {
			return e
		}
		return put(tx, "commissions", "history-commission", Commission{ID: "history-commission", UserID: u.ID, Status: "claimed", Amount: 1})
	}))
	before := markSixCombinedStoredState(t, s)
	original := managementStored(t, s, u.ID)
	in := deletionInput(t, s, root.ID, u.ID)
	state, e := s.MemberDeletionState(root.ID, u.ID)
	must(t, e)
	if !state.CanDelete || state.DirectCount != 1 || state.Deleted {
		t.Fatal(state)
	}
	result, e := s.DeleteMember(root.ID, u.ID, in)
	must(t, e)
	if !result.Deleted || result.Status != "disabled" {
		t.Fatal(result)
	}
	after := markSixCombinedStoredState(t, s)
	for bucket, contents := range before {
		switch bucket {
		case "users", "logins", loginChallengeBucket, "keys", "audit":
			continue
		}
		if !reflect.DeepEqual(contents, after[bucket]) {
			t.Fatal("deletion changed historical bucket", bucket)
		}
	}
	for id, raw := range before["users"] {
		if id != u.ID && !reflect.DeepEqual(raw, after["users"][id]) {
			t.Fatal("deletion changed another user", id)
		}
	}
	deleted := managementStored(t, s, u.ID)
	if !deleted.Deleted || deleted.Status != "disabled" || deleted.DeletionRevision == "" {
		t.Fatal(deleted.User)
	}
	deleted.Deleted = original.Deleted
	deleted.Status = original.Status
	deleted.DeletionRevision = original.DeletionRevision
	if deleted != original {
		t.Fatal("deletion changed credentials or financial fields")
	}
	for _, token := range tokens {
		if _, e = s.Authenticate(token); e == nil {
			t.Fatal("session survived deletion")
		}
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		challenge, e := get[loginSecurityPending](tx, loginChallengeBucket, "delete-challenge")
		if e != nil {
			return e
		}
		if !challenge.Invalidated {
			t.Fatal("pending login challenge survived deletion")
		}
		return nil
	}))
	if _, e = s.Authenticate(childToken); e != nil {
		t.Fatal("child session revoked", e)
	}
	if _, _, e = s.Login(u.Username, testPassword); e == nil {
		t.Fatal("deleted member logged in")
	}
	if e = s.SetStatus(root.ID, u.ID, "active"); e == nil {
		t.Fatal("ordinary status endpoint revived deleted member")
	}
	if _, e = s.MemberPasswordState(root.ID, u.ID); e == nil {
		t.Fatal("deleted account accepted password action")
	}
	if _, e = s.MemberInviter(root.ID, u.ID); e == nil {
		t.Fatal("deleted account accepted inviter action")
	}
	if e = s.AdjustWithKind(root.ID, u.ID, 100, "不能调整已删除账号", randomID(), "deposit"); e == nil {
		t.Fatal("deleted account credited")
	}
	w, e := s.Wagering(root.ID, u.ID)
	must(t, e)
	if _, e = s.EditWagering(root.ID, u.ID, WagerEdit{Revision: w.Revision, Version: w.Version, Required: wagerValue(0), Note: "不能调整已删除账号", Key: randomID()}); e == nil {
		t.Fatal("deleted account wagering changed")
	}
	for _, args := range [][2]string{{u.Username, ""}, {"new_invited_member", u.InviteCode}} {
		e = s.db.Update(func(tx *bolt.Tx) error {
			_, err := createUser(tx, args[0], "test-hash", "test-salt", args[1], "user", "", "")
			return err
		})
		if e == nil {
			t.Fatal("deleted username or invite was reused", args)
		}
	}
	normal, e := s.AdminData(root.ID, "users", u.Username, 1)
	must(t, e)
	if normal["total"] != 0 {
		t.Fatal("deleted member visible in normal list", normal)
	}
	archived, e := s.AdminData(root.ID, "deleted-users", u.Username, 1)
	must(t, e)
	if archived["total"] != 1 {
		t.Fatal(archived)
	}
	raw, _ := json.Marshal(archived)
	for _, secret := range []string{original.Hash, original.PasswordSalt, "DeletionRevision"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private credential leaked")
		}
	}
	tree, e := s.AgentTree(root.ID, u.Username, "", 1)
	must(t, e)
	if tree.Items[0].Status != "deleted" || tree.Items[0].DirectCount != 1 {
		t.Fatal("tree lost archived parent", tree)
	}
	if managementStored(t, s, child.ID).InviterID != u.ID {
		t.Fatal("child silently reparented")
	}
	for _, resource := range []string{"ledger", "orders", "requests", "commissions"} {
		if _, e = s.AdminData(root.ID, resource, "", 1); e != nil {
			t.Fatal("history lookup broken", resource, e)
		}
	}
	stable := markSixCombinedStoredState(t, s)
	_, e = s.DeleteMember(root.ID, u.ID, in)
	must(t, e)
	if !reflect.DeepEqual(stable, markSixCombinedStoredState(t, s)) {
		t.Fatal("retry reapplied deletion")
	}
	in.Note = "同编号更改原因"
	if _, e = s.DeleteMember(root.ID, u.ID, in); e == nil {
		t.Fatal("changed body reused deletion key")
	}
	path := s.db.Path()
	must(t, s.Close())
	next, e := Open(path)
	must(t, e)
	t.Cleanup(func() { next.Close() })
	if !managementStored(t, next, u.ID).Deleted {
		t.Fatal("restart lost deleted flag")
	}
}

func TestMemberDeletionBlockersAndStalePreview(t *testing.T) {
	for _, kind := range []string{"balance", "negative-balance", "frozen", "pending-order", "pending-request", "available-commission"} {
		t.Run(kind, func(t *testing.T) {
			s, root := setup(t)
			u := register(t, s, "guard_member", "")
			old := deletionInput(t, s, root.ID, u.ID)
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				switch kind {
				case "balance", "negative-balance", "frozen":
					x, e := get[storedUser](tx, "users", u.ID)
					if e != nil {
						return e
					}
					if kind == "frozen" {
						x.Frozen = 100
					} else if kind == "negative-balance" {
						x.Balance = -1
					} else {
						x.Balance = 100
					}
					return put(tx, "users", u.ID, x)
				case "pending-order":
					return put(tx, "orders", "pending", Order{ID: "pending", UserID: u.ID, Status: "pending", Cost: 0})
				case "pending-request":
					return put(tx, "requests", "pending", WalletRequest{ID: "pending", UserID: u.ID, Kind: "deposit", Status: "pending"})
				default:
					return put(tx, "commissions", "available", Commission{ID: "available", UserID: u.ID, Status: "available", Amount: 1})
				}
			}))
			before := markSixCombinedStoredState(t, s)
			if _, e := s.DeleteMember(root.ID, u.ID, old); e == nil {
				t.Fatal("stale preview accepted")
			}
			v, e := s.MemberDeletionState(root.ID, u.ID)
			must(t, e)
			if v.CanDelete || len(v.Blockers) == 0 {
				t.Fatal(v)
			}
			if _, e = s.DeleteMember(root.ID, u.ID, deletionInput(t, s, root.ID, u.ID)); e == nil {
				t.Fatal("blocked account deleted")
			}
			if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
				t.Fatal("rejected operation changed records")
			}
		})
	}
}

func TestMemberDeletionRestoreAndABAPreview(t *testing.T) {
	s, root := setup(t)
	u := register(t, s, "restore_member", "")
	_, token, e := s.Login(u.Username, testPassword)
	must(t, e)
	old := deletionInput(t, s, root.ID, u.ID)
	_, e = s.DeleteMember(root.ID, u.ID, old)
	must(t, e)
	in := deletionInput(t, s, root.ID, u.ID)
	_, e = s.RestoreMember(root.ID, u.ID, in)
	must(t, e)
	x := managementStored(t, s, u.ID)
	if x.Deleted || x.Status != "disabled" {
		t.Fatal("restore activated or failed", x.User)
	}
	if _, _, e = s.Login(u.Username, testPassword); e == nil {
		t.Fatal("restore implicitly activated account")
	}
	if _, e = s.Authenticate(token); e == nil {
		t.Fatal("restore revived an old session")
	}
	stable := markSixCombinedStoredState(t, s)
	_, e = s.RestoreMember(root.ID, u.ID, in)
	must(t, e)
	if !reflect.DeepEqual(stable, markSixCombinedStoredState(t, s)) {
		t.Fatal("restore retry wrote again")
	}
	must(t, s.SetStatus(root.ID, u.ID, "active"))
	if _, _, e = s.Login(u.Username, testPassword); e != nil {
		t.Fatal("explicit reactivation failed", e)
	}
	newPreview, e := s.MemberDeletionState(root.ID, u.ID)
	must(t, e)
	if newPreview.Revision == old.Revision {
		t.Fatal("ABA deletion revision reused")
	}
	stale := old
	stale.Key = randomID()
	if _, e = s.DeleteMember(root.ID, u.ID, stale); e == nil {
		t.Fatal("old pre-delete preview revived after restore")
	}
	stable = markSixCombinedStoredState(t, s)
	_, e = s.DeleteMember(root.ID, u.ID, old)
	must(t, e)
	if !reflect.DeepEqual(stable, markSixCombinedStoredState(t, s)) || managementStored(t, s, u.ID).Deleted {
		t.Fatal("old receipt deleted restored account")
	}
	// Existing order snapshots may generate a later commission. Recovery must
	// remain available even when deletion blockers would now prohibit deletion.
	_, e = s.DeleteMember(root.ID, u.ID, deletionInput(t, s, root.ID, u.ID))
	must(t, e)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "commissions", "later", Commission{ID: "later", UserID: u.ID, Status: "available", Amount: 100})
	}))
	in = deletionInput(t, s, root.ID, u.ID)
	_, e = s.RestoreMember(root.ID, u.ID, in)
	must(t, e)
	x = managementStored(t, s, u.ID)
	if x.Deleted || x.Status != "disabled" {
		t.Fatal(x.User)
	}
}

func TestMemberDeletionValidationPermissionsAndConcurrentWrites(t *testing.T) {
	s, root := setup(t)
	u := register(t, s, "race_delete_member", "")
	in := deletionInput(t, s, root.ID, u.ID)
	for _, change := range []func(*MemberDeletionInput){func(v *MemberDeletionInput) { v.Username = "wrong" }, func(v *MemberDeletionInput) { v.Username = " " + v.Username }, func(v *MemberDeletionInput) { v.Username += " " }, func(v *MemberDeletionInput) { v.Note = " " }, func(v *MemberDeletionInput) { v.Key = "bad" }, func(v *MemberDeletionInput) { v.Revision = "" }} {
		bad := in
		change(&bad)
		before := markSixCombinedStoredState(t, s)
		if _, e := s.DeleteMember(root.ID, u.ID, bad); e == nil {
			t.Fatal("invalid input accepted")
		}
		if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("invalid request wrote data")
		}
	}
	if _, e := s.MemberDeletionState(u.ID, u.ID); e != ErrForbidden {
		t.Fatal("member preview allowed", e)
	}
	if _, e := s.DeleteMember(u.ID, u.ID, in); e != ErrForbidden {
		t.Fatal("member deleted self", e)
	}
	if _, e := s.MemberDeletionState(root.ID, root.ID); e == nil {
		t.Fatal("administrator target allowed")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "users", "other-admin", storedUser{User: User{ID: "other-admin", Username: "other_admin", Role: "admin", Status: "active"}})
	}))
	if _, e := s.MemberDeletionState(root.ID, "other-admin"); e == nil {
		t.Fatal("another administrator target allowed")
	}
	if _, e := s.MemberDeletionState(root.ID, "missing"); e != ErrNotFound {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := in
			copy.Key = randomID()
			_, e := s.DeleteMember(root.ID, u.ID, copy)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("concurrent deletes both succeeded", success)
	}
	_, e := s.RestoreMember(root.ID, u.ID, deletionInput(t, s, root.ID, u.ID))
	must(t, e)
	must(t, s.SetStatus(root.ID, u.ID, "active"))
	in = deletionInput(t, s, root.ID, u.ID)
	errList := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, e := s.DeleteMember(root.ID, u.ID, in); errList <- e }()
	go func() {
		defer wg.Done()
		errList <- s.AdjustWithKind(root.ID, u.ID, 100, "并发充值与删除", randomID(), "deposit")
	}()
	wg.Wait()
	close(errList)
	success = 0
	for e := range errList {
		if e == nil {
			success++
		}
	}
	x := managementStored(t, s, u.ID)
	if success != 1 || (x.Deleted && x.Balance != 0) || (!x.Deleted && x.Balance != 100) {
		t.Fatal("concurrent credit lost or credited deleted user", success, x.User)
	}
}

func TestMemberDeletionHTTPAuthorizationConfirmationAndRecovery(t *testing.T) {
	s, root := setup(t)
	u := register(t, s, "http_delete_member", "")
	var adminToken, memberToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var e error
		adminToken, e = newLogin(tx, root.ID)
		if e != nil {
			return e
		}
		memberToken, e = newLogin(tx, u.ID)
		return e
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, token string, body any, origin string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://localhost/api/v1/admin/"+path, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	preview := "users/" + u.ID + "/deletion"
	target := "users/" + u.ID
	for _, tc := range []struct {
		path, token string
		code        int
	}{{preview, "", 401}, {preview, memberToken, 403}, {"deleted-users", memberToken, 403}, {"deleted-users", adminToken, 200}, {preview, adminToken, 200}, {"users/missing/deletion", adminToken, 404}} {
		w := call("GET", tc.path, tc.token, nil, "")
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	in := deletionInput(t, s, root.ID, u.ID)
	for _, tc := range []struct {
		token, origin string
		code          int
	}{{"", "", 401}, {memberToken, "", 403}, {adminToken, "https://other.invalid", 403}} {
		w := call("DELETE", target, tc.token, in, tc.origin)
		if w.Code != tc.code {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	w := call("DELETE", target, adminToken, in, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("GET", "deleted-users", adminToken, nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	in = deletionInput(t, s, root.ID, u.ID)
	w = call("POST", target+"/restore", adminToken, in, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if x := managementStored(t, s, u.ID); x.Deleted || x.Status != "disabled" {
		t.Fatal(fmt.Sprint(x.User))
	}
}
