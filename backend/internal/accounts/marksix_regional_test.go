package accounts

import (
	"context"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestRegionalUpgradePreservesOrdersFundsQuotesAndSettlesOldEveningIssues(t *testing.T) {
	s, path, _ := fundsFixture(t)
	start := games.RegionalMarkSixStart()
	at := start.Add(9*time.Hour + 30*time.Minute)
	reader := &batchReader{rounds: map[string]games.Round{}}
	orders := []Order{}
	oldNames := map[string]string{"lhc-hk": "香港六合彩", "lhc-sg": "新加坡六合彩", "lhc-bj": "北京六合彩"}
	for _, id := range []string{"lhc-hk", "lhc-sg", "lhc-bj"} {
		g, _ := games.Find(id)
		in := MarketOrderInput{SessionID: id, Issue: at.Format(model.IssueLayout), MarketID: "lhc6.p1.big", OddsMilli: 1000, UnitCents: 200, Multiplier: 5, Key: randomID()}
		o, err := s.PlaceMarketOrder("member", in, g.CloseAt(at).Add(-time.Second))
		must(t, err)
		for _, m := range markets.LegacyMarkSixCatalog() {
			if m.ID == "size2.p1.big" {
				o.RuleSnapshot = &m
				o.MarketID = m.ID
				o.OddsMilli = 1960
			}
		}
		// Recreate the persisted shape of an evening order accepted before the upgrade.
		// Its financial reservation and locked rule/odds use the real order path.
		o.GameName, o.SessionName, o.CreatedAt = oldNames[id], "每日21:30", start.Add(-2*time.Hour)
		must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "orders", o.ID, o) }))
		orders = append(orders, o)
		r := marketRound(in, []int{30, 2, 3, 4, 5, 6, 7})
		reader.rounds[id] = r
	}
	book, err := s.MarketBook("lhc-hk")
	must(t, err)
	book.Quotes[0].OddsMilli = 1810
	must(t, s.SaveQuotes("admin", "lhc-hk", book.Quotes))
	cfg, err := s.Settings()
	must(t, err)
	kept := cfg.Plays[:0]
	for _, play := range cfg.Plays {
		if play.SessionID == "lhc-id" || play.SessionID == "lhc-vn" {
			continue
		}
		if _, old := oldNames[play.SessionID]; old {
			play.CloseSeconds = 905
		}
		if play.SessionID == "lhc-daily" {
			play.Enabled, play.UnitCents, play.UserIssueLimitCents = false, 500, 123456
		}
		kept = append(kept, play)
	}
	cfg.Plays = kept
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "settings", "global", cfg) }))
	snapshot := func() map[string]map[string]string {
		out := map[string]map[string]string{}
		must(t, s.db.View(func(tx *bolt.Tx) error {
			for _, bucket := range []string{"users", "orders", "ledger", "commissions", "pending_markets", "settlement_plans", "prepared_payouts", issueStakeBucket} {
				out[bucket] = map[string]string{}
				if err := tx.Bucket([]byte(bucket)).ForEach(func(k, v []byte) error { out[bucket][string(k)] = string(v); return nil }); err != nil {
					return err
				}
			}
			return nil
		}))
		return out
	}
	before := snapshot()
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer s.Close()
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("upgrade changed financial state or pending orders")
	}
	afterBook, err := s.MarketBook("lhc-hk")
	must(t, err)
	if !reflect.DeepEqual(book.Quotes, afterBook.Quotes) {
		t.Fatal("upgrade changed existing odds")
	}
	updated, err := s.Settings()
	must(t, err)
	if len(updated.Plays) != len(games.All()) {
		t.Fatal("missing regional settings")
	}
	for _, play := range updated.Plays {
		g, _ := games.Find(play.SessionID)
		if play.CloseSeconds != g.ClosedWindowSeconds() {
			t.Fatal("wrong cutoff", play.SessionID)
		}
		if play.SessionID == "lhc-daily" && (play.Enabled || play.UnitCents != 500 || play.UserIssueLimitCents != 123456) {
			t.Fatal("custom limits overwritten")
		}
	}
	assertFunds(t, s, "member", 97000, 3000, 0)
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	must(t, s.processSettlementPlans(context.Background(), reader, at.Add(-4*time.Second), batchLogger))
	assertFunds(t, s, "member", 97000, 3000, 0)
	reader.published = true
	plans, err := s.settlementPlans()
	must(t, err)
	if len(plans) != 1 {
		t.Fatal("old evening issue missing from settlement plan")
	}
	// Supply the publication clock explicitly so this transition test also runs
	// before the deployment date; the live worker samples the real clock instead.
	must(t, s.commitUsers(plans[0], planUsers(plans[0]), reader.rounds, at))
	must(t, s.commitUsers(plans[0], planUsers(plans[0]), reader.rounds, at.Add(time.Second)))
	must(t, s.cleanupPlan(plans[0]))
	assertFunds(t, s, "member", 102880, 0, 0)
	for _, old := range orders {
		got, err := s.Order(old.ID)
		must(t, err)
		if got.Status != "settled" || got.Payout != 1960 || got.GameName != old.GameName || got.Issue != old.Issue || got.OddsMilli != old.OddsMilli {
			t.Fatal("legacy order settlement changed", got.ID)
		}
	}
}
