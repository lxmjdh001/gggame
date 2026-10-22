package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func seedOrderPages(t *testing.T, s *Store, count int) {
	t.Helper()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 1; i <= count; i++ {
			for _, owner := range []string{"member", "admin"} {
				o := Order{ID: seq(tx, "orders"), UserID: owner, Username: owner, Issue: fmt.Sprint(i), Status: "pending"}
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

func TestOrderPagesHistoryAndIsolation(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	empty, err := s.OrdersPage("member", 99)
	must(t, err)
	if empty.Page != 1 || empty.Total != 0 || empty.Items == nil || empty.PageSize != 10 {
		t.Fatal("invalid empty page", empty)
	}
	seedOrderPages(t, s, 205)
	seen := map[string]bool{}
	for page := 1; page <= 21; page++ {
		data, err := s.OrdersPage("member", page)
		must(t, err)
		want := 10
		if page == 21 {
			want = 5
		}
		if data.Page != page || data.Total != 205 || data.PageSize != 10 || len(data.Items) != want {
			t.Fatal("incorrect page", data)
		}
		for i, o := range data.Items {
			if o.UserID != "member" || seen[o.ID] || o.Issue != fmt.Sprint(205-(page-1)*10-i) {
				t.Fatal("leaked, duplicated or misordered record", o)
			}
			seen[o.ID] = true
		}
	}
	if len(seen) != 205 {
		t.Fatal("old orders inaccessible")
	}
	for _, tc := range []struct{ requested, expected int }{{0, 1}, {-1, 1}, {1000000, 21}} {
		data, err := s.OrdersPage("member", tc.requested)
		must(t, err)
		if data.Page != tc.expected {
			t.Fatal("out of range page not clamped")
		}
	}
	if _, err := s.OrdersPage("missing", 1); err == nil {
		t.Fatal("unknown user accepted")
	}
	// The administrator paginates after applying the search, with ten orders per page.
	for _, tc := range []struct {
		query                        string
		page, total, current, length int
	}{
		{"", 1, 410, 1, 10}, {"member", 2, 205, 2, 10}, {"member", 99, 205, 21, 5}, {"no_match", 99, 0, 1, 0},
	} {
		data, err := s.AdminData("admin", "orders", tc.query, tc.page)
		must(t, err)
		if data["total"] != tc.total || data["page"] != tc.current || data["page_size"] != 10 || len(data["items"].([]any)) != tc.length {
			t.Fatal("wrong admin order page", data)
		}
		if tc.query == "member" {
			for _, raw := range data["items"].([]any) {
				if raw.(Order).UserID != "member" {
					t.Fatal("search not applied before pagination")
				}
			}
		}
	}
	users, err := s.AdminData("admin", "users", "", 1)
	must(t, err)
	if users["page_size"] != 20 {
		t.Fatal("unrelated table size changed")
	}
	if _, err := s.AdminData("member", "orders", "", 1); err != ErrForbidden {
		t.Fatal("member can access admin orders")
	}
}

func TestOrderPagesHTTP(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	seedOrderPages(t, s, 11)
	var memberToken, adminToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var err error
		memberToken, err = newLogin(tx, "member")
		if err != nil {
			return err
		}
		adminToken, err = newLogin(tx, "admin")
		return err
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	for _, tc := range []struct {
		path, token                 string
		status, page, total, length int
	}{
		{"/orders", "", 401, 0, 0, 0},
		{"/orders", memberToken, 200, 1, 11, 10},
		{"/orders?status=pending&user_id=admin", memberToken, 200, 1, 11, 10},
		{"/orders?status=win", memberToken, 200, 1, 0, 0},
		{"/orders?status=unknown", memberToken, 400, 0, 0, 0},
		{"/orders?page=2&user_id=admin&page_size=1000", memberToken, 200, 2, 11, 1},
		{"/orders?page=999", memberToken, 200, 2, 11, 1},
		{"/orders?page=0", memberToken, 400, 0, 0, 0},
		{"/orders?page=-1", memberToken, 400, 0, 0, 0},
		{"/orders?page=x", memberToken, 400, 0, 0, 0},
		{"/orders?page=1000001", memberToken, 400, 0, 0, 0},
		{"/admin/orders?page=2", memberToken, 403, 0, 0, 0},
		{"/admin/orders?page=3", adminToken, 200, 3, 22, 2},
		{"/admin/orders?page=0", adminToken, 400, 0, 0, 0},
	} {
		req := httptest.NewRequest("GET", "http://localhost/api/v1"+tc.path, nil)
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s: status %d want %d: %s", tc.path, w.Code, tc.status, w.Body.String())
		}
		if tc.status == 200 {
			var body struct {
				Data OrderPage `json:"data"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &body))
			if body.Data.Page != tc.page || body.Data.Total != tc.total || body.Data.PageSize != 10 || len(body.Data.Items) != tc.length {
				t.Fatal("wrong HTTP page", body)
			}
			if tc.token == memberToken {
				for _, o := range body.Data.Items {
					if o.UserID != "member" {
						t.Fatal("user ID query leaked another user's orders")
					}
				}
			}
		}
	}
}

func TestOrderPageSummaryAndStatusAcrossPages(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 205; i++ {
			for _, owner := range []string{"member", "admin"} {
				o := Order{ID: seq(tx, "orders"), UserID: owner, Cost: 100, Status: "settled", Outcome: "win", Payout: 250}
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
			}
		}
		for _, o := range []Order{
			{UserID: "member", Cost: 200, Status: "settled", Outcome: "lose"},
			{UserID: "member", Cost: 300, Status: "settled", Outcome: "push", Payout: 300},
			{UserID: "member", Cost: 400, Status: "pending"},
			{UserID: "member", Cost: 500, Status: "refunded", Payout: 500},
			{UserID: "member", Cost: 600, Status: "settled", Payout: 900},
			{UserID: "member", Cost: 700, Status: "settled"},
		} {
			o.ID = seq(tx, "orders")
			if err := put(tx, "orders", o.ID, o); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, tc := range []struct {
		status        string
		total         int
		stake, payout int64
	}{
		{"", 211, 23200, 52450}, {"win", 206, 21100, 52150}, {"lose", 2, 900, 0}, {"push", 1, 300, 300}, {"pending", 1, 400, 0}, {"refunded", 1, 500, 0},
	} {
		for _, page := range []int{1, 2, 99} {
			data, err := s.OrdersPageFiltered("member", page, tc.status)
			must(t, err)
			if data.Total != tc.total || data.TotalStake != tc.stake || data.TotalPayout != tc.payout {
				t.Fatalf("%s page %d summary: %+v", tc.status, page, data)
			}
			for _, o := range data.Items {
				if o.UserID != "member" || tc.status != "" && orderResultStatus(o) != tc.status {
					t.Fatal("filter or owner isolation failed", o)
				}
			}
		}
	}
	if _, err := s.OrdersPageFiltered("member", 1, "not-a-status"); err == nil {
		t.Fatal("invalid filter accepted")
	}
	data, err := s.OrdersPageFiltered("admin", 1, "pending")
	must(t, err)
	if data.Total != 0 || data.TotalStake != 0 || data.TotalPayout != 0 {
		t.Fatal("empty summary", data)
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "member")
		if err != nil {
			return err
		}
		u.Status = "disabled"
		return put(tx, "users", u.ID, u)
	}))
	if _, err := s.OrdersPageFiltered("member", 1, ""); err != ErrForbidden {
		t.Fatal("disabled account accepted", err)
	}
}
