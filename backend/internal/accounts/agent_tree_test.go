package accounts

import (
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func agentFixture(t *testing.T) *Store {
	t.Helper()
	s, _, _ := fundsFixture(t)
	t.Cleanup(func() { s.Close() })
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, x := range []struct{ id, parent, status string }{{"a", "admin", "active"}, {"b", "a", "disabled"}, {"c", "b", "active"}, {"d", "c", "active"}, {"sibling", "admin", "active"}, {"orphan", "missing", "active"}} {
			u := storedUser{User: User{ID: x.id, Username: x.id, Name: "姓名" + x.id, Role: "user", Status: x.status, InviterID: x.parent, CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, Hash: "SECRET-HASH", PasswordSalt: "SECRET-SALT", SecondaryHash: "SECRET-SECONDARY"}
			if e := put(tx, "users", x.id, u); e != nil {
				return e
			}
		}
		for i, l := range []Ledger{{UserID: "a", Kind: "admin_deposit", Amount: 10000}, {UserID: "b", Kind: "deposit_approved", Amount: 20000}, {UserID: "c", Kind: "admin_adjust", Amount: 30000, Note: "充值"}, {UserID: "d", Kind: "admin_deposit", Amount: 40000}, {UserID: "a", Kind: "admin_reward", Amount: 99999}, {UserID: "b", Kind: "admin_deposit", Amount: -1000}} {
			if e := put(tx, "ledger", fmt.Sprint(i), l); e != nil {
				return e
			}
		}
		return nil
	}))
	return s
}

func TestAgentTreeAllDepthTotalsPrivacyAndReadOnly(t *testing.T) {
	s := agentFixture(t)
	before := markSixCombinedStoredState(t, s)
	p, e := s.AgentTree("admin", "A", "", 1)
	must(t, e)
	if len(p.Items) != 1 {
		t.Fatal(p)
	}
	a := p.Items[0]
	if a.DirectCount != 1 || a.TeamCount != 3 || a.Deposit != 10000 || a.TeamDeposit != 90000 || a.InviterUsername != "admin" {
		t.Fatal("wrong hierarchy or money totals", a)
	}
	p, e = s.AgentTree("admin", "", "b", 1)
	must(t, e)
	if p.Parent == nil || p.Parent.Status != "disabled" || p.Total != 1 || p.Items[0].Username != "c" {
		t.Fatal("disabled parent branch omitted", p)
	}
	p, e = s.AgentTree("admin", "orphan", "", 1)
	must(t, e)
	if !p.Items[0].MissingParent {
		t.Fatal("missing parent not indicated")
	}
	raw, e := json.Marshal(p)
	must(t, e)
	for _, secret := range []string{"SECRET", "Hash", "Salt", "phone", "wechat", "password"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("sensitive fields leaked", secret)
		}
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("tree read changed data")
	}
}

func TestAgentTreePaginationAndExactLookup(t *testing.T) {
	s := agentFixture(t)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 47; i++ {
			id := fmt.Sprintf("child%02d", i)
			if e := put(tx, "users", id, storedUser{User: User{ID: id, Username: id, Role: "user", Status: "active", InviterID: "a"}}); e != nil {
				return e
			}
		}
		return nil
	}))
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		p, e := s.AgentTree("admin", "", "a", page)
		must(t, e)
		if p.Total != 48 || p.Page != page || len(p.Items) > 20 {
			t.Fatal(p)
		}
		for _, n := range p.Items {
			if seen[n.ID] {
				t.Fatal("duplicate child")
			}
			seen[n.ID] = true
		}
	}
	if len(seen) != 48 {
		t.Fatal("children truncated")
	}
	p, e := s.AgentTree("admin", "", "a", 99)
	must(t, e)
	if p.Page != 3 {
		t.Fatal("page not clamped")
	}
	if _, e = s.AgentTree("admin", "child", "", 1); e != ErrNotFound {
		t.Fatal("lookup unexpectedly used partial match", e)
	}
	if _, e = s.AgentTree("admin", "a", "admin", 1); e == nil {
		t.Fatal("ambiguous query accepted")
	}
}

func TestAgentTreeDetectsCycles(t *testing.T) {
	s := agentFixture(t)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "a")
		if e != nil {
			return e
		}
		u.InviterID = "d"
		return put(tx, "users", u.ID, u)
	}))
	if _, e := s.AgentTree("admin", "", "", 1); e == nil || !strings.Contains(e.Error(), "循环") {
		t.Fatal("cycle did not fail clearly", e)
	}
}

func TestAgentTreeHTTPRequiresAdministrator(t *testing.T) {
	s := agentFixture(t)
	tokens := map[string]string{}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range []string{"admin", "member"} {
			token, e := newLogin(tx, id)
			if e != nil {
				return e
			}
			tokens[id] = token
		}
		return nil
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	for _, tc := range []struct {
		token, path string
		code        int
	}{{"", "/api/v1/admin/agents", 401}, {tokens["member"], "/api/v1/admin/agents?username=a", 403}, {tokens["admin"], "/api/v1/admin/agents?username=A", 200}, {tokens["admin"], "/api/v1/admin/agents?page=0", 400}, {tokens["admin"], "/api/v1/admin/agents?parent_id=missing", 404}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private tree response was cacheable")
		}
	}
}
