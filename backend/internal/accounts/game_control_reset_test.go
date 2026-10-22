package accounts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

func TestGameControlResetPreservesMoneyAndRestarts(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	now = now.In(model.Beijing)
	var existing []Order
	for i := 0; i < 3; i++ {
		o, err := s.PlaceMarketOrder("member", marketInput(now, "p1.big"), now)
		must(t, err)
		existing = append(existing, o)
	}
	for i := 0; i < 2; i++ {
		must(t, s.SettleMarket(existing[i].ID, marketRound(marketInput(now, "p1.big"), []int{1, 2, 3, 4, 5})))
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 2; i++ {
			o, err := get[Order](tx, "orders", existing[i].ID)
			if err != nil {
				return err
			}
			at := now.Add(-time.Minute)
			if i == 1 {
				at = at.AddDate(0, 0, -1)
			}
			o.SettledAt = &at
			if err = put(tx, "orders", o.ID, o); err != nil {
				return err
			}
		}
		return put(tx, "commissions", "test_commission", Commission{ID: "test_commission", OrderID: existing[0].ID, Amount: 50})
	}))
	before, err := s.GameControl(now)
	must(t, err)
	wantMonth := 1
	if now.Day() > 1 {
		wantMonth++
	}
	if before.Today.Orders != 1 || before.Yesterday.Orders != 1 || before.Month.Orders != wantMonth || before.PendingOrders != 1 {
		t.Fatal("bad fixture", before)
	}
	// Compare all actual business records byte-for-byte across the reset transaction.
	snapshot := func() map[string]string {
		out := map[string]string{}
		must(t, s.db.View(func(tx *bolt.Tx) error {
			for _, bucket := range []string{"users", "orders", "ledger", "commissions", "pending_markets", "requests", "market_quotes"} {
				if err := tx.Bucket([]byte(bucket)).ForEach(func(k, v []byte) error { out[bucket+string(k)] = string(v); return nil }); err != nil {
					return err
				}
			}
			return nil
		}))
		return out
	}
	original := snapshot()
	requestKey := randomID()
	reset, err := s.ResetGameControl("admin", requestKey, now)
	must(t, err)
	if !reflect.DeepEqual(original, snapshot()) {
		t.Fatal("reset modified money or orders")
	}
	cleared, err := s.GameControl(now)
	must(t, err)
	if cleared.Today.Orders != 0 || cleared.Yesterday.Orders != 0 || cleared.Month.Orders != 0 || cleared.PendingAmount != 0 || cleared.PendingOrders != 0 || cleared.Today.Net != 0 || cleared.Today.Commission != 0 || cleared.ResetAt == nil {
		t.Fatal("cards not cleared", cleared)
	}
	if !reflect.DeepEqual(before.Sessions, cleared.Sessions) {
		t.Fatal("reset affected session breakdown or estimates")
	}
	assertFunds(t, s, "member", 70000, 10000, 0)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	// A new order with the same timestamp must count: the boundary uses ordered IDs.
	newOrder, err := s.PlaceMarketOrder("member", marketInput(now, "p2.small"), now)
	must(t, err)
	again, err := s.ResetGameControl("admin", requestKey, now.Add(time.Second))
	must(t, err)
	if !reset.At.Equal(again.At) || reset.AfterOrderID != again.AfterOrderID {
		t.Fatal("retry reset the boundary a second time")
	}
	current, err := s.GameControl(now)
	must(t, err)
	if current.PendingOrders != 1 || current.PendingAmount != 10000 {
		t.Fatal("new order lost on retry/restart", current)
	}
	// The old pending order still settles, but never re-enters the reset cards.
	must(t, s.SettleMarket(existing[2].ID, marketRound(marketInput(now, "p1.big"), []int{8, 1, 2, 3, 4})))
	current, err = s.GameControl(time.Now().Add(time.Second))
	must(t, err)
	if current.Month.Orders != 0 || current.PendingOrders != 1 {
		t.Fatal("old order returned to reset summary")
	}
	must(t, s.SettleMarket(newOrder.ID, marketRound(marketInput(now, "p2.small"), []int{8, 1, 2, 3, 4})))
	current, err = s.GameControl(time.Now().Add(time.Second))
	must(t, err)
	if current.Today.Orders != 1 || current.Today.Payout != 19600 || current.Today.Net != -9600 || current.PendingOrders != 0 {
		t.Fatal("new settlement not counted", current.Today)
	}
	assertFunds(t, s, "member", 99200, 0, 0)
	_, err = s.ResetGameControl("admin", randomID(), time.Now())
	must(t, err)
	current, err = s.GameControl(time.Now())
	must(t, err)
	if current.Today.Orders != 0 || current.Month.Net != 0 {
		t.Fatal("second deliberate reset failed")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		items, err := list[Audit](tx, "audit")
		if err != nil {
			return err
		}
		count := 0
		for _, item := range items {
			if item.Action == "game_control.reset" {
				count++
			}
		}
		if count != 2 {
			t.Fatal("audit missing or retry duplicated audit", count)
		}
		return nil
	}))
}

func TestGameControlResetHTTPAuthorization(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var adminToken, memberToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var err error
		adminToken, err = newLogin(tx, "admin")
		if err != nil {
			return err
		}
		memberToken, err = newLogin(tx, "member")
		return err
	}))
	if _, err := s.ResetGameControl("member", randomID(), time.Now()); err != ErrForbidden {
		t.Fatal("member reset stats")
	}
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	body, _ := json.Marshal(map[string]string{"key": randomID()})
	for _, tc := range []struct {
		token, origin string
		body          []byte
		code          int
	}{{"", "", body, 401}, {memberToken, "", body, 403}, {adminToken, "https://attacker.test", body, 403}, {adminToken, "", []byte(`{"key":"short"}`), 400}, {adminToken, "", body, 200}, {adminToken, "", body, 200}} {
		r := httptest.NewRequest("POST", "http://localhost/api/v1/admin/game-control/reset", bytes.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "http://localhost/api/v1/admin/game-control", nil)
	r.AddCookie(&http.Cookie{Name: "starlight_session", Value: adminToken})
	w := httptest.NewRecorder()
	h.Protect(mux).ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "reset_at") {
		t.Fatal("reset marker unavailable after refresh")
	}
}
