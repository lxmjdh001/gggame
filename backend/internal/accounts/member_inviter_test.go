package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestMemberInviterMovesWholeBranchAndSynchronizesTeams(t *testing.T) {
	s := agentFixture(t)
	before := markSixCombinedStoredState(t, s)
	state, e := s.PreviewMemberInviter("admin", "c", MemberInviterInput{InviterUsername: "SIBLING"})
	must(t, e)
	if state.InviterUsername != "b" || state.NewInviterUsername != "sibling" || state.Descendants != 1 {
		t.Fatal(state)
	}
	in := MemberInviterInput{InviterUsername: "sibling", Revision: state.Revision, Note: "调整团队归属", Key: randomID()}
	changed, e := s.ChangeMemberInviter("admin", "c", in)
	must(t, e)
	if changed.InviterUsername != "sibling" {
		t.Fatal(changed)
	}
	_, e = s.ChangeMemberInviter("admin", "c", in)
	must(t, e)
	old, e := s.AgentTree("admin", "a", "", 1)
	must(t, e)
	next, e := s.AgentTree("admin", "sibling", "", 1)
	must(t, e)
	if old.Items[0].TeamCount != 1 || old.Items[0].TeamDeposit != 20000 || next.Items[0].TeamCount != 2 || next.Items[0].TeamDeposit != 70000 {
		t.Fatal(old, next)
	}
	a, e := s.Account("a")
	must(t, e)
	b, e := s.Account("sibling")
	must(t, e)
	if a["team_counts"].([3]int) != ([3]int{1, 0, 0}) || b["team_counts"].([3]int) != ([3]int{1, 1, 0}) {
		t.Fatal(a["team_counts"], b["team_counts"])
	}
	after := markSixCombinedStoredState(t, s)
	for bucket, data := range before {
		if bucket != "users" && bucket != "audit" && bucket != "keys" && !reflect.DeepEqual(data, after[bucket]) {
			t.Fatal("unrelated bucket changed", bucket)
		}
	}
	for id, raw := range before["users"] {
		var u storedUser
		must(t, json.Unmarshal(raw, &u))
		if id == "c" {
			u.InviterID = "sibling"
		}
		var got storedUser
		must(t, json.Unmarshal(after["users"][id], &got))
		if !reflect.DeepEqual(u, got) {
			t.Fatal("other user fields changed", id)
		}
	}
	state, e = s.PreviewMemberInviter("admin", "c", MemberInviterInput{Detach: true})
	must(t, e)
	_, e = s.ChangeMemberInviter("admin", "c", MemberInviterInput{Detach: true, Revision: state.Revision, Note: "设为顶层团队", Key: randomID()})
	must(t, e)
	got, e := s.MemberInviter("admin", "c")
	must(t, e)
	if got.InviterID != "" || got.Descendants != 1 {
		t.Fatal(got)
	}
}

func TestMemberInviterRejectsCyclesStaleAndUnauthorizedChanges(t *testing.T) {
	s := agentFixture(t)
	for _, name := range []string{"a", "b", "c", "d", "missing"} {
		if _, e := s.PreviewMemberInviter("admin", "a", MemberInviterInput{InviterUsername: name}); e == nil {
			t.Fatal("invalid parent accepted", name)
		}
	}
	if _, e := s.MemberInviter("a", "c"); e != ErrForbidden {
		t.Fatal(e)
	}
	if _, e := s.MemberInviter("admin", "admin"); e == nil {
		t.Fatal("admin can be moved")
	}
	state, e := s.PreviewMemberInviter("admin", "c", MemberInviterInput{InviterUsername: "sibling"})
	must(t, e)
	must(t, s.SetStatus("admin", "sibling", "disabled"))
	if _, e = s.ChangeMemberInviter("admin", "c", MemberInviterInput{InviterUsername: "sibling", Revision: state.Revision, Note: "旧预览不可覆盖", Key: randomID()}); e == nil {
		t.Fatal("disabled/stale parent accepted")
	}
	must(t, s.SetStatus("admin", "sibling", "active"))
	other, e := s.PreviewMemberInviter("admin", "sibling", MemberInviterInput{InviterUsername: "d"})
	must(t, e)
	_, e = s.ChangeMemberInviter("admin", "sibling", MemberInviterInput{InviterUsername: "d", Revision: other.Revision, Note: "先发生关系变化", Key: randomID()})
	must(t, e)
	if _, e = s.ChangeMemberInviter("admin", "c", MemberInviterInput{InviterUsername: "sibling", Revision: state.Revision, Note: "不能形成新循环", Key: randomID()}); e == nil {
		t.Fatal("cycle created by concurrent move")
	}
}

func TestMemberInviterPreservesOldOrderCommissionSnapshot(t *testing.T) {
	s, admin := setup(t)
	old := register(t, s, "old_parent", "")
	next := register(t, s, "new_parent", "")
	member := register(t, s, "move_child", old.InviteCode)
	must(t, s.Adjust(admin.ID, member.ID, 100000, "测试资金", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	cfg.CommissionBPS = [3]int64{500, 0, 0}
	must(t, s.SaveSettings(admin.ID, cfg))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	input := marketInput(now, "p1.big")
	order, e := s.PlaceMarketOrder(member.ID, input, now)
	must(t, e)
	state, e := s.PreviewMemberInviter(admin.ID, member.ID, MemberInviterInput{InviterUsername: next.Username})
	must(t, e)
	_, e = s.ChangeMemberInviter(admin.ID, member.ID, MemberInviterInput{InviterUsername: next.Username, Revision: state.Revision, Note: "订单快照保持", Key: randomID()})
	must(t, e)
	kept, e := s.Order(order.ID)
	must(t, e)
	if !reflect.DeepEqual(order, kept) {
		t.Fatal("historical order modified")
	}
	must(t, s.SettleMarket(order.ID, marketRound(input, []int{9, 1, 2, 3, 4})))
	oldAccount, e := s.Account(old.ID)
	must(t, e)
	newAccount, e := s.Account(next.ID)
	must(t, e)
	if oldAccount["commission_available_cents"].(int64) != 500 || newAccount["commission_available_cents"].(int64) != 0 {
		t.Fatal("accepted order commission redirected")
	}
	input.Key = randomID()
	fresh, e := s.PlaceMarketOrder(member.ID, input, now)
	must(t, e)
	if fresh.InviterID != next.ID || len(fresh.Ancestors) == 0 || fresh.Ancestors[0] != next.ID {
		t.Fatal("new order kept old parent", fresh.Ancestors)
	}
}

func TestMemberInviterHTTPPermissionsPreviewAndCommit(t *testing.T) {
	s := agentFixture(t)
	var adminToken, userToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var e error
		adminToken, e = newLogin(tx, "admin")
		if e != nil {
			return e
		}
		userToken, e = newLogin(tx, "a")
		return e
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost/api/v1/admin/users/c/"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {userToken, 403}, {adminToken, 200}} {
		w := request("GET", "inviter", tc.token, "", "")
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body)
		}
	}
	w := request("POST", "inviter/preview", adminToken, `{"inviter_username":"sibling"}`, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var preview struct {
		Data MemberInviterState `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &preview))
	body, _ := json.Marshal(MemberInviterInput{InviterUsername: "sibling", Revision: preview.Data.Revision, Note: "已核实分组变更", Key: randomID()})
	if w = request("POST", "inviter", adminToken, string(body), "https://foreign.invalid"); w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if w = request("POST", "inviter", adminToken, string(body), ""); w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal(w.Code, w.Body)
	}
}
