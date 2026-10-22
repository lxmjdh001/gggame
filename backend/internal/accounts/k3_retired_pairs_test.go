package accounts

import (
	"encoding/json"
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

// Literal historical contracts keep the retirement test independent from the
// catalog offered to new orders. Both generations of short pairs are retained.
func k3RetiredPairMarkets() []markets.Market {
	var result []markets.Market
	for a := 1; a <= 5; a++ {
		for b := a + 1; b <= 6; b++ {
			result = append(result, markets.Market{ID: fmt.Sprintf("k3v2.pair.%d_%d", a, b), Group: "长牌", Label: fmt.Sprintf("%d-%d", a, b), Category: "regular", Kind: "dice_pair", Positions: []int{1, 2, 3}, Pick: fmt.Sprintf("%d,%d", a, b), Rule: "两个不同点数同时出现即中奖。", RuleVersion: 2, DefaultOdds: 1000})
		}
	}
	for _, version := range []int{2, 3} {
		for n := 1; n <= 6; n++ {
			result = append(result, markets.Market{ID: fmt.Sprintf("k3v%d.double.n%d", version, n), Group: "短牌", Label: fmt.Sprintf("%d-%d", n, n), Category: "regular", Kind: "dice_double", Positions: []int{1, 2, 3}, Pick: strconv.Itoa(n), Rule: "按接受投注时的短牌规则判奖。", RuleVersion: version, DefaultOdds: 1000})
		}
	}
	return result
}

func TestK3RetiredV3DoubleRecalculatesChangedResultAfterPreparedRestart(t *testing.T) {
	for _, tc := range []struct {
		name               string
		preview, published []int
		want               string
		payout             int64
	}{
		{"triple changes to pair", []int{2, 2, 2}, []int{2, 2, 3}, "win", 200},
		{"pair changes to triple", []int{2, 2, 3}, []int{2, 2, 2}, "lose", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path, _ := fundsFixture(t)
			defer func() { s.Close() }()
			g, _ := games.Find("k3-1")
			now := time.Now().Add(-24 * time.Hour).Truncate(time.Hour).Add(10 * time.Second)
			at := g.Next(now)
			in := MarketOrderInput{SessionID: g.ID, Issue: at.Format(model.IssueLayout), MarketID: "k3v3.single.n2", OddsMilli: 1000, UnitCents: 200, Multiplier: 1, Key: randomID()}
			o, err := s.PlaceMarketOrder("member", in, now)
			must(t, err)
			for _, m := range k3RetiredPairMarkets() {
				if m.ID == "k3v3.double.n2" {
					snapshot := m
					o.MarketID, o.MarketName, o.RuleSnapshot = m.ID, "短牌 · 2-2", &snapshot
					break
				}
			}
			must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "orders", o.ID, o) }))
			reader := &batchReader{rounds: map[string]games.Round{g.ID: marketRound(in, tc.preview)}}
			must(t, s.discoverSettlementPlans(at))
			plans, err := s.settlementPlans()
			must(t, err)
			if len(plans) != 1 {
				t.Fatal("missing historical settlement plan")
			}
			plan, users := plans[0], planUsers(plans[0])
			must(t, s.prepareUsers(plan, users, reader))
			if preparedCount(t, s) != 1 {
				t.Fatal("retired v3 short pair was not prepared")
			}
			assertFunds(t, s, "member", 99800, 200, 0)
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			published := marketRound(in, tc.published)
			var estimate NextEstimate
			estimate.add(o, &published)
			if estimate.UnresolvedOrders != 0 || estimate.Payout != tc.payout {
				t.Fatal("retired v3 order became unresolved in admin preview", estimate)
			}
			// Different result digest forces calculateMarket again rather than
			// letting the cached prepared result hide a broken retired validator.
			must(t, s.commitUsers(plan, users, map[string]games.Round{g.ID: published}, at.Add(time.Second)))
			must(t, s.commitUsers(plan, users, map[string]games.Round{g.ID: published}, at.Add(time.Second)))
			settled, err := s.Order(o.ID)
			must(t, err)
			if settled.Outcome != tc.want || settled.Payout != tc.payout || !reflect.DeepEqual(settled.ResultNumbers, tc.published) || !reflect.DeepEqual(settled.RuleSnapshot, o.RuleSnapshot) {
				t.Fatal("retired v3 short pair did not recalculate the published result", settled)
			}
			assertFunds(t, s, "member", 99800+tc.payout, 0, 0)
		})
	}
}

func TestK3RetiredPairsRejectNewOrdersAndPreserveQuotesAndAcceptedOrders(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	g, _ := games.Find("k3-1")
	retired := k3RetiredPairMarkets()
	type accepted struct {
		order   Order
		input   MarketOrderInput
		numbers []int
		outcome string
	}
	var historical []accepted
	for _, m := range retired {
		outcomes := []string{"win"}
		if m.RuleVersion == 3 {
			outcomes = append(outcomes, "lose")
		}
		for _, outcome := range outcomes {
			in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: "k3v3.single.n1", OddsMilli: 1000, UnitCents: 200, Multiplier: 1, Key: randomID()}
			o, err := s.PlaceMarketOrder("member", in, now)
			must(t, err)
			in.MarketID, in.OddsMilli, in.Key = m.ID, 2345, randomID()
			snapshot := m
			o.MarketID, o.MarketName, o.OddsMilli, o.RuleSnapshot = m.ID, m.Group+" · "+m.Label, 2345, &snapshot
			var numbers []int
			if m.Kind == "dice_pair" {
				var a, b int
				_, err := fmt.Sscanf(m.Pick, "%d,%d", &a, &b)
				must(t, err)
				numbers = []int{a, b, a}
			} else {
				n, err := strconv.Atoi(m.Pick)
				must(t, err)
				numbers = []int{n, n, n}
				if m.RuleVersion == 3 && outcome == "win" {
					numbers[2] = n%6 + 1
				}
			}
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
				return remember(tx, "market_order:member", in.Key, o.ID, in)
			}))
			historical = append(historical, accepted{o, in, numbers, outcome})
		}
	}
	// A previously accepted batch must also replay after its selections retire.
	batch := MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 200, Key: randomID(), Selections: []MarketSelection{{MarketID: "k3v3.single.n1", OddsMilli: 1000}, {MarketID: "k3v3.single.n2", OddsMilli: 1000}}}
	batchResult, err := s.PlaceMarketBatch("member", batch, now)
	must(t, err)
	batch.Key = randomID()
	ids := []string{}
	for i, m := range []markets.Market{retired[0], retired[len(retired)-1]} {
		snapshot := m
		o := &batchResult.Orders[i]
		o.MarketID, o.MarketName, o.RuleSnapshot = m.ID, m.Group+" · "+m.Label, &snapshot
		batch.Selections[i].MarketID = m.ID
		ids = append(ids, o.ID)
		must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "orders", o.ID, *o) }))
	}
	encoded, err := json.Marshal(ids)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error { return remember(tx, "market_batch:member", batch.Key, string(encoded), batch) }))

	expectedActive, expectedStored := map[string][]Quote{}, map[string][]Quote{}
	for _, session := range games.All() {
		stored := k3StoredQuotes(t, s, session.ID)
		if session.GameID != "k3" {
			expectedStored[session.ID] = stored
			continue
		}
		if len(stored) != 31 {
			t.Fatal("fresh K3 room must contain only 31 quotes", session.ID)
		}
		for i := range stored {
			stored[i].OddsMilli, stored[i].Enabled = 1800+int64(i*13), i%3 != 0
		}
		expectedActive[session.ID] = append([]Quote(nil), stored...)
		for i, m := range retired {
			stored = append(stored, Quote{MarketID: m.ID, OddsMilli: 3000 + int64(i), Enabled: i%2 == 0})
		}
		expectedStored[session.ID] = stored
		must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "market_quotes", session.ID, stored) }))
	}
	before := pk10SizeFinancialState(t, s)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	if !reflect.DeepEqual(before, pk10SizeFinancialState(t, s)) {
		t.Fatal("retirement changed financial state or idempotency records")
	}
	for _, session := range games.All() {
		if !reflect.DeepEqual(expectedStored[session.ID], k3StoredQuotes(t, s, session.ID)) {
			t.Fatal("retirement changed stored odds", session.ID)
		}
		if session.GameID != "k3" {
			continue
		}
		book, err := s.MarketBook(session.ID)
		must(t, err)
		if len(book.Markets) != 31 || !reflect.DeepEqual(book.Quotes, expectedActive[session.ID]) {
			t.Fatal("public book must retain exactly the same 31 active prices", session.ID)
		}
		for _, m := range book.Markets {
			if m.Kind == "dice_pair" || m.Kind == "dice_double" {
				t.Fatal("retired offering exposed", m.ID)
			}
		}
		for _, m := range retired {
			in := MarketOrderInput{SessionID: session.ID, Issue: session.Next(now).Format(model.IssueLayout), MarketID: m.ID, OddsMilli: 3000, UnitCents: 200, Multiplier: 1, Key: randomID()}
			if _, err := s.PlaceMarketOrder("member", in, now); err == nil || err.Error() != "玩法不存在" {
				t.Fatal("retired single order accepted", session.ID, m.ID, err)
			}
			q := book.Quotes[1] // Enabled, so the first item tests atomic rollback.
			mixed := MarketBatchInput{SessionID: session.ID, Issue: in.Issue, UnitCents: 200, AmountCents: 200, Key: randomID(), Selections: []MarketSelection{{MarketID: q.MarketID, OddsMilli: q.OddsMilli}, {MarketID: m.ID, OddsMilli: 3000}}}
			if _, err := s.PlaceMarketBatch("member", mixed, now); err == nil || !strings.Contains(err.Error(), "玩法不存在") {
				t.Fatal("retired batch order accepted", session.ID, m.ID, err)
			}
		}
		oldTable := append([]Quote(nil), book.Quotes...)
		for _, m := range retired {
			if m.RuleVersion == 3 || m.Kind == "dice_pair" {
				oldTable = append(oldTable, Quote{MarketID: m.ID, OddsMilli: 3000, Enabled: true})
			}
		}
		if len(oldTable) != 52 || s.SaveQuotes("admin", session.ID, oldTable) == nil {
			t.Fatal("retired 52-item admin table accepted", session.ID)
		}
		bad := append([]Quote(nil), book.Quotes...)
		bad[0] = Quote{MarketID: retired[0].ID, OddsMilli: 3000, Enabled: true}
		if s.SaveQuotes("admin", session.ID, bad) == nil {
			t.Fatal("retired ID in 31-item admin table accepted", session.ID)
		}
		must(t, s.SaveQuotes("admin", session.ID, book.Quotes))
		if !reflect.DeepEqual(expectedStored[session.ID], k3StoredQuotes(t, s, session.ID)) {
			t.Fatal("saving active table changed archived quotes", session.ID)
		}
	}
	if !reflect.DeepEqual(before, pk10SizeFinancialState(t, s)) {
		t.Fatal("rejected submissions or quote saves changed financial state")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	for id, want := range expectedStored {
		if !reflect.DeepEqual(want, k3StoredQuotes(t, s, id)) {
			t.Fatal("restart changed archived quotes", id)
		}
	}
	gotBatch, err := s.PlaceMarketBatch("member", batch, now)
	must(t, err)
	if !reflect.DeepEqual(gotBatch, batchResult) {
		t.Fatal("historical retired batch retry changed its accepted orders")
	}
	for _, old := range historical {
		retry, err := s.PlaceMarketOrder("member", old.input, now)
		must(t, err)
		if !reflect.DeepEqual(retry, old.order) {
			t.Fatal("historical retired single retry changed its accepted order")
		}
		round := marketRound(old.input, old.numbers)
		must(t, s.SettleMarket(old.order.ID, round))
		must(t, s.SettleMarket(old.order.ID, round))
		settled, err := s.Order(old.order.ID)
		must(t, err)
		want := int64(0)
		if old.outcome == "win" {
			want = 469
		}
		if settled.Outcome != old.outcome || settled.Payout != want || !reflect.DeepEqual(settled.RuleSnapshot, old.order.RuleSnapshot) {
			t.Fatalf("retired contract changed its locked outcome: %+v", settled)
		}
	}
}
