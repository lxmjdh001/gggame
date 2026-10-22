package accounts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func batchInput(now time.Time) MarketBatchInput {
	g, _ := games.Find("ffc-1")
	return MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Selections: []MarketSelection{{MarketID: "p1.big", OddsMilli: 1960}, {MarketID: "p2.small", OddsMilli: 1960}}, Key: randomID()}
}

func TestMarketBatchAtomicRejections(t *testing.T) {
	for _, name := range []string{"balance", "odds", "disabled", "duplicate", "invalid_market", "closed", "issue", "limit", "amount", "key", "empty", "too_many"} {
		t.Run(name, func(t *testing.T) {
			s, _, now := fundsFixture(t)
			defer s.Close()
			in := batchInput(now)
			switch name {
			case "balance":
				in.AmountCents = 60000
			case "odds":
				in.Selections[1].OddsMilli = 1800
			case "disabled":
				book, err := s.MarketBook(in.SessionID)
				must(t, err)
				for i := range book.Quotes {
					if book.Quotes[i].MarketID == in.Selections[1].MarketID {
						book.Quotes[i].Enabled = false
					}
				}
				must(t, s.SaveQuotes("admin", in.SessionID, book.Quotes))
			case "duplicate":
				in.Selections[1] = in.Selections[0]
			case "invalid_market":
				in.Selections[1].MarketID = "missing"
			case "closed":
				g, _ := games.Find(in.SessionID)
				now = g.CloseAt(g.Next(now))
			case "issue":
				in.Issue = "200001010001"
			case "limit":
				cfg, err := s.Settings()
				must(t, err)
				for i := range cfg.Plays {
					if cfg.Plays[i].SessionID == in.SessionID {
						cfg.Plays[i].UserIssueLimitCents = 1500
					}
				}
				must(t, s.SaveSettings("admin", cfg))
			case "amount":
				in.AmountCents = 0
			case "key":
				in.Key = "short"
			case "empty":
				in.Selections = nil
			case "too_many":
				in.Selections = make([]MarketSelection, 501)
			}
			result, err := s.PlaceMarketBatch("member", in, now)
			if err == nil || len(result.Orders) != 0 || result.TotalCost != 0 {
				t.Fatal("failed batch returned success", result)
			}
			assertFunds(t, s, "member", 100000, 0, 0)
			must(t, s.db.View(func(tx *bolt.Tx) error {
				for _, bucket := range []string{"orders", "ledger", "pending_markets", "keys"} {
					if tx.Bucket([]byte(bucket)).Stats().KeyN != 0 {
						t.Fatal("partial batch persisted", bucket)
					}
				}
				return nil
			}))
			if name == "limit" {
				single := marketInput(now, "p1.big")
				single.Multiplier = 0
				single.AmountCents = &in.AmountCents
				_, err = s.PlaceMarketOrder("member", single, now)
				must(t, err)
			}
		})
	}
}

func TestMarketBatchRetryRestartAndSettlement(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	in := batchInput(now)
	first, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	if len(first.Orders) != 2 || first.TotalCost != 2000 {
		t.Fatal("wrong batch total", first)
	}
	assertFunds(t, s, "member", 98000, 2000, 0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			again, err := s.PlaceMarketBatch("member", in, now.Add(time.Hour))
			if err != nil || len(again.Orders) != 2 || again.Orders[0].ID != first.Orders[0].ID {
				t.Error("retry duplicated or lost batch", err)
			}
		}()
	}
	wg.Wait()
	changed := in
	changed.AmountCents++
	if _, err = s.PlaceMarketBatch("member", changed, now); err == nil {
		t.Fatal("same key accepted with changed amount")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	again, err := s.PlaceMarketBatch("member", in, now.Add(time.Hour))
	must(t, err)
	if again.Orders[1].ID != first.Orders[1].ID {
		t.Fatal("restart lost batch idempotency")
	}
	assertFunds(t, s, "member", 98000, 2000, 0)
	round := marketRound(MarketOrderInput{SessionID: in.SessionID, Issue: in.Issue}, []int{8, 1, 2, 3, 4})
	for _, o := range first.Orders {
		must(t, s.SettleMarket(o.ID, round))
		must(t, s.SettleMarket(o.ID, round))
	}
	assertFunds(t, s, "member", 101920, 0, 0)
	after, err := s.Account("member")
	must(t, err)
	if len(after["orders"].([]Order)) != 2 || len(after["ledger"].([]Ledger)) != 4 {
		t.Fatal("duplicate orders or settlement ledger")
	}
}

func TestMarketBatchAllSessions(t *testing.T) {
	for _, g := range games.All() {
		t.Run(g.ID, func(t *testing.T) {
			s, _, now := fundsFixture(t)
			defer s.Close()
			if !g.BettingOpen(now) {
				now = g.CloseAt(g.Next(now)).Add(-time.Second)
			}
			book, err := s.marketBookAt(g, g.Next(now))
			must(t, err)
			in := MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Key: randomID()}
			// Mixed categories must both remain independent, settled orders.
			for _, category := range []string{"two_sides", "regular"} {
				for _, m := range book.Markets {
					if m.Category == category {
						for _, q := range book.Quotes {
							if q.MarketID == m.ID {
								in.Selections = append(in.Selections, MarketSelection{MarketID: m.ID, OddsMilli: q.OddsMilli})
								break
							}
						}
						break
					}
				}
			}
			if len(in.Selections) != 2 {
				t.Fatal("missing categories")
			}
			out, err := s.PlaceMarketBatch("member", in, now)
			must(t, err)
			if len(out.Orders) != 2 || out.Orders[0].RuleSnapshot == nil || out.Orders[1].RuleSnapshot == nil {
				t.Fatal("missing rule snapshots")
			}
			assertFunds(t, s, "member", 98000, 2000, 0)
		})
	}
}

func TestMarketBatchConcurrentBalance(t *testing.T) {
	s, _, now := fundsFixture(t)
	defer s.Close()
	in := batchInput(now)
	in.AmountCents = 30000
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := in
			request.Key = randomID()
			if _, err := s.PlaceMarketBatch("member", request, now); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatal("concurrent batch overspent", success)
	}
	assertFunds(t, s, "member", 40000, 60000, 0)
}

func TestMarketBatchHTTP(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var err error; token, err = newLogin(tx, "member"); return err }))
	// Daily session avoids the one-minute cutoff racing this HTTP integration check.
	g, _ := games.Find("lhc-daily")
	book, err := s.MarketBook(g.ID)
	must(t, err)
	now := time.Now()
	if !g.BettingOpen(now) {
		t.Skip("daily draw is sealed")
	}
	in := MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Key: randomID(), Selections: []MarketSelection{{MarketID: book.Quotes[0].MarketID, OddsMilli: book.Quotes[0].OddsMilli}, {MarketID: book.Quotes[1].MarketID, OddsMilli: book.Quotes[1].OddsMilli}}}
	body, err := json.Marshal(in)
	must(t, err)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {token, 200}, {token, 200}} {
		r := httptest.NewRequest("POST", "http://localhost/api/v1/market-orders/batch", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	assertFunds(t, s, "member", 98000, 2000, 0)
}
