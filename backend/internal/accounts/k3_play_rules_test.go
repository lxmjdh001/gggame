package accounts

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func k3CurrentIDs() []string {
	ids := []string{"k3v2.sum.big", "k3v2.sum.small", "k3v2.sum.odd", "k3v2.sum.even"}
	for n := 1; n <= 6; n++ {
		ids = append(ids, fmt.Sprintf("k3v3.single.n%d", n))
	}
	for n := 1; n <= 6; n++ {
		ids = append(ids, fmt.Sprintf("k3v2.triple.n%d", n))
	}
	ids = append(ids, "k3v2.triple.any")
	for n := 4; n <= 17; n++ {
		ids = append(ids, fmt.Sprintf("k3v2.sum.n%d", n))
	}
	return ids
}

func k3LegacyQuotes() []Quote {
	quotes := []Quote{}
	for p := 1; p <= 3; p++ {
		for _, pick := range []string{"big", "small", "odd", "even"} {
			quotes = append(quotes, Quote{MarketID: fmt.Sprintf("p%d.%s", p, pick), OddsMilli: 1960, Enabled: true})
		}
		for n := 1; n <= 6; n++ {
			quotes = append(quotes, Quote{MarketID: fmt.Sprintf("p%d.n%d", p, n), OddsMilli: 5880, Enabled: true})
		}
	}
	for _, pick := range []string{"big", "small", "odd", "even"} {
		quotes = append(quotes, Quote{MarketID: "sum." + pick, OddsMilli: 1960, Enabled: true})
	}
	ways := [19]int64{}
	for a := 1; a <= 6; a++ {
		for b := 1; b <= 6; b++ {
			for c := 1; c <= 6; c++ {
				ways[a+b+c]++
			}
		}
	}
	for n := 3; n <= 18; n++ {
		quotes = append(quotes, Quote{MarketID: fmt.Sprintf("sum.n%d", n), OddsMilli: 211680 / ways[n], Enabled: true})
	}
	// Preserve customized prices and disabled selections as well as defaults.
	quotes[0].OddsMilli, quotes[1].Enabled = 1810, false
	return quotes
}

func k3StoredQuotes(t *testing.T, s *Store, id string) []Quote {
	t.Helper()
	var quotes []Quote
	must(t, s.db.View(func(tx *bolt.Tx) error {
		var err error
		quotes, err = get[[]Quote](tx, "market_quotes", id)
		return err
	}))
	return quotes
}

func k3FinancialState(t *testing.T, s *Store) map[string]map[string]string {
	t.Helper()
	state := map[string]map[string]string{}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, bucket := range []string{"users", "orders", "ledger", "commissions", "requests", "pending_markets", issueStakeBucket} {
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

func TestK3V2QuotesMigrateAndPreserveLegacyOrders(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	legacy := k3LegacyQuotes()
	otherQuotes := map[string][]Quote{}
	var sessions []games.Session
	for _, g := range games.All() {
		if g.GameID != "k3" {
			otherQuotes[g.ID] = k3StoredQuotes(t, s, g.ID)
			continue
		}
		sessions = append(sessions, g)
		book, err := s.MarketBook(g.ID)
		must(t, err)
		if len(book.Markets) != 31 || len(book.Quotes) != 31 {
			t.Fatalf("%s: expected exactly 31 current markets and quotes", g.ID)
		}
		for _, q := range book.Quotes {
			if q.OddsMilli != 1000 || !q.Enabled {
				t.Fatalf("%s: initial odds must be 1.000 and enabled: %+v", g.ID, q)
			}
		}
	}
	if len(sessions) != 9 {
		t.Fatalf("expected nine K3 sessions, got %d", len(sessions))
	}

	// Reserve real funds, then recreate persisted version-1 orders from before
	// the catalog upgrade. Their snapshots must settle independently of Catalog.
	type oldOrder struct {
		order   Order
		input   MarketOrderInput
		numbers []int
	}
	var oldOrders []oldOrder
	g := sessions[0]
	for _, tc := range []struct {
		id, kind, pick string
		positions      []int
		numbers        []int
	}{
		{"sum.big", "size", "big", []int{1, 2, 3}, []int{6, 6, 6}},
		{"sum.small", "size", "small", []int{1, 2, 3}, []int{1, 1, 1}},
		{"sum.odd", "parity", "odd", []int{1, 2, 3}, []int{1, 1, 1}},
		{"sum.even", "parity", "even", []int{1, 2, 3}, []int{6, 6, 6}},
		{"sum.n3", "number", "3", []int{1, 2, 3}, []int{1, 1, 1}},
		{"sum.n18", "number", "18", []int{1, 2, 3}, []int{6, 6, 6}},
		{"p1.big", "size", "big", []int{1}, []int{6, 1, 2}},
		{"p1.n6", "number", "6", []int{1}, []int{6, 1, 2}},
	} {
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: "k3v2.sum.big", OddsMilli: 1000, UnitCents: 200, Multiplier: 1, Key: randomID()}
		o, err := s.PlaceMarketOrder("member", in, now)
		must(t, err)
		for _, q := range legacy {
			if q.MarketID == tc.id {
				o.OddsMilli = q.OddsMilli
			}
		}
		o.MarketID, o.MarketName = tc.id, "旧版快三 · "+tc.id
		o.RuleSnapshot = &markets.Market{ID: tc.id, Group: "总和", Label: tc.pick, Category: "two_sides", Kind: tc.kind, Positions: tc.positions, Pick: tc.pick, SmallMax: 10, BigMin: 11, Rule: "三颗骰子和值3–10小、11–18大；单双按和值奇偶，本项目不设豹子通吃。", RuleVersion: 1, DefaultOdds: o.OddsMilli}
		if len(tc.positions) == 1 {
			o.RuleSnapshot.Group = "第1球"
			o.RuleSnapshot.SmallMax, o.RuleSnapshot.BigMin = 3, 4
			o.RuleSnapshot.Rule = "骰子单球1–3为小，4–6为大；按点数奇偶判单双。"
		}
		if tc.kind == "number" {
			o.RuleSnapshot.Category = "regular"
			o.RuleSnapshot.SmallMax, o.RuleSnapshot.BigMin = 0, 0
			o.RuleSnapshot.Rule = "仅比较所选位置的开奖号码，其他位置不影响本注。"
			if len(tc.positions) == 3 {
				o.RuleSnapshot.Group, o.RuleSnapshot.Rule = "和值数字", "三个骰子的点数之和与所选和值相同即中奖。"
			}
		}
		in.MarketID, in.OddsMilli, in.Key = tc.id, o.OddsMilli, randomID()
		must(t, s.db.Update(func(tx *bolt.Tx) error {
			if err := put(tx, "orders", o.ID, o); err != nil {
				return err
			}
			return remember(tx, "market_order:member", in.Key, o.ID, in)
		}))
		oldOrders = append(oldOrders, oldOrder{o, in, tc.numbers})
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, g := range sessions {
			if err := put(tx, "market_quotes", g.ID, legacy); err != nil {
				return err
			}
		}
		return nil
	}))
	settings, err := s.Settings()
	must(t, err)
	before := k3FinancialState(t, s)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	if !reflect.DeepEqual(before, k3FinancialState(t, s)) {
		t.Fatal("catalog migration changed orders, balances, reservations, or ledger")
	}
	gotSettings, err := s.Settings()
	must(t, err)
	if !reflect.DeepEqual(settings, gotSettings) {
		t.Fatal("catalog migration changed game settings")
	}

	wantQuotes := map[string][]Quote{}
	for sessionIndex, g := range sessions {
		stored := k3StoredQuotes(t, s, g.ID)
		if len(stored) != len(legacy)+31 || !reflect.DeepEqual(stored[:len(legacy)], legacy) {
			t.Fatalf("%s: migration did not retain old quotes unchanged", g.ID)
		}
		book, err := s.MarketBook(g.ID)
		must(t, err)
		if len(book.Markets) != 31 || len(book.Quotes) != 31 {
			t.Fatalf("%s: retired markets or quotes are still offered", g.ID)
		}
		seen := map[string]bool{}
		for i := range book.Quotes {
			q := &book.Quotes[i]
			if q.OddsMilli != 1000 || !q.Enabled || seen[q.MarketID] {
				t.Fatalf("%s: bad initialized quote %+v", g.ID, *q)
			}
			seen[q.MarketID] = true
			q.OddsMilli += int64(100*sessionIndex + i + 1)
			q.Enabled = i%3 != 0
		}
		for _, id := range k3CurrentIDs() {
			if !seen[id] {
				t.Fatalf("%s: missing new market %s", g.ID, id)
			}
		}
		must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
		wantQuotes[g.ID] = book.Quotes
	}
	if !reflect.DeepEqual(before, k3FinancialState(t, s)) {
		t.Fatal("saving odds changed financial state")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	for _, g := range sessions {
		book, err := s.MarketBook(g.ID)
		must(t, err)
		if !reflect.DeepEqual(wantQuotes[g.ID], book.Quotes) {
			t.Fatalf("%s: customized odds or disabled selections changed after restart", g.ID)
		}
		stored := map[string]Quote{}
		for _, q := range k3StoredQuotes(t, s, g.ID) {
			stored[q.MarketID] = q
		}
		for _, q := range legacy {
			if !reflect.DeepEqual(stored[q.MarketID], q) {
				t.Fatalf("%s: saving new odds changed or removed legacy quote %+v", g.ID, q)
			}
		}
	}
	for id, want := range otherQuotes {
		if !reflect.DeepEqual(want, k3StoredQuotes(t, s, id)) {
			t.Fatalf("K3 migration or odds update changed another game's quotes: %s", id)
		}
	}
	legacyStake := int64(len(oldOrders) * 200)
	assertFunds(t, s, "member", 100000-legacyStake, legacyStake, 0)
	var payout int64
	for _, old := range oldOrders {
		stored, err := s.Order(old.order.ID)
		must(t, err)
		if !reflect.DeepEqual(stored, old.order) {
			t.Fatalf("old order or rule snapshot changed: %s", old.order.ID)
		}
		retry, err := s.PlaceMarketOrder("member", old.input, now)
		must(t, err)
		if retry.ID != old.order.ID {
			t.Fatal("legacy idempotent retry created another order")
		}
		round := marketRound(old.input, old.numbers)
		must(t, s.SettleMarket(old.order.ID, round))
		must(t, s.SettleMarket(old.order.ID, round))
		settled, err := s.Order(old.order.ID)
		must(t, err)
		want := old.order.Cost * old.order.OddsMilli / 1000
		if settled.Outcome != "win" || settled.Payout != want || settled.OddsMilli != old.order.OddsMilli || !reflect.DeepEqual(settled.RuleSnapshot, old.order.RuleSnapshot) {
			t.Fatalf("legacy triple/position order did not retain old settlement: %+v", settled)
		}
		payout += want
	}
	assertFunds(t, s, "member", 100000-legacyStake+payout, 0, 0)
}

func k3NumbersForSum(sum int) []int {
	for a := 1; a <= 6; a++ {
		for b := 1; b <= 6; b++ {
			c := sum - a - b
			if c >= 1 && c <= 6 {
				return []int{a, b, c}
			}
		}
	}
	return nil
}

func TestK3V2OrdersLockRulesAndOddsAndSettleOnce(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	g, _ := games.Find("k3-1")
	now := time.Now().Add(-24 * time.Hour).Truncate(time.Hour).Add(10 * time.Second)
	book, err := s.MarketBook(g.ID)
	must(t, err)
	for i := range book.Quotes {
		book.Quotes[i].OddsMilli = 2345
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	type accepted struct {
		order   Order
		input   MarketOrderInput
		numbers []int
		outcome string
	}
	var orders []accepted
	add := func(id string, numbers []int, outcome string) {
		t.Helper()
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: id, OddsMilli: 2345, UnitCents: 200, Multiplier: 1, Key: randomID()}
		o, err := s.PlaceMarketOrder("member", in, now)
		must(t, err)
		m, ok := markets.Find("k3", id)
		if !ok || o.RuleSnapshot == nil || o.RuleSnapshot.RuleVersion != m.RuleVersion || !reflect.DeepEqual(*o.RuleSnapshot, m) || o.OddsMilli != 2345 {
			t.Fatalf("new order failed to lock current rules and selected odds: %+v", o)
		}
		orders = append(orders, accepted{o, in, numbers, outcome})
	}
	for _, id := range k3CurrentIDs() {
		numbers := []int{2, 3, 6}
		switch {
		case id == "k3v2.sum.small" || id == "k3v2.sum.even":
			numbers = []int{1, 3, 6}
		case id == "k3v2.triple.any":
			numbers = []int{3, 3, 3}
		case strings.HasPrefix(id, "k3v3.single.n"):
			n, err := strconv.Atoi(strings.TrimPrefix(id, "k3v3.single.n"))
			must(t, err)
			numbers = []int{n, n, n%6 + 1}
		case strings.HasPrefix(id, "k3v2.triple.n"):
			n, err := strconv.Atoi(strings.TrimPrefix(id, "k3v2.triple.n"))
			must(t, err)
			numbers = []int{n, n, n}
		case strings.HasPrefix(id, "k3v2.sum.n"):
			n, err := strconv.Atoi(strings.TrimPrefix(id, "k3v2.sum.n"))
			must(t, err)
			numbers = k3NumbersForSum(n)
		}
		add(id, numbers, "win")
	}
	for _, id := range []string{"k3v2.sum.big", "k3v2.sum.small", "k3v2.sum.odd", "k3v2.sum.even"} {
		add(id, []int{1, 1, 1}, "lose")
		add(id, []int{6, 6, 6}, "lose")
	}
	add("k3v2.triple.n3", []int{2, 2, 2}, "lose")
	add("k3v2.triple.n3", []int{3, 3, 2}, "lose")
	add("k3v2.triple.any", []int{1, 1, 2}, "lose")
	add("k3v2.sum.n9", []int{3, 3, 3}, "win")
	add("k3v2.sum.n9", []int{3, 3, 4}, "lose")
	for n := 1; n <= 6; n++ {
		other := n%6 + 1
		// Repeated matching dice pay one locked payout, rather than one per die.
		add(fmt.Sprintf("k3v3.single.n%d", n), []int{other, n, other}, "win")
		add(fmt.Sprintf("k3v3.single.n%d", n), []int{other, other, other}, "lose")
		add(fmt.Sprintf("k3v3.single.n%d", n), []int{n, n, n}, "lose")
	}
	for i := range book.Quotes {
		book.Quotes[i].OddsMilli = 9876
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	stale := orders[0].input
	stale.Key = randomID()
	if _, err := s.PlaceMarketOrder("member", stale, now); err == nil || err.Error() != "赔率已变更，请刷新并重新确认" {
		t.Fatal("accepted stale K3 odds", err)
	}
	for i := range book.Quotes {
		book.Quotes[i].Enabled = false
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	stale.Key, stale.OddsMilli = randomID(), 9876
	if _, err := s.PlaceMarketOrder("member", stale, now); err == nil || err.Error() != "该投注选项已暂停" {
		t.Fatal("accepted disabled K3 selection", err)
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	retry, err := s.PlaceMarketOrder("member", orders[0].input, now)
	must(t, err)
	if retry.ID != orders[0].order.ID {
		t.Fatal("retry changed the original locked order")
	}
	stake, payout := int64(len(orders)*200), int64(0)
	assertFunds(t, s, "member", 100000-stake, stake, 0)
	for _, accepted := range orders {
		round := marketRound(accepted.input, accepted.numbers)
		must(t, s.SettleMarket(accepted.order.ID, round))
		must(t, s.SettleMarket(accepted.order.ID, round))
		settled, err := s.Order(accepted.order.ID)
		must(t, err)
		want := int64(0)
		if accepted.outcome == "win" {
			want = 469
		}
		if settled.Status != "settled" || settled.Outcome != accepted.outcome || settled.Payout != want || settled.OddsMilli != 2345 || !reflect.DeepEqual(settled.RuleSnapshot, accepted.order.RuleSnapshot) {
			t.Fatalf("wrong locked settlement for %s: %+v", accepted.input.MarketID, settled)
		}
		payout += want
	}
	assertFunds(t, s, "member", 100000-stake+payout, 0, 0)
	pending, err := s.PendingMarkets()
	must(t, err)
	if len(pending) != 0 {
		t.Fatal("settled K3 orders remained pending")
	}
}

func TestK3V2RejectsRetiredAndCrossGameSelectionsWithoutDebiting(t *testing.T) {
	s, _, now := fundsFixture(t)
	defer s.Close()
	quotesBefore := map[string][]Quote{}
	for _, g := range games.All() {
		quotes := k3StoredQuotes(t, s, g.ID)
		if g.GameID == "k3" {
			// A stored quote from an old page must not authorize a retired ID.
			quotes = append(quotes, k3LegacyQuotes()...)
			for n := 1; n <= 6; n++ {
				quotes = append(quotes, Quote{MarketID: fmt.Sprintf("k3v2.single.n%d", n), OddsMilli: 1000, Enabled: true}, Quote{MarketID: fmt.Sprintf("k3v2.double.n%d", n), OddsMilli: 1000, Enabled: true})
			}
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				return put(tx, "market_quotes", g.ID, quotes)
			}))
		}
		quotesBefore[g.ID] = quotes
	}
	before := k3FinancialState(t, s)
	for _, g := range games.All() {
		ids := k3CurrentIDs()
		if g.GameID == "k3" {
			ids = []string{"k3v2.sum.n3", "k3v2.sum.n18"}
			for n := 1; n <= 6; n++ {
				ids = append(ids, fmt.Sprintf("k3v2.single.n%d", n), fmt.Sprintf("k3v2.double.n%d", n))
			}
			for _, q := range k3LegacyQuotes() {
				ids = append(ids, q.MarketID)
			}
		}
		for _, id := range ids {
			in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: id, OddsMilli: 1000, UnitCents: 200, Multiplier: 1, Key: randomID()}
			if _, err := s.PlaceMarketOrder("member", in, now); err == nil || err.Error() != "玩法不存在" {
				t.Fatalf("%s accepted unavailable selection %s: %v", g.ID, id, err)
			}
			if g.GameID == "k3" {
				batch := MarketBatchInput{SessionID: g.ID, Issue: in.Issue, UnitCents: 200, AmountCents: 200, Key: randomID(), Selections: []MarketSelection{{MarketID: "k3v2.sum.big", OddsMilli: 1000}, {MarketID: id, OddsMilli: 1960}}}
				if _, err := s.PlaceMarketBatch("member", batch, now); err == nil || !strings.Contains(err.Error(), "玩法不存在") {
					t.Fatalf("%s accepted mixed batch containing retired %s: %v", g.ID, id, err)
				}
			}
		}
	}
	if !reflect.DeepEqual(before, k3FinancialState(t, s)) {
		t.Fatal("rejected selections changed orders, funds, or reservations")
	}
	for id, want := range quotesBefore {
		if !reflect.DeepEqual(want, k3StoredQuotes(t, s, id)) {
			t.Fatalf("rejected selections changed quotes for %s", id)
		}
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}
