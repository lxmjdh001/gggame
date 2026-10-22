package accounts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

func TestAccountRecordPagesScopePaginationAndDailyResults(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, model.Beijing)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 205; i++ {
			id := fmt.Sprintf("%020d", i+1)
			if err := put(tx, "ledger", id, Ledger{ID: id, UserID: "member", Amount: 100, CreatedAt: at}); err != nil {
				return err
			}
			if err := put(tx, "requests", id, WalletRequest{ID: id, UserID: "member", Amount: 100, Kind: "deposit", CreatedAt: at}); err != nil {
				return err
			}
			if err := put(tx, "orders", id, Order{ID: id, UserID: "member", Status: "settled", Cost: 100, Payout: 200, CreatedAt: at}); err != nil {
				return err
			}
		}
		for _, id := range []string{"refunded", "pending"} {
			if err := put(tx, "orders", id, Order{ID: id, UserID: "member", Status: id, Cost: 999900, Payout: 999900, CreatedAt: at}); err != nil {
				return err
			}
		}
		if err := put(tx, "orders", "push", Order{ID: "push", UserID: "member", Status: "settled", Cost: 500, Payout: 500, CreatedAt: at}); err != nil {
			return err
		}
		midnight := time.Date(2026, 10, 20, 16, 0, 0, 0, time.UTC)
		if err := put(tx, "orders", "midnight", Order{ID: "midnight", UserID: "member", Status: "settled", Cost: 300, Payout: 0, CreatedAt: midnight}); err != nil {
			return err
		}
		for i := 1; i <= 12; i++ {
			id := fmt.Sprintf("older%02d", i)
			if err := put(tx, "orders", id, Order{ID: id, UserID: "member", Status: "settled", Cost: 100, Payout: 50, CreatedAt: at.AddDate(0, 0, -i)}); err != nil {
				return err
			}
		}
		if err := put(tx, "ledger", "unrelated", Ledger{ID: "unrelated", UserID: "other", Note: "other-private-ledger"}); err != nil {
			return err
		}
		if err := put(tx, "requests", "unrelated", WalletRequest{ID: "unrelated", UserID: "other", Note: "other-private-payment"}); err != nil {
			return err
		}
		return put(tx, "orders", "unrelated", Order{ID: "unrelated", UserID: "other", Status: "settled", Cost: 999999, Payout: 888888, CreatedAt: at})
	}))
	before := registrationCounts(t, s)
	ledger, err := s.LedgerPage("member", 1)
	must(t, err)
	if ledger.Total != 205 || len(ledger.Items) != 10 || ledger.Items[0].ID != fmt.Sprintf("%020d", 205) {
		t.Fatal("ledger scope or ordering incorrect", ledger)
	}
	last, err := s.LedgerPage("member", 999)
	must(t, err)
	if last.Page != 21 || len(last.Items) != 5 {
		t.Fatal("ledger last page incorrect", last)
	}
	requests, err := s.TransactionsPage("member", 21)
	must(t, err)
	if requests.Total != 205 || len(requests.Items) != 5 {
		t.Fatal("transaction pagination incorrect", requests)
	}
	days, err := s.DailyResultsPage("member", 1)
	must(t, err)
	if days.Total != 14 || len(days.Items) != 10 || days.Items[0].Date != "2026-10-21" || days.Items[0].Net != -300 {
		t.Fatal("daily date or totals incorrect", days)
	}
	today := days.Items[1]
	if today.Date != "2026-10-20" || today.Orders != 206 || today.Stake != 21000 || today.Payout != 41500 || today.Net != 20500 {
		t.Fatal("daily full-history aggregation incorrect", today)
	}
	dailyLast, err := s.DailyResultsPage("member", 2)
	must(t, err)
	if len(dailyLast.Items) != 4 || dailyLast.Items[3].Date != "2026-10-08" {
		t.Fatal("daily pagination incorrect", dailyLast)
	}
	if _, err = s.DailyResultsPage("missing", 1); err != ErrUnauthorized {
		t.Fatal("missing account accepted", err)
	}
	assertRegistrationUnchanged(t, s, before)
}

func TestAccountRecordHTTPAuthorizationAndPageValidation(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var err error; token, err = newLogin(tx, "member"); return err }))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	for _, endpoint := range []string{"ledger", "transactions", "daily-results"} {
		for _, tc := range []struct {
			query, cookie string
			code          int
		}{{"", "", 401}, {"?page=-1", token, 400}, {"?page=bad", token, 400}, {"?page=999&user_id=other", token, 200}} {
			req := httptest.NewRequest("GET", "http://localhost/api/v1/account/"+endpoint+tc.query, nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.cookie})
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.code {
				t.Fatalf("%s: want %d got %d %s", endpoint, tc.code, w.Code, w.Body.String())
			}
			if tc.code == 200 {
				var v struct {
					Data struct {
						Items []any `json:"items"`
						Total int   `json:"total"`
					} `json:"data"`
				}
				must(t, json.Unmarshal(w.Body.Bytes(), &v))
				if len(v.Data.Items) != 0 || v.Data.Total != 0 {
					t.Fatal("unexpected records")
				}
			}
			if bytes.Contains(w.Body.Bytes(), []byte("PasswordSalt")) {
				t.Fatal("credentials exposed")
			}
		}
	}
	must(t, s.SetStatus("admin", "member", "disabled"))
	for _, endpoint := range []string{"ledger", "transactions", "daily-results"} {
		req := httptest.NewRequest("GET", "http://localhost/api/v1/account/"+endpoint, nil)
		req.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("disabled account read records", w.Code)
		}
	}
}
