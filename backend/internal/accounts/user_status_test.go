package accounts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
)

func TestUserStatusEstimatesAndIsolation(t *testing.T) {
	s, admin := setup(t)
	parent := register(t, s, "status_parent", "")
	user := register(t, s, "status_child", parent.InviteCode)
	other := register(t, s, "status_child2", "")
	for _, u := range []User{user, other} {
		must(t, s.Adjust(admin.ID, u.ID, 100000, "test", randomID()))
	}
	cfg, err := s.Settings()
	must(t, err)
	cfg.CommissionBPS = [3]int64{333, 222, 111}
	must(t, s.SaveSettings(admin.ID, cfg))
	now := time.Date(2026, 10, 11, 18, 20, 10, 0, model.Beijing)
	g, _ := games.Find("ffc-1")
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	must(t, err)
	defer db.Close()
	must(t, db.CacheSources([]model.Draw{previewSource(g.Next(now).Add(-games.SourcePublishDelay))}))
	var orders []Order
	for _, market := range []string{"p1.big", "p1.small", "dt.dragon"} {
		in := marketInput(now, market)
		amount := int64(201)
		in.AmountCents = &amount
		in.Multiplier = 0
		order, err := s.PlaceMarketOrder(user.ID, in, now)
		must(t, err)
		orders = append(orders, order)
	}
	_, err = s.PlaceMarketOrder(other.ID, marketInput(now, "p1.big"), now)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, o := range []Order{
			{ID: "old", UserID: user.ID, SessionID: g.ID, Issue: g.Due(now).Format(model.IssueLayout), Status: "pending", Cost: 900},
			{ID: "refund", UserID: user.ID, SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), Status: "refunded", Cost: 900},
			{ID: "settled", UserID: user.ID, SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), Status: "settled", Cost: 900},
		} {
			if err := put(tx, "orders", o.ID, o); err != nil {
				return err
			}
		}
		return nil
	}))
	// Current prices and commission settings must not replace locked order snapshots.
	cfg.CommissionBPS = [3]int64{1000, 0, 0}
	must(t, s.SaveSettings(admin.ID, cfg))
	book, err := s.MarketBook(g.ID)
	must(t, err)
	for i := range book.Quotes {
		book.Quotes[i].OddsMilli = 2500
	}
	must(t, s.SaveQuotes(admin.ID, g.ID, book.Quotes))
	before, err := s.Account(user.ID)
	must(t, err)
	snapshot, err := s.UserStatus("  STATUS_CHILD  ", now, db)
	must(t, err)
	total := snapshot.Summary
	if snapshot.User.ID != user.ID || total.Orders != 3 || total.Stake != 603 || total.Payout != 594 || total.UserProfit == nil || *total.UserProfit != -9 || total.Net == nil || *total.Net != -3 || total.Commission != 12 {
		t.Fatalf("wrong user estimate: %+v", snapshot)
	}
	var row UserStatusSession
	for _, r := range snapshot.Sessions {
		if r.SessionID == g.ID {
			row = r
		}
	}
	if len(row.Orders) != 3 || row.Round == nil || row.NumberRule.Count != 5 || row.NumberRule.Min != 0 || row.NumberRule.Max != 9 || !row.NumberRule.AllowRepeat {
		t.Fatalf("missing current orders: %+v", row)
	}
	wanted := map[string]int64{"win": 192, "lose": -201, "push": 0}
	for _, o := range row.Orders {
		want, ok := wanted[o.Outcome]
		if !ok || o.Profit == nil || *o.Profit != want || o.Payout == nil {
			t.Fatalf("incorrect order forecast: %+v", o)
		}
	}
	// A blank query aggregates all users and retains every game, including empty games.
	all, err := s.UserStatus(" \t ", now, db)
	must(t, err)
	otherStatus, err := s.UserStatus(other.Username, now, db)
	must(t, err)
	if !all.AllUsers || all.User != nil || snapshot.AllUsers || len(all.Sessions) != len(games.All()) || all.Summary.Orders != 4 || all.Summary.Stake != total.Stake+otherStatus.Summary.Stake || all.Summary.Payout != total.Payout+otherStatus.Summary.Payout || all.Summary.UserProfit == nil || *all.Summary.UserProfit != *total.UserProfit+*otherStatus.Summary.UserProfit || all.BettingFrozen != 11503 {
		t.Fatalf("wrong all-user estimate: %+v", all)
	}
	owners := map[string]int{}
	for _, r := range all.Sessions {
		for _, o := range r.Orders {
			owners[o.Username]++
		}
		if r.SessionID != g.ID && (r.Estimate.Orders != 0 || r.Estimate.UserProfit == nil || *r.Estimate.UserProfit != 0) {
			t.Fatal("empty game estimate incorrect")
		}
	}
	if owners[user.Username] != 3 || owners[other.Username] != 1 {
		t.Fatal("missing order owners", owners)
	}
	after, err := s.Account(user.ID)
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read changed account state")
	}
	history, err := db.History(g.ID, 1, "")
	must(t, err)
	if history.Total != 0 {
		t.Fatal("forecast published a draw")
	}
	// A preview selected by the admin must immediately be the estimate source.
	draft, err := db.ManualPreview(g, row.Issue, []int{0, 1, 2, 3, 4}, row.Round.Numbers, now)
	must(t, err)
	adopted, err := db.ApplyPreview(g, row.Issue, draft.Candidate.ID, admin.ID, now)
	must(t, err)
	snapshot, err = s.UserStatus(user.Username, now, db)
	must(t, err)
	var payout int64
	for _, o := range orders {
		_, amount, err := calculateMarket(o, *adopted)
		must(t, err)
		payout += amount
	}
	if snapshot.Summary.Payout != payout || snapshot.Summary.UserProfit == nil || *snapshot.Summary.UserProfit != payout-603 {
		t.Fatal("adopted numbers not used")
	}
	all, err = s.UserStatus("", now, db)
	must(t, err)
	otherStatus, err = s.UserStatus(other.Username, now, db)
	must(t, err)
	if all.Summary.Payout != payout+otherStatus.Summary.Payout || *all.Summary.UserProfit != *snapshot.Summary.UserProfit+*otherStatus.Summary.UserProfit {
		t.Fatal("all-user estimate did not follow adopted numbers")
	}
	// Missing source cannot masquerade as a loss or zero payout estimate.
	snapshot, err = s.UserStatus(user.Username, now, nil)
	must(t, err)
	if snapshot.Summary.UserProfit != nil || snapshot.Summary.UnresolvedOrders != 3 {
		t.Fatal("unknown results estimated")
	}
	all, err = s.UserStatus("", now, nil)
	must(t, err)
	if all.Summary.UserProfit != nil || all.Summary.UnresolvedOrders != 4 {
		t.Fatal("all-user missing source estimated")
	}
	for _, r := range snapshot.Sessions {
		for _, o := range r.Orders {
			if o.Payout != nil || o.Profit != nil || o.Outcome != "unavailable" {
				t.Fatal("missing source not marked")
			}
		}
	}
	// On issue rollover, previous pending orders no longer count as the current issue.
	snapshot, err = s.UserStatus(user.Username, g.Next(now), db)
	must(t, err)
	if snapshot.Summary.Orders != 0 || snapshot.Summary.UserProfit == nil || *snapshot.Summary.UserProfit != 0 {
		t.Fatal("old issue still counted")
	}
	all, err = s.UserStatus("", g.Next(now), db)
	must(t, err)
	if all.Summary.Orders != 0 || all.BettingFrozen != 11503 {
		t.Fatal("all-user issue rollover incorrect")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "orders", "manual", Order{ID: "manual", UserID: user.ID, SessionID: g.ID, Issue: row.Issue, Status: "pending", Cost: 999})
	}))
	snapshot, err = s.UserStatus(user.Username, now, db)
	must(t, err)
	if snapshot.Summary.UserProfit != nil || snapshot.Summary.UnresolvedOrders != 1 || snapshot.Summary.Orders != 4 || snapshot.Summary.Stake != 1602 {
		t.Fatal("manual order silently estimated")
	}
	if _, err = s.UserStatus("status_ch", now, db); !errors.Is(err, ErrNotFound) {
		t.Fatal("partial username matched")
	}
	all, err = s.UserStatus("", now, db)
	must(t, err)
	if all.Summary.UserProfit != nil || all.Summary.UnresolvedOrders != 1 || all.Summary.Orders != 5 {
		t.Fatal("all-user manual order silently estimated")
	}
}

func TestUserStatusHTTPAdminOnly(t *testing.T) {
	s, _ := setup(t)
	user := register(t, s, "status_viewer", "")
	_, adminToken, err := s.Login("admin", testPassword)
	must(t, err)
	_, userToken, err := s.Login(user.Username, testPassword)
	must(t, err)
	mux := http.NewServeMux()
	h := HTTP{Store: s}
	h.Bind(mux)
	for _, tc := range []struct {
		token, query string
		status       int
	}{
		{"", "status_viewer", 401}, {userToken, "status_viewer", 403}, {adminToken, "status_viewer", 200}, {adminToken, "missing_user", 404}, {adminToken, "", 200}, {"", "", 401}, {userToken, "", 403},
	} {
		req := httptest.NewRequest("GET", "/api/v1/admin/user-status?username="+tc.query, nil)
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private response may be cached")
		}
		if tc.status == 200 {
			var envelope struct {
				Data UserStatus `json:"data"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			if envelope.Data.Summary.UserProfit == nil || *envelope.Data.Summary.UserProfit != 0 {
				t.Fatal("empty user response wrong")
			}
			if tc.query == "" {
				if !envelope.Data.AllUsers || envelope.Data.User != nil || len(envelope.Data.Sessions) != len(games.All()) {
					t.Fatal("all-user HTTP response wrong")
				}
			} else if envelope.Data.AllUsers || envelope.Data.User == nil || envelope.Data.User.ID != user.ID {
				t.Fatal("single-user HTTP response wrong")
			}
			if strings.Contains(w.Body.String(), "PasswordSalt") || strings.Contains(w.Body.String(), "Hash") {
				t.Fatal("credential data leaked")
			}
		}
	}
}
