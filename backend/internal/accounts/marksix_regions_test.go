package accounts

import (
	bolt "go.etcd.io/bbolt"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestRegionalSettingsMigrationPreservesMacau(t *testing.T) {
	s, path, _ := fundsFixture(t)
	cfg, err := s.Settings()
	must(t, err)
	old := cfg
	old.Plays = nil
	old.Announcement = "keep existing announcement"
	for _, p := range cfg.Plays {
		g, _ := games.Find(p.SessionID)
		if g.GameID == "lhc" && g.ID != "lhc-daily" {
			continue
		}
		if g.ID == "lhc-daily" {
			p.Enabled = false
			p.UnitCents = 500
			p.UserIssueLimitCents = 333333
		}
		old.Plays = append(old.Plays, p)
	}
	book, err := s.MarketBook("lhc-daily")
	must(t, err)
	book.Quotes[0].OddsMilli = 1880
	must(t, s.SaveQuotes("admin", "lhc-daily", book.Quotes))
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "settings", "global", old) }))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer s.Close()
	got, err := s.Settings()
	must(t, err)
	if len(got.Plays) != len(games.All()) || got.Announcement != old.Announcement {
		t.Fatal("missing sessions or overwritten settings")
	}
	for _, p := range old.Plays {
		found := false
		for _, q := range got.Plays {
			if q.SessionID == p.SessionID {
				found = true
				if !reflect.DeepEqual(p, q) {
					t.Fatal("old play changed", p.SessionID)
				}
			}
		}
		if !found {
			t.Fatal("old play removed")
		}
	}
	after, err := s.MarketBook("lhc-daily")
	must(t, err)
	if !reflect.DeepEqual(book.Quotes, after.Quotes) {
		t.Fatal("Macau odds changed")
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}

func TestAllMarkSixRegionsBetAndSettleIndependently(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	now := time.Now().In(model.Beijing).Add(-48 * time.Hour).Truncate(time.Hour).Add(10 * time.Second)
	orders := []Order{}
	rounds := []games.Round{}
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			continue
		}
		book, err := s.marketBookAt(g, g.Next(now))
		must(t, err)
		marketID := "lhc6.special.big"
		if g.MarketCatalogIDAt(g.Next(now)) == "lhc-au" {
			marketID = "au45.p7.big"
		}
		var odds int64
		for _, q := range book.Quotes {
			if q.MarketID == marketID {
				odds = q.OddsMilli
			}
		}
		in := MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Key: randomID(), Selections: []MarketSelection{{MarketID: marketID, OddsMilli: odds}}}
		result, err := s.PlaceMarketBatch("member", in, now)
		must(t, err)
		orders = append(orders, result.Orders...)
		special := 30
		if len(orders)%2 == 0 {
			special = 10
		}
		numbers := []int{1, 2, 3, 4, 5, 6, special}
		if g.MarketCatalogIDAt(g.Next(now)) == "lhc-au" {
			numbers = append(numbers, 8)
		}
		rounds = append(rounds, marketRound(MarketOrderInput{SessionID: g.ID, Issue: in.Issue}, numbers))
	}
	if len(orders) != 8 {
		t.Fatal("need eight independent games")
	}
	assertFunds(t, s, "member", 92000, 8000, 0)
	if err := s.SettleMarket(orders[0].ID, rounds[1]); err == nil {
		t.Fatal("accepted other region's result")
	}
	for i, o := range orders {
		must(t, s.SettleMarket(o.ID, rounds[i]))
		must(t, s.SettleMarket(o.ID, rounds[i]))
	}
	var payout int64
	for i, o := range orders {
		if i%2 == 0 {
			payout += o.Cost * o.OddsMilli / 1000
		}
	}
	assertFunds(t, s, "member", 92000+payout, 0, 0)
}
