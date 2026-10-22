package accounts

import (
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func TestAustraliaBatchFreezesAndSettlesSupplementaryPositions(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	g, _ := games.Find("lhc-au")
	// Fix the historical 45-of-8 issue inside its open betting window.
	now := games.Sydney49Start().Add(-time.Hour).Add(time.Minute)
	in := MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Key: randomID(), Selections: []MarketSelection{
		{MarketID: "au45.p7.big", OddsMilli: 1960}, {MarketID: "au45.p7.odd", OddsMilli: 1960}, {MarketID: "au45.p7.even", OddsMilli: 1960}, {MarketID: "au45.p8.big", OddsMilli: 1960},
	}}
	batch, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	assertFunds(t, s, "member", 96000, 4000, 0)
	r := marketRound(MarketOrderInput{SessionID: g.ID, Issue: in.Issue}, []int{1, 2, 3, 4, 5, 6, 23, 45})
	for i, order := range batch.Orders {
		want := []string{"push", "win", "lose", "win"}[i]
		outcome, _, err := calculateMarket(order, r)
		must(t, err)
		if outcome != want {
			t.Fatal(outcome, want)
		}
		must(t, s.SettleMarket(order.ID, r))
		must(t, s.SettleMarket(order.ID, r))
	}
	assertFunds(t, s, "member", 100920, 0, 0)
	old := in
	old.Key = randomID()
	old.Selections = []MarketSelection{{MarketID: "p7.big", OddsMilli: 1960}}
	if _, err := s.PlaceMarketBatch("member", old, now); err == nil {
		t.Fatal("old browser market accepted")
	}
	if validateNumbersAt(g, []int{1, 2, 3, 4, 5, 46}, games.Sydney49Start().Add(-time.Hour)) || !validateNumbersAt(g, []int{1, 2, 3, 4, 5, 45}, games.Sydney49Start().Add(-time.Hour)) {
		t.Fatal("classic range not updated")
	}
}

func TestAustraliaAndParityMigrationPreserveCustomOdds(t *testing.T) {
	s, path, _ := fundsFixture(t)
	legacy := []Quote{}
	for _, m := range markets.Catalog("lhc") {
		if len(m.Positions) != 1 {
			continue
		}
		id := strings.TrimPrefix(strings.TrimPrefix(m.ID, "size2."), "parity2.")
		legacy = append(legacy, Quote{MarketID: id, OddsMilli: m.DefaultOdds, Enabled: true})
	}
	for i := range legacy {
		if legacy[i].MarketID == "p1.odd" {
			legacy[i].OddsMilli = 1880
			legacy[i].Enabled = false
		}
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("rule_migrations")).Delete([]byte("au45-v2")); err != nil {
			return err
		}
		if err := put(tx, "market_quotes", "lhc-daily", legacy); err != nil {
			return err
		}
		return put(tx, "market_quotes", "lhc-au", legacy)
	}))
	must(t, s.Close())
	s, err := Open(path)
	must(t, err)
	for _, id := range []string{"lhc-au", "lhc-daily"} {
		book, err := s.MarketBook(id)
		must(t, err)
		for _, q := range book.Quotes {
			if q.MarketID == "au45.p1.odd" || q.MarketID == "parity2.p1.odd" {
				if q.OddsMilli != 1880 || q.Enabled {
					t.Fatal("custom parity quote lost")
				}
			}
			if q.MarketID == "au45.p1.n1" && q.OddsMilli != 44100 {
				t.Fatal("45-number default odds not updated")
			}
		}
	}
	before, err := s.MarketBook("lhc-au")
	must(t, err)
	g, _ := games.Find("lhc-au")
	expectedCount := len(markets.Catalog(g.MarketCatalogIDAt(g.Next(time.Now()))))
	if len(before.Markets) != expectedCount || len(before.Quotes) != expectedCount {
		t.Fatal("incomplete Australia market book")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer s.Close()
	after, err := s.MarketBook("lhc-au")
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("migration is not idempotent")
	}
}

func TestAustraliaUpgradeRefusesOutstandingLegacyOrder(t *testing.T) {
	s, path, _ := fundsFixture(t)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("rule_migrations")).Delete([]byte("au45-v2")); err != nil {
			return err
		}
		return put(tx, "orders", "old-au", Order{ID: "old-au", SessionID: "lhc-au", Status: "pending"})
	}))
	must(t, s.Close())
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("upgraded with outstanding old stake")
	}
}
