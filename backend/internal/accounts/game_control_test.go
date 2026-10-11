package accounts

import (
	bolt "go.etcd.io/bbolt"
	"net/http"
	"net/http/httptest"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestGameControlPeriodsAndExposure(t *testing.T) {
	for _, now := range []time.Time{time.Date(2026, 10, 11, 12, 0, 0, 0, model.Beijing), time.Date(2026, 1, 1, 0, 0, 0, 0, model.Beijing)} {
		t.Run(now.Format("20060102"), func(t *testing.T) {
			s, _ := setup(t)
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, model.Beijing)
			before := start.Add(-time.Nanosecond)
			after := now.Add(time.Hour)
			// Store UTC instants to verify classification uses Beijing boundaries, not timestamp offsets.
			utc := start.UTC()
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				for _, o := range []Order{
					{ID: "win", SessionID: "ffc-1", Status: "settled", Cost: 10000, Payout: 19600, SettledAt: &utc, CreatedAt: before},
					{ID: "lose", SessionID: "ffc-1", Status: "settled", Cost: 20000, Payout: 0, SettledAt: &utc},
					{ID: "push", SessionID: "ffc-1", Status: "settled", Outcome: "push", Cost: 5000, Payout: 5000, SettledAt: &utc},
					{ID: "yesterday", SessionID: "ssc-10", Status: "settled", Cost: 5000, Payout: 8000, SettledAt: &before},
					{ID: "refund", SessionID: "ffc-1", Status: "refunded", Cost: 30000, Payout: 30000, SettledAt: &utc},
					{ID: "pending", SessionID: "ffc-1", Status: "pending", Cost: 12000, MarketID: "p1.big"},
					{ID: "manual_pending", SessionID: "lhc-daily", Status: "pending", Cost: 7000},
					{ID: "undated", SessionID: "ssc-10", Status: "settled", Cost: 1000},
					{ID: "future", SessionID: "ssc-10", Status: "settled", Cost: 9000, SettledAt: &after},
				} {
					if e := put(tx, "orders", o.ID, o); e != nil {
						return e
					}
				}
				for _, c := range []Commission{{ID: "c1", OrderID: "win", Amount: 100, Status: "claimed"}, {ID: "c2", OrderID: "lose", Amount: 200, Status: "available"}, {ID: "c3", OrderID: "yesterday", Amount: 50, Status: "available"}} {
					if e := put(tx, "commissions", c.ID, c); e != nil {
						return e
					}
				}
				return nil
			}))
			got, e := s.GameControl(now)
			must(t, e)
			if got.Today.Stake != 35000 || got.Today.Payout != 24600 || got.Today.Net != 10100 || got.Today.Commission != 300 || got.Today.Orders != 3 {
				t.Fatalf("today wrong %+v", got.Today)
			}
			if got.Yesterday.Net != -3050 || got.Yesterday.Orders != 1 {
				t.Fatalf("yesterday wrong %+v", got.Yesterday)
			}
			monthNet := int64(7050)
			if now.Day() == 1 {
				monthNet = 10100
			}
			if got.Month.Net != monthNet {
				t.Fatalf("month wrong %+v", got.Month)
			}
			if got.PendingAmount != 19000 || got.PendingOrders != 2 || got.UndatedOrders != 1 {
				t.Fatalf("exposure wrong %+v", got)
			}
			for _, row := range got.Sessions {
				if row.SessionID == "ffc-1" && (row.Today.Net != 10100 || row.PendingAmount != 12000) {
					t.Fatal("session aggregation wrong", row)
				}
			}
			again, e := s.GameControl(now)
			must(t, e)
			if again.Today.Net != got.Today.Net || again.PendingAmount != got.PendingAmount {
				t.Fatal("repeated reads changed results")
			}
		})
	}
}
func TestGameControlAdminOnly(t *testing.T) {
	s, a := setup(t)
	u := register(t, s, "stats_user", "")
	_, at, e := s.Login(a.Username, testPassword)
	must(t, e)
	_, ut, e := s.Login(u.Username, testPassword)
	must(t, e)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {ut, 403}, {at, 200}} {
		r := httptest.NewRequest("GET", "http://localhost/api/v1/admin/game-control", nil)
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
