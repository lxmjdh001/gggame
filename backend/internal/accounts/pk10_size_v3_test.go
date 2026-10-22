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

func pk10SizeStoredQuotes(t *testing.T, s *Store, session string) []Quote {
	t.Helper()
	var quotes []Quote
	must(t, s.db.View(func(tx *bolt.Tx) error {
		var err error
		quotes, err = get[[]Quote](tx, "market_quotes", session)
		return err
	}))
	return quotes
}

func pk10SizeQuoteMap(quotes []Quote) map[string]Quote {
	out := map[string]Quote{}
	for _, q := range quotes {
		out[q.MarketID] = q
	}
	return out
}

func pk10SizeFinancialState(t *testing.T, s *Store) map[string]map[string]string {
	t.Helper()
	state := map[string]map[string]string{}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, bucket := range []string{"users", "orders", "ledger", "commissions", "requests", "keys", "pending_markets", issueStakeBucket} {
			state[bucket] = map[string]string{}
			if err := tx.Bucket([]byte(bucket)).ForEach(func(k, v []byte) error {
				state[bucket][string(k)] = string(v)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}))
	return state
}

func TestPK10SizeV3QuoteMigrationPreservesPricesSwitchesAndOtherMarkets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy []Quote
		want   []Quote
	}{
		{
			"v2 takes precedence over v1",
			[]Quote{{MarketID: "sum.big", OddsMilli: 1555, Enabled: true}, {MarketID: "sum.small", OddsMilli: 1666, Enabled: false}, {MarketID: "size2.sum.big", OddsMilli: 2110, Enabled: false}, {MarketID: "size2.sum.small", OddsMilli: 2330, Enabled: true}},
			[]Quote{{MarketID: "size3.sum.big", OddsMilli: 2110, Enabled: false}, {MarketID: "size3.sum.small", OddsMilli: 2330, Enabled: true}},
		},
		{
			"v1 fallback",
			[]Quote{{MarketID: "sum.big", OddsMilli: 1777, Enabled: false}, {MarketID: "sum.small", OddsMilli: 1888, Enabled: true}},
			[]Quote{{MarketID: "size3.sum.big", OddsMilli: 1777, Enabled: false}, {MarketID: "size3.sum.small", OddsMilli: 1888, Enabled: true}},
		},
		{
			"fallback is per selection",
			[]Quote{{MarketID: "sum.big", OddsMilli: 1555, Enabled: true}, {MarketID: "sum.small", OddsMilli: 1888, Enabled: false}, {MarketID: "size2.sum.big", OddsMilli: 2222, Enabled: true}},
			[]Quote{{MarketID: "size3.sum.big", OddsMilli: 2222, Enabled: true}, {MarketID: "size3.sum.small", OddsMilli: 1888, Enabled: false}},
		},
		{
			"existing v3 quote is never replaced",
			[]Quote{{MarketID: "size2.sum.big", OddsMilli: 2110, Enabled: true}, {MarketID: "size2.sum.small", OddsMilli: 2330, Enabled: false}, {MarketID: "size3.sum.big", OddsMilli: 2777, Enabled: false}},
			[]Quote{{MarketID: "size3.sum.big", OddsMilli: 2777, Enabled: false}, {MarketID: "size3.sum.small", OddsMilli: 2330, Enabled: false}},
		},
		{
			"defaults when no prior size quotes exist",
			nil,
			[]Quote{{MarketID: "size3.sum.big", OddsMilli: 1960, Enabled: true}, {MarketID: "size3.sum.small", OddsMilli: 1960, Enabled: true}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path, _ := fundsFixture(t)
			defer func() { s.Close() }()
			book, err := s.MarketBook("pk10-10")
			must(t, err)
			if len(book.Markets) != 167 || len(book.Quotes) != 167 {
				t.Fatal("expected 167 current PK10 markets and quotes")
			}
			originalMarkets := book.Markets
			otherBooks := map[string]MarketBook{}
			otherStored := map[string][]Quote{}
			for _, g := range games.All() {
				if g.ID == book.SessionID {
					continue
				}
				otherBooks[g.ID], err = s.MarketBook(g.ID)
				must(t, err)
				otherStored[g.ID] = pk10SizeStoredQuotes(t, s, g.ID)
			}
			legacy := []Quote{}
			unchanged := map[string]Quote{}
			for i, q := range book.Quotes {
				if strings.HasPrefix(q.MarketID, "size3.sum.") {
					continue
				}
				q.OddsMilli += int64(i + 1)
				q.Enabled = i%3 != 0
				unchanged[q.MarketID] = q
				legacy = append(legacy, q)
			}
			if len(unchanged) != 165 {
				t.Fatal("size upgrade must replace exactly two markets")
			}
			legacy = append(legacy, tc.legacy...)
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				return put(tx, "market_quotes", book.SessionID, legacy)
			}))
			settings, err := s.Settings()
			must(t, err)
			beforeFunds := pk10SizeFinancialState(t, s)
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			book, err = s.MarketBook("pk10-10")
			must(t, err)
			if len(book.Markets) != 167 || len(book.Quotes) != 167 || !reflect.DeepEqual(originalMarkets, book.Markets) {
				t.Fatal("migration changed the active catalog or exposed retired markets")
			}
			want := pk10SizeQuoteMap(tc.want)
			for id, q := range unchanged {
				want[id] = q
			}
			if !reflect.DeepEqual(want, pk10SizeQuoteMap(book.Quotes)) {
				t.Fatal("migration changed existing selections or failed to inherit size quotes")
			}
			stored := pk10SizeStoredQuotes(t, s, book.SessionID)
			if len(stored) < len(legacy) || !reflect.DeepEqual(stored[:len(legacy)], legacy) {
				t.Fatal("migration replaced or removed stored legacy quotes")
			}
			gotSettings, err := s.Settings()
			must(t, err)
			if !reflect.DeepEqual(settings, gotSettings) || !reflect.DeepEqual(beforeFunds, pk10SizeFinancialState(t, s)) {
				t.Fatal("quote migration changed settings or financial state")
			}

			// Admin changes are independent of the inherited retired quotes and
			// must survive every later startup, including disabled selections.
			for i := range book.Quotes {
				q := &book.Quotes[i]
				if q.MarketID == "size3.sum.big" {
					q.OddsMilli, q.Enabled = 2444, true
				}
				if q.MarketID == "size3.sum.small" {
					q.OddsMilli, q.Enabled = 2555, false
				}
			}
			must(t, s.SaveQuotes("admin", book.SessionID, book.Quotes))
			wantBook := book
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			book, err = s.MarketBook("pk10-10")
			must(t, err)
			if !reflect.DeepEqual(wantBook, book) {
				t.Fatal("restart replaced admin odds or enabled state")
			}
			storedByID := pk10SizeQuoteMap(pk10SizeStoredQuotes(t, s, book.SessionID))
			for _, q := range legacy {
				if strings.HasPrefix(q.MarketID, "size3.") {
					continue
				}
				if !reflect.DeepEqual(storedByID[q.MarketID], q) {
					t.Fatal("admin save changed an unrelated or retired quote", q)
				}
			}
			for id, want := range otherBooks {
				got, err := s.MarketBook(id)
				must(t, err)
				want.MarkSixContext, got.MarkSixContext = nil, nil
				if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(otherStored[id], pk10SizeStoredQuotes(t, s, id)) {
					t.Fatal("PK10 migration or admin save changed another game's markets or quotes", id)
				}
			}
			if !reflect.DeepEqual(beforeFunds, pk10SizeFinancialState(t, s)) {
				t.Fatal("saving size odds changed financial state")
			}
		})
	}
}

func TestPK10SizeV3LegacyAndCurrentSum11OrdersKeepSnapshotsAndPayOnce(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	g, _ := games.Find("pk10-10")
	now := time.Now().Add(-24 * time.Hour).Truncate(10 * time.Minute).Add(10 * time.Second)
	type accepted struct {
		order   Order
		input   MarketOrderInput
		outcome string
	}
	var orders []accepted
	for _, tc := range []struct {
		id, pick, outcome string
		version           int
		odds              int64
	}{
		{"sum.big", "big", "win", 1, 2175},
		{"sum.small", "small", "lose", 1, 2245},
		{"size2.sum.big", "big", "push", 2, 2375},
		{"size2.sum.small", "small", "push", 2, 2445},
		{"size3.sum.big", "big", "lose", 3, 2675},
		{"size3.sum.small", "small", "win", 3, 2745},
	} {
		book, err := s.MarketBook(g.ID)
		must(t, err)
		for i := range book.Quotes {
			if book.Quotes[i].MarketID == "size3.sum."+tc.pick {
				book.Quotes[i].OddsMilli = tc.odds
			}
		}
		must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: "size3.sum." + tc.pick, OddsMilli: tc.odds, UnitCents: 200, Multiplier: 1, Key: randomID()}
		o, err := s.PlaceMarketOrder("member", in, now)
		must(t, err)
		current, ok := markets.Find("pk10", in.MarketID)
		if !ok || o.RuleSnapshot == nil || !reflect.DeepEqual(*o.RuleSnapshot, current) || current.RuleVersion != 3 || current.SmallMax != 11 || current.BigMin != 12 || len(current.PushValues) != 0 {
			t.Fatal("current order did not lock the v3 size rule", o)
		}
		if tc.version < 3 {
			// Reserve real funds first, then recreate an accepted historical
			// order. The v1 fixture counts 11 as big; the v2 fixture pushes on 11.
			o.MarketID = tc.id
			snapshot := *o.RuleSnapshot
			snapshot.ID, snapshot.RuleVersion = tc.id, tc.version
			snapshot.SmallMax, snapshot.BigMin, snapshot.PushValues = 10, 11, nil
			snapshot.Rule = "前两名号码之和3–10小、11–19大。"
			if tc.version == 2 {
				snapshot.BigMin, snapshot.PushValues = 12, []int{11}
				snapshot.Rule = "前两名号码之和3–10小、11为和、12–19大；开11时仅大小退本金。"
			}
			o.RuleSnapshot = &snapshot
			in.MarketID, in.Key = tc.id, randomID()
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
				return remember(tx, "market_order:member", in.Key, o.ID, in)
			}))
		}
		orders = append(orders, accepted{o, in, tc.outcome})
	}
	// Recreate the pre-upgrade quote set while accepted old and new orders
	// remain pending. Startup must migrate quotes without rewriting orders.
	quotes := []Quote{}
	for _, q := range pk10SizeStoredQuotes(t, s, g.ID) {
		if !strings.HasPrefix(q.MarketID, "size3.") {
			quotes = append(quotes, q)
		}
	}
	quotes = append(quotes, Quote{MarketID: "size2.sum.big", OddsMilli: 1777, Enabled: false}, Quote{MarketID: "size2.sum.small", OddsMilli: 1888, Enabled: false})
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "market_quotes", g.ID, quotes) }))
	before := pk10SizeFinancialState(t, s)
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	if !reflect.DeepEqual(before, pk10SizeFinancialState(t, s)) {
		t.Fatal("startup migrated a pending order, rule snapshot, or financial record")
	}
	stake, payout := int64(len(orders)*200), int64(0)
	assertFunds(t, s, "member", 100000-stake, stake, 0)
	for _, accepted := range orders {
		stored, err := s.Order(accepted.order.ID)
		must(t, err)
		if !reflect.DeepEqual(stored, accepted.order) {
			t.Fatal("startup rewrote an accepted order", accepted.order.MarketID)
		}
		retry, err := s.PlaceMarketOrder("member", accepted.input, now)
		must(t, err)
		if retry.ID != accepted.order.ID {
			t.Fatal("idempotent retry replaced an order")
		}
		round := marketRound(accepted.input, pk10NumbersForSum(11))
		must(t, s.SettleMarket(accepted.order.ID, round))
		settledOnce := pk10SizeFinancialState(t, s)
		must(t, s.SettleMarket(accepted.order.ID, round))
		if !reflect.DeepEqual(settledOnce, pk10SizeFinancialState(t, s)) {
			t.Fatal("repeated settlement changed funds, ledger, commissions, or pending state")
		}
		settled, err := s.Order(accepted.order.ID)
		must(t, err)
		want := int64(0)
		if accepted.outcome == "win" {
			want = accepted.order.Cost * accepted.order.OddsMilli / 1000
		} else if accepted.outcome == "push" {
			want = accepted.order.Cost
		}
		if settled.Status != "settled" || settled.Outcome != accepted.outcome || settled.Payout != want || settled.OddsMilli != accepted.order.OddsMilli || !reflect.DeepEqual(settled.RuleSnapshot, accepted.order.RuleSnapshot) {
			t.Fatalf("sum 11 ignored locked version or odds for %s: %+v", accepted.order.MarketID, settled)
		}
		payout += want
	}
	assertFunds(t, s, "member", 100000-stake+payout, 0, 0)
	pending, err := s.PendingMarkets()
	must(t, err)
	if len(pending) != 0 {
		t.Fatal("settled size orders are still pending")
	}
}

func TestPK10SizeV3RejectsRetiredIDsAndMixedBatchesWithoutDebiting(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	g, _ := games.Find("pk10-10")
	now := time.Now().Add(-24 * time.Hour).Truncate(10 * time.Minute).Add(10 * time.Second)
	quotes := pk10SizeStoredQuotes(t, s, g.ID)
	retired := []string{"sum.big", "sum.small", "size2.sum.big", "size2.sum.small"}
	for _, id := range retired {
		quotes = append(quotes, Quote{MarketID: id, OddsMilli: 1960, Enabled: true})
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "market_quotes", g.ID, quotes) }))
	before := pk10SizeFinancialState(t, s)
	for _, id := range retired {
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: id, OddsMilli: 1960, UnitCents: 200, Multiplier: 1, Key: randomID()}
		if _, err := s.PlaceMarketOrder("member", in, now); err == nil || err.Error() != "玩法不存在" {
			t.Fatal("retired size ID accepted or rejected for the wrong reason", id, err)
		}
		// The valid selection comes first, so failure must roll back its debit.
		batch := MarketBatchInput{SessionID: g.ID, Issue: in.Issue, UnitCents: 200, AmountCents: 200, Key: randomID(), Selections: []MarketSelection{{MarketID: "size3.sum.small", OddsMilli: 1960}, {MarketID: id, OddsMilli: 1960}}}
		if _, err := s.PlaceMarketBatch("member", batch, now); err == nil || !strings.Contains(err.Error(), "玩法不存在") {
			t.Fatal("mixed batch containing retired size ID accepted", id, err)
		}
	}
	if !reflect.DeepEqual(before, pk10SizeFinancialState(t, s)) || !reflect.DeepEqual(quotes, pk10SizeStoredQuotes(t, s, g.ID)) {
		t.Fatal("rejected size orders changed funds, financial records, or quotes")
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}
