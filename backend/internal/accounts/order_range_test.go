package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestOrderRangeBeijingBoundaries(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	// Deliberately UTC timestamps: Beijing leap day starts on the preceding UTC day.
	start := time.Date(2024, 2, 28, 16, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	settled := end.Add(7 * 24 * time.Hour)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i, at := range []time.Time{start.Add(-time.Nanosecond), start, end.Add(-time.Nanosecond), end, {}} {
			for _, owner := range []string{"member", "admin"} {
				o := Order{ID: seq(tx, "orders"), UserID: owner, Issue: fmt.Sprint(i), CreatedAt: at, SettledAt: &settled, Status: "settled", Cost: 100, Payout: 200}
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	for _, tc := range []struct {
		start, end string
		issues     []string
	}{
		{"2024-02-29", "2024-02-29", []string{"2", "1"}},
		{"2024-02-29", "", []string{"3", "2", "1"}},
		{"", "2024-02-29", []string{"2", "1", "0"}},
		{"", "", []string{"4", "3", "2", "1", "0"}},
		{"2024-03-02", "2024-03-03", []string{}},
	} {
		data, err := s.OrdersPageRange("member", 99, "win", tc.start, tc.end)
		must(t, err)
		got := []string{}
		for _, o := range data.Items {
			if o.UserID != "member" {
				t.Fatal("another account's order leaked", o)
			}
			got = append(got, o.Issue)
		}
		if !reflect.DeepEqual(got, tc.issues) || data.Total != len(got) || data.Page != 1 || data.Items == nil || data.TotalStake != int64(len(got))*100 || data.TotalPayout != int64(len(got))*200 {
			t.Fatalf("%s..%s: %+v, issues %v want %v", tc.start, tc.end, data, got, tc.issues)
		}
	}
	if _, err := s.OrdersPageRange("missing", 1, "", "2024-02-29", ""); err == nil {
		t.Fatal("unknown account accepted")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "member")
		if err != nil {
			return err
		}
		u.Status = "disabled"
		return put(tx, "users", u.ID, u)
	}))
	if _, err := s.OrdersPageRange("member", 1, "", "", "2024-02-29"); err != ErrForbidden {
		t.Fatal("disabled account accepted", err)
	}
}

func TestOrderRangeFullHistorySummaryPaginationAndStatuses(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	at := time.Date(2024, 2, 29, 4, 0, 0, 0, time.UTC)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		insert := func(o Order) error {
			for _, owner := range []string{"member", "admin"} {
				o.ID, o.UserID = seq(tx, "orders"), owner
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
			}
			return nil
		}
		for i := 0; i < 205; i++ {
			if err := insert(Order{Issue: fmt.Sprint(i), CreatedAt: at, Status: "settled", Outcome: "win", Cost: 100, Payout: 250}); err != nil {
				return err
			}
		}
		for _, o := range []Order{
			{CreatedAt: at, Status: "pending", Cost: 200},
			{CreatedAt: at, Status: "settled", Outcome: "push", Cost: 300, Payout: 300},
			{CreatedAt: at, Status: "settled", Outcome: "lose", Cost: 400},
			{CreatedAt: at, Status: "settled", Cost: 500, Payout: 900},
			{CreatedAt: at, Status: "settled", Cost: 600},
			{CreatedAt: at, Status: "refunded", Cost: 700, Payout: 700},
		} {
			if err := insert(o); err != nil {
				return err
			}
		}
		// More than 200 newer records must not hide the matching older history.
		for i := 0; i < 210; i++ {
			if err := insert(Order{CreatedAt: at.AddDate(0, 0, 1), Status: "settled", Outcome: "win", Cost: 9999, Payout: 9999}); err != nil {
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
		{"", 211, 23200, 52450}, {"win", 206, 21000, 52150}, {"lose", 2, 1000, 0},
		{"push", 1, 300, 300}, {"pending", 1, 200, 0}, {"refunded", 1, 700, 0},
	} {
		seen := map[string]bool{}
		last := (tc.total + 9) / 10
		for page := 1; page <= last; page++ {
			data, err := s.OrdersPageRange("member", page, tc.status, "2024-02-29", "2024-02-29")
			must(t, err)
			if data.Total != tc.total || data.TotalStake != tc.stake || data.TotalPayout != tc.payout || data.Page != page || data.PageSize != 10 || len(data.Items) != min(10, tc.total-(page-1)*10) {
				t.Fatalf("%s page %d: %+v", tc.status, page, data)
			}
			for _, o := range data.Items {
				if o.UserID != "member" || seen[o.ID] || !o.CreatedAt.Equal(at) || tc.status != "" && orderResultStatus(o) != tc.status {
					t.Fatal("leaked, duplicated, wrong date or status", o)
				}
				seen[o.ID] = true
			}
		}
		if len(seen) != tc.total {
			t.Fatal("history truncated", len(seen), tc.total)
		}
		data, err := s.OrdersPageRange("member", 999, tc.status, "2024-02-29", "2024-02-29")
		must(t, err)
		if data.Page != last || data.Total != tc.total || data.TotalStake != tc.stake || data.TotalPayout != tc.payout {
			t.Fatal("last page or summary changed", data)
		}
	}
}

func TestOrderRangeHTTPValidationAndIsolation(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var err error
		token, err = newLogin(tx, "member")
		if err != nil {
			return err
		}
		for i := 0; i < 11; i++ {
			for _, owner := range []string{"member", "admin"} {
				o := Order{ID: seq(tx, "orders"), UserID: owner, Status: "pending", Cost: 100, CreatedAt: time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)}
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	request := func(query, session string, status int) OrderPage {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/orders?"+query, nil)
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: session})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: status %d want %d: %s", query, w.Code, status, w.Body.String())
		}
		var body struct {
			Data  OrderPage                `json:"data"`
			Error struct{ Message string } `json:"error"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &body))
		if status != 200 && body.Error.Message == "" {
			t.Fatal("missing error message", w.Body.String())
		}
		return body.Data
	}
	request("start_date=2024-02-29", "", 401)
	for _, invalid := range []string{"2023-02-29", "1900-02-29", "2024-02-30", "2024-04-31", "2024-00-01", "2024-13-01", "2024-01-00", "2024-2-29", "24-02-29", "0000-01-01", "10000-01-01", "2024-02-29T00:00:00Z", " 2024-02-29", "2024-02-29 ", "garbage"} {
		for _, name := range []string{"start_date", "end_date"} {
			request(name+"="+url.QueryEscape(invalid), token, 400)
		}
	}
	for _, query := range []string{"start_date=2024-03-01&end_date=2024-02-29", "status=settled&start_date=2024-02-29", "page=0&start_date=2024-02-29"} {
		request(query, token, 400)
	}
	for _, query := range []string{"start_date=2000-02-29", "start_date=0001-01-01&end_date=9999-12-31", "start_date=&end_date=", "start_date=2024-02-29", "end_date=2024-02-29"} {
		data := request(query, token, 200)
		if data.Total != 11 || data.TotalStake != 1100 || data.TotalPayout != 0 {
			t.Fatal("optional/valid date summary", query, data)
		}
	}
	data := request("page=2&status=pending&start_date=2024-02-29&end_date=2024-02-29&user_id=admin&page_size=1000", token, 200)
	if data.Page != 2 || data.PageSize != 10 || len(data.Items) != 1 || data.Total != 11 || data.TotalStake != 1100 || data.Items[0].UserID != "member" {
		t.Fatal("wrong page or leaked another user's history", data)
	}
	data = request("start_date=2024-03-01&status=pending", token, 200)
	if data.Items == nil || data.Total != 0 || data.TotalStake != 0 || data.TotalPayout != 0 || data.Page != 1 {
		t.Fatal("wrong empty response", data)
	}
}
