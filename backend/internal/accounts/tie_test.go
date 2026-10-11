package accounts

import (
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"testing"
	"time"
)

func TestTieSettlementAndOddsSnapshot(t *testing.T) {
	s, a := setup(t)
	parent := register(t, s, "tie_parent", "")
	u := register(t, s, "tie_child", parent.InviteCode)
	must(t, s.Adjust(a.ID, u.ID, 50000, "测试资金", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	cfg.CommissionBPS = [3]int64{500, 0, 0}
	must(t, s.SaveSettings(a.ID, cfg))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	in := marketInput(now, "dt.tie")
	in.OddsMilli = 8000
	o, e := s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	book, e := s.MarketBook("ffc-1")
	must(t, e)
	for i, q := range book.Quotes {
		if q.MarketID == "dt.tie" {
			book.Quotes[i].OddsMilli = 7500
		}
	}
	must(t, s.SaveQuotes(a.ID, "ffc-1", book.Quotes))
	r := marketRound(in, []int{4, 1, 2, 3, 4})
	must(t, s.SettleMarket(o.ID, r))
	must(t, s.SettleMarket(o.ID, r))
	o, e = s.Order(o.ID)
	must(t, e)
	if o.Outcome != "win" || o.Payout != 80000 || o.OddsMilli != 8000 {
		t.Fatalf("tie payout/snapshot incorrect: %+v", o)
	}
	account, e := s.Account(u.ID)
	must(t, e)
	if account["user"].(User).Balance != 120000 {
		t.Fatal("incorrect or duplicate payout")
	}
	p, e := s.Account(parent.ID)
	must(t, e)
	if p["commission_available_cents"].(int64) != 500 {
		t.Fatal("tie win commission incorrect")
	}
	in.Key = randomID()
	if _, e = s.PlaceMarketOrder(u.ID, in, now); e == nil {
		t.Fatal("stale tie odds accepted")
	}
	in.OddsMilli = 7500
	o, e = s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	must(t, s.SettleMarket(o.ID, marketRound(in, []int{4, 1, 2, 3, 5})))
	o, e = s.Order(o.ID)
	must(t, e)
	if o.Outcome != "lose" || o.Payout != 0 {
		t.Fatal("unequal numbers paid tie")
	}
	p, e = s.Account(parent.ID)
	must(t, e)
	if p["commission_available_cents"].(int64) != 1000 {
		t.Fatal("tie loss commission incorrect")
	}
	for i, q := range book.Quotes {
		if q.MarketID == "dt.tie" {
			book.Quotes[i].Enabled = false
		}
	}
	must(t, s.SaveQuotes(a.ID, "ffc-1", book.Quotes))
	in.Key = randomID()
	if _, e = s.PlaceMarketOrder(u.ID, in, now); e == nil {
		t.Fatal("disabled tie accepted")
	}
}

func TestMissingQuoteMigrationPreservesSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range []string{"ffc-1", "ffc-3", "ffc-5"} {
			quotes, e := get[[]Quote](tx, "market_quotes", id)
			if e != nil {
				return e
			}
			old := []Quote{}
			for _, q := range quotes {
				if q.MarketID == "dt.tie" {
					continue
				}
				if q.MarketID == "dt.dragon" {
					q.OddsMilli = 1850
					q.Enabled = false
				}
				old = append(old, q)
			}
			if e = put(tx, "market_quotes", id, old); e != nil {
				return e
			}
		}
		return nil
	}))
	must(t, s.Close())
	for round := 0; round < 2; round++ {
		s, e = Open(path)
		must(t, e)
		for _, id := range []string{"ffc-1", "ffc-3", "ffc-5"} {
			book, e := s.MarketBook(id)
			must(t, e)
			ties := 0
			for _, q := range book.Quotes {
				if q.MarketID == "dt.tie" {
					ties++
					if q.OddsMilli != 8000 || !q.Enabled {
						t.Fatal("tie defaults incorrect")
					}
				}
				if q.MarketID == "dt.dragon" && (q.OddsMilli != 1850 || q.Enabled) {
					t.Fatal("existing configuration overwritten")
				}
			}
			if ties != 1 || len(book.Quotes) != len(book.Markets) {
				t.Fatal("missing or duplicate quotes")
			}
		}
		must(t, s.Close())
	}
}
