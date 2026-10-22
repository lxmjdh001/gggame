package accounts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func pk10AdditionalOdds() map[string]int64 {
	odds := []int64{39600, 39600, 19600, 19600, 12900, 12900, 9450, 9450, 7740, 9450, 9450, 12900, 12900, 19800, 19800, 39600, 39600}
	out := map[string]int64{"sum.set_3_4_18_19": 11800, "sum.set_5_6_16_17": 5400, "sum.set_7_8_14_15": 3600, "sum.set_9_10_12_13": 2700}
	for i, odds := range odds {
		out[fmt.Sprintf("sum.n%d", i+3)] = odds
	}
	return out
}

func TestPK10SumQuotesMigrateWithoutOverwritingExistingSettingsOrOrders(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	newOdds := pk10AdditionalOdds()
	book, err := s.MarketBook("pk10-10")
	must(t, err)
	for i := range book.Quotes {
		if book.Quotes[i].MarketID == "p1.big" {
			book.Quotes[i].OddsMilli = 1860
		}
		if book.Quotes[i].MarketID == "p1.small" {
			book.Quotes[i].Enabled = false
		}
	}
	must(t, s.SaveQuotes("admin", "pk10-10", book.Quotes))
	g, _ := games.Find("pk10-10")
	in := marketInput(now, "p1.big")
	in.SessionID, in.Issue, in.OddsMilli = g.ID, g.Next(now).Format(model.IssueLayout), 1860
	order, err := s.PlaceMarketOrder("member", in, now)
	must(t, err)
	orderJSON, err := json.Marshal(order)
	must(t, err)
	settings, err := s.Settings()
	must(t, err)
	oldQuotes := map[string]Quote{}
	otherQuotes := map[string][]Quote{}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var legacy []Quote
		for _, q := range book.Quotes {
			if _, added := newOdds[q.MarketID]; !added {
				legacy = append(legacy, q)
				oldQuotes[q.MarketID] = q
			}
		}
		for _, session := range games.All() {
			if session.ID == g.ID {
				continue
			}
			var err error
			otherQuotes[session.ID], err = get[[]Quote](tx, "market_quotes", session.ID)
			if err != nil {
				return err
			}
		}
		return put(tx, "market_quotes", g.ID, legacy)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	book, err = s.MarketBook(g.ID)
	must(t, err)
	if len(book.Quotes) != 167 || len(oldQuotes) != 146 {
		t.Fatal("missing old or new selections after migration")
	}
	for _, q := range book.Quotes {
		if want, old := oldQuotes[q.MarketID]; old {
			if !reflect.DeepEqual(q, want) {
				t.Fatal("old quote or enabled state changed", q, want)
			}
		} else if q.OddsMilli != newOdds[q.MarketID] || !q.Enabled {
			t.Fatal("new quote not initialized to its default", q)
		}
	}
	updatedSettings, err := s.Settings()
	must(t, err)
	if !reflect.DeepEqual(settings, updatedSettings) {
		t.Fatal("play settings changed during quote migration")
	}
	for id, want := range otherQuotes {
		var stored []Quote
		must(t, s.db.View(func(tx *bolt.Tx) error {
			var err error
			stored, err = get[[]Quote](tx, "market_quotes", id)
			return err
		}))
		if !reflect.DeepEqual(stored, want) {
			t.Fatal("another game's quotes changed", id)
		}
	}
	wantQuotes := map[string]Quote{}
	for i := range book.Quotes {
		q := &book.Quotes[i]
		if _, added := newOdds[q.MarketID]; added {
			q.OddsMilli += int64(i + 1)
			q.Enabled = i%2 == 0
		}
		wantQuotes[q.MarketID] = *q
	}
	// Each new quote is independently editable and survives subsequent startup.
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	// Also exercise a partial upgrade: only the missing quote may be initialized.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var partial []Quote
		for _, q := range book.Quotes {
			if q.MarketID != "sum.n19" {
				partial = append(partial, q)
			}
		}
		return put(tx, "market_quotes", g.ID, partial)
	}))
	wantQuotes["sum.n19"] = Quote{MarketID: "sum.n19", OddsMilli: 39600, Enabled: true}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	book, err = s.MarketBook(g.ID)
	must(t, err)
	for _, q := range book.Quotes {
		if !reflect.DeepEqual(q, wantQuotes[q.MarketID]) {
			t.Fatal("reopening replaced a customized quote", q)
		}
	}
	stored, err := s.Order(order.ID)
	must(t, err)
	storedJSON, err := json.Marshal(stored)
	must(t, err)
	if string(storedJSON) != string(orderJSON) {
		t.Fatal("migration rewrote an existing order or rule snapshot")
	}
	must(t, s.SettleMarket(order.ID, marketRound(in, []int{10, 1, 2, 3, 4, 5, 6, 7, 8, 9})))
	assertFunds(t, s, "member", 108600, 0, 0)
}

func pk10NumbersForSum(sum int) []int {
	for a := 1; a <= 10; a++ {
		b := sum - a
		if b < 1 || b > 10 || a == b {
			continue
		}
		numbers := []int{a, b}
		for n := 1; n <= 10; n++ {
			if n != a && n != b {
				numbers = append(numbers, n)
			}
		}
		return numbers
	}
	return nil
}

func TestPK10SumOrdersLockRulesAndOddsAndSettleOnce(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	g, _ := games.Find("pk10-10")
	base := time.Now().Add(-24 * time.Hour).Truncate(10 * time.Minute).Add(10 * time.Second)
	ids := []string{}
	for id := range pk10AdditionalOdds() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	type accepted struct {
		order   Order
		input   MarketOrderInput
		now     time.Time
		numbers []int
		outcome string
		payout  int64
	}
	var orders []accepted
	add := func(id string, numbers []int, outcome string) {
		t.Helper()
		m, ok := markets.Find("pk10", id)
		if !ok {
			t.Fatal("missing market", id)
		}
		now := base.Add(time.Duration(len(orders)) * 10 * time.Minute)
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: id, OddsMilli: m.DefaultOdds, UnitCents: 200, Multiplier: 1, Key: randomID()}
		o, err := s.PlaceMarketOrder("member", in, now)
		must(t, err)
		if o.RuleSnapshot == nil || !reflect.DeepEqual(*o.RuleSnapshot, m) || o.OddsMilli != m.DefaultOdds {
			t.Fatal("new order did not lock its rule and quote", o)
		}
		payout := int64(0)
		if outcome == "win" {
			payout = 200 * m.DefaultOdds / 1000
		} else if outcome == "push" {
			payout = 200
		}
		orders = append(orders, accepted{o, in, now, numbers, outcome, payout})
	}
	for _, id := range ids {
		m, _ := markets.Find("pk10", id)
		sum, err := strconv.Atoi(strings.Split(m.Pick, ",")[0])
		must(t, err)
		add(id, pk10NumbersForSum(sum), "win")
	}
	// A sum of 11 never pushes a number/set selection; current size rules count it as small.
	for _, id := range []string{"sum.set_3_4_18_19", "sum.set_5_6_16_17", "sum.set_7_8_14_15", "sum.set_9_10_12_13"} {
		add(id, pk10NumbersForSum(11), "lose")
	}
	for _, tc := range []struct{ id, outcome string }{
		{"size3.sum.big", "lose"}, {"size3.sum.small", "win"}, {"sum.odd", "win"}, {"sum.even", "lose"},
		{"dt1.dragon", "lose"}, {"dt1.tiger", "win"}, {"p1.n1", "win"},
	} {
		add(tc.id, pk10NumbersForSum(11), tc.outcome)
	}
	book, err := s.MarketBook(g.ID)
	must(t, err)
	for i := range book.Quotes {
		if _, added := pk10AdditionalOdds()[book.Quotes[i].MarketID]; added {
			book.Quotes[i].OddsMilli += 1500
		}
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	stale := orders[0].input
	stale.Key = randomID()
	if _, err := s.PlaceMarketOrder("member", stale, orders[0].now); err == nil {
		t.Fatal("new order accepted a stale quote")
	}
	for i := range book.Quotes {
		if _, added := pk10AdditionalOdds()[book.Quotes[i].MarketID]; added {
			book.Quotes[i].Enabled = false
		}
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	stale.Key, stale.OddsMilli = randomID(), stale.OddsMilli+1500
	if _, err := s.PlaceMarketOrder("member", stale, orders[0].now); err == nil {
		t.Fatal("new order accepted a disabled selection")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	retry, err := s.PlaceMarketOrder("member", orders[0].input, orders[0].now)
	must(t, err)
	if retry.ID != orders[0].order.ID {
		t.Fatal("retry replaced the original locked order")
	}
	totalStake, totalPayout := int64(len(orders)*200), int64(0)
	assertFunds(t, s, "member", 100000-totalStake, totalStake, 0)
	for _, accepted := range orders {
		round := marketRound(accepted.input, accepted.numbers)
		must(t, s.SettleMarket(accepted.order.ID, round))
		must(t, s.SettleMarket(accepted.order.ID, round))
		settled, err := s.Order(accepted.order.ID)
		must(t, err)
		if settled.Status != "settled" || settled.OddsMilli != accepted.order.OddsMilli || settled.Outcome != accepted.outcome || settled.Payout != accepted.payout {
			t.Fatal("locked order settled incorrectly or paid more than once", settled, accepted.outcome, accepted.payout)
		}
		totalPayout += accepted.payout
	}
	assertFunds(t, s, "member", 100000-totalStake+totalPayout, 0, 0)
}

func TestPK10SumSetsCannotBeOrderedForOtherGames(t *testing.T) {
	s, _, now := fundsFixture(t)
	defer s.Close()
	for _, g := range games.All() {
		if g.GameID == "pk10" {
			continue
		}
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: "sum.set_3_4_18_19", OddsMilli: 11800, UnitCents: 200, Multiplier: 1, Key: randomID()}
		if _, err := s.PlaceMarketOrder("member", in, now); err == nil || err.Error() != "玩法不存在" {
			t.Fatal("PK10 set accepted for another game", g.ID, err)
		}
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}
