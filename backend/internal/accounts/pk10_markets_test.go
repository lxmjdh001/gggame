package accounts

import (
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func TestPK10OnlyFirstLastDragonTiger(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	// Simulate an existing database with retired quotes and custom odds.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		quotes, err := get[[]Quote](tx, "market_quotes", "pk10-10")
		if err != nil {
			return err
		}
		for i := range quotes {
			if quotes[i].MarketID == "dt1.dragon" {
				quotes[i].OddsMilli = 1850
			}
			if quotes[i].MarketID == "dt1.tiger" {
				quotes[i].Enabled = false
			}
		}
		for i := 2; i <= 5; i++ {
			for _, pick := range []string{"dragon", "tiger"} {
				quotes = append(quotes, Quote{MarketID: fmt.Sprintf("dt%d.%s", i, pick), OddsMilli: 1960, Enabled: true})
			}
		}
		return put(tx, "market_quotes", "pk10-10", quotes)
	}))
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	book, err := s.MarketBook("pk10-10")
	must(t, err)
	comparisons := 0
	for _, m := range book.Markets {
		if m.Kind == "compare" {
			comparisons++
			if len(m.Positions) != 2 || m.Positions[0] != 1 || m.Positions[1] != 10 {
				t.Fatal("unexpected comparison", m)
			}
		}
	}
	if comparisons != 2 || len(book.Markets) != len(book.Quotes) {
		t.Fatal("invalid market book")
	}
	for _, q := range book.Quotes {
		if _, ok := markets.Find("pk10", q.MarketID); !ok {
			t.Fatal("retired quote exposed", q)
		}
		if q.MarketID == "dt1.dragon" && q.OddsMilli != 1850 {
			t.Fatal("custom odds lost")
		}
		if q.MarketID == "dt1.tiger" && q.Enabled {
			t.Fatal("disabled selection enabled")
		}
	}
	must(t, s.SaveQuotes("admin", "pk10-10", book.Quotes))
	g, _ := games.Find("pk10-10")
	in := marketInput(now, "dt1.dragon")
	in.SessionID, in.Issue, in.OddsMilli = g.ID, g.Next(now).Format(model.IssueLayout), 1850
	for i := 2; i <= 5; i++ {
		for _, pick := range []string{"dragon", "tiger"} {
			removed := in
			removed.MarketID = fmt.Sprintf("dt%d.%s", i, pick)
			if _, err := s.PlaceMarketOrder("member", removed, now); err == nil {
				t.Fatal("retired market accepted")
			}
			batch := MarketBatchInput{SessionID: g.ID, Issue: in.Issue, UnitCents: 200, AmountCents: 10000, Key: randomID(), Selections: []MarketSelection{{MarketID: "dt1.dragon", OddsMilli: 1850}, {MarketID: removed.MarketID, OddsMilli: 1960}}}
			if _, err := s.PlaceMarketBatch("member", batch, now); err == nil {
				t.Fatal("retired batch accepted")
			}
		}
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	o, err := s.PlaceMarketOrder("member", in, now)
	must(t, err)
	must(t, s.SettleMarket(o.ID, marketRound(in, []int{10, 2, 3, 4, 5, 6, 7, 8, 9, 1})))
	assertFunds(t, s, "member", 108500, 0, 0)
	// Existing removed selections still settle from their stored rule snapshot.
	old, err := s.Order(o.ID)
	must(t, err)
	old.MarketID = "dt2.dragon"
	snapshot := *o.RuleSnapshot
	snapshot.ID, snapshot.Positions = old.MarketID, []int{2, 9}
	old.RuleSnapshot = &snapshot
	outcome, payout, err := calculateMarket(old, marketRound(in, []int{1, 10, 3, 4, 5, 6, 7, 8, 2, 9}))
	must(t, err)
	if outcome != "win" || payout != 18500 {
		t.Fatal("retired snapshot cannot settle", outcome, payout)
	}
	// Retrying an already accepted retired order must not submit or debit again.
	oldInput := in
	oldInput.MarketID, oldInput.Key = old.MarketID, randomID()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := put(tx, "orders", old.ID, old); err != nil {
			return err
		}
		return remember(tx, "market_order:member", oldInput.Key, old.ID, oldInput)
	}))
	retry, err := s.PlaceMarketOrder("member", oldInput, now)
	must(t, err)
	if retry.ID != old.ID {
		t.Fatal("retry changed order")
	}
	assertFunds(t, s, "member", 108500, 0, 0)
}
