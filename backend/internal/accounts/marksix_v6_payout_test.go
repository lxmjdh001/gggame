package accounts

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func markSixV6Time() time.Time { return time.Date(2026, 10, 17, 12, 0, 10, 0, model.Beijing) }
func markSixV6Input(t *testing.T, s *Store, id string, values []int) MarketOrderInput {
	t.Helper()
	g, _ := games.Find("lhc-tw")
	book, err := s.MarketBook(g.ID)
	must(t, err)
	stake := int64(201)
	for _, q := range book.Quotes {
		if q.MarketID == id {
			return MarketOrderInput{SessionID: g.ID, Issue: g.Next(markSixV6Time()).Format(model.IssueLayout), MarketID: id, OddsMilli: q.OddsMilli, TierOddsMilli: q.TierOddsMilli, Values: values, UnitCents: 200, AmountCents: &stake, Key: randomID()}
		}
	}
	t.Fatal("missing v6 market", id)
	return MarketOrderInput{}
}
func markSixV6Prices(t *testing.T, s *Store) {
	t.Helper()
	book, err := s.MarketBook("lhc-tw")
	must(t, err)
	for i := range book.Quotes {
		q := &book.Quotes[i]
		switch q.MarketID {
		case "lhc6.link.three_two":
			q.TierOddsMilli = map[string]int64{"hit2": 3500, "hit3": 5250}
		case "lhc6.link.two_special":
			q.TierOddsMilli = map[string]int64{"special": 4250, "hit2": 2200}
		case "lhc6.regular.zodiac.z6", "lhc6.any.zodiac.z6":
			q.OddsMilli = 1881
		}
	}
	must(t, s.SaveQuotes("admin", "lhc-tw", book.Quotes))
}

func TestMarkSixV6TierAndCountSnapshotsDebitOnceSettleOnce(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	markSixV6Prices(t, s)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "member")
		if e != nil {
			return e
		}
		u.InviterID = "parent"
		if e = put(tx, "users", "member", u); e != nil {
			return e
		}
		return put(tx, "users", "parent", storedUser{User: User{ID: "parent", Username: "parent", Role: "member", Status: "active"}})
	}))
	cfg, err := s.Settings()
	must(t, err)
	cfg.CommissionBPS = [3]int64{100, 0, 0}
	must(t, s.SaveSettings("admin", cfg))
	cases := []struct {
		id                  string
		values              []int
		tier                string
		count, odds, payout int64
	}{
		{"lhc6.link.three_two", []int{1, 13, 26}, "hit2", 1, 3500, 703},
		{"lhc6.link.three_two", []int{1, 13, 25}, "hit3", 1, 5250, 1055},
		{"lhc6.link.two_special", []int{2, 14}, "special", 1, 4250, 854},
		{"lhc6.link.two_special", []int{1, 13}, "hit2", 1, 2200, 442},
		{"lhc6.link.three_two", []int{1, 14, 26}, "", 0, 0, 0},
		{"lhc6.regular.zodiac.z6", nil, "", 5, 1881, 1890},
		{"lhc6.any.zodiac.z6", nil, "", 1, 1881, 378},
	}
	var orders []Order
	var inputs []MarketOrderInput
	for _, tc := range cases {
		in := markSixV6Input(t, s, tc.id, tc.values)
		o, e := s.PlaceMarketOrder("member", in, markSixV6Time())
		must(t, e)
		orders = append(orders, o)
		inputs = append(inputs, in)
		if o.Cost != 201 {
			t.Fatal("charged more than one stake", o)
		}
	}
	assertFunds(t, s, "member", 100000-7*201, 7*201, 0)
	// Mutating a returned/client map or later administrative prices cannot change locks.
	inputs[0].TierOddsMilli["hit3"] = 9999
	stored, err := s.Order(orders[0].ID)
	must(t, err)
	if stored.TierOddsMilli["hit3"] != 5250 {
		t.Fatal("client map aliases stored order")
	}
	inputs[0].TierOddsMilli["hit3"] = 5250
	book, err := s.MarketBook("lhc-tw")
	must(t, err)
	for i := range book.Quotes {
		q := &book.Quotes[i]
		if len(q.TierOddsMilli) > 0 {
			for k := range q.TierOddsMilli {
				q.TierOddsMilli[k] = 1000
			}
		} else if strings.HasPrefix(q.MarketID, "lhc6.") {
			q.OddsMilli = 1000
		}
	}
	must(t, s.SaveQuotes("admin", "lhc-tw", book.Quotes))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	var wg sync.WaitGroup
	for i, o := range orders {
		retry, e := s.PlaceMarketOrder("member", inputs[i], markSixV6Time().Add(time.Hour))
		must(t, e)
		if retry.ID != o.ID {
			t.Fatal("retry debited again")
		}
		for attempt := 0; attempt < 3; attempt++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if e := s.SettleMarket(orders[i].ID, marketRound(inputs[i], []int{1, 13, 25, 37, 49, 2, 14})); e != nil {
					t.Error(e)
				}
			}(i)
		}
	}
	wg.Wait()
	var total int64
	for i, tc := range cases {
		o, e := s.Order(orders[i].ID)
		must(t, e)
		outcome := "win"
		if tc.payout == 0 {
			outcome = "lose"
		}
		if o.Outcome != outcome || o.Payout != tc.payout || o.PayoutTier != tc.tier || o.PayoutMultiplier != tc.count || o.PayoutOddsMilli != tc.odds {
			t.Fatalf("%s: wrong details: %+v", tc.id, o)
		}
		total += tc.payout
	}
	assertFunds(t, s, "member", 100000-7*201+total, 0, 0)
	parent, err := s.Account("parent")
	must(t, err)
	if parent["commission_available_cents"].(int64) != 14 {
		t.Fatal("commission must use each stake once", parent)
	}
}

func TestMarkSixV6TierValidationAndAtomicBatch(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	markSixV6Prices(t, s)
	good := markSixV6Input(t, s, "lhc6.link.three_two", []int{1, 13, 25})
	for name, odds := range map[string]map[string]int64{
		"missing": nil, "partial": {"hit2": 3500}, "extra": {"hit2": 3500, "hit3": 5250, "special": 1}, "unknown": {"hit2": 3500, "other": 5250}, "low": {"hit2": 999, "hit3": 5250}, "high": {"hit2": 3500, "hit3": 1000001}, "stale_unhit": {"hit2": 3500, "hit3": 5260},
	} {
		t.Run(name, func(t *testing.T) {
			in := good
			in.Key = randomID()
			in.TierOddsMilli = odds
			before := markSixCombinedStoredState(t, s)
			if _, e := s.PlaceMarketOrder("member", in, markSixV6Time()); e == nil {
				t.Fatal("invalid tiers accepted")
			}
			if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
				t.Fatal("invalid tiers changed storage")
			}
		})
	}
	// A valid first batch item must roll back when a later item's other tier is stale.
	bad := map[string]int64{"hit2": 3500, "hit3": 5251}
	in := MarketBatchInput{SessionID: good.SessionID, Issue: good.Issue, UnitCents: 200, AmountCents: 201, Key: randomID(), Selections: []MarketSelection{
		{MarketID: good.MarketID, OddsMilli: good.OddsMilli, Values: good.Values, TierOddsMilli: good.TierOddsMilli},
		{MarketID: good.MarketID, OddsMilli: good.OddsMilli, Values: []int{1, 13, 26}, TierOddsMilli: bad}}}
	before := markSixCombinedStoredState(t, s)
	if _, e := s.PlaceMarketBatch("member", in, markSixV6Time()); e == nil {
		t.Fatal("partly invalid batch accepted")
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("partially charged invalid batch")
	}
	in.Selections[1].TierOddsMilli = map[string]int64{"hit3": 5250, "hit2": 3500}
	out, e := s.PlaceMarketBatch("member", in, markSixV6Time())
	must(t, e)
	if len(out.Orders) != 2 || out.TotalCost != 402 {
		t.Fatal(out)
	}
	retry, e := s.PlaceMarketBatch("member", in, markSixV6Time())
	must(t, e)
	firstJSON, _ := json.Marshal(out)
	retryJSON, _ := json.Marshal(retry)
	if string(firstJSON) != string(retryJSON) {
		t.Fatal("retry changed locks")
	}
	in.Selections[1].TierOddsMilli = map[string]int64{"hit3": 5251, "hit2": 3500}
	if _, e = s.PlaceMarketBatch("member", in, markSixV6Time()); e == nil {
		t.Fatal("key reused with changed tier accepted")
	}
	ordinary := markSixV6Input(t, s, "p7.n49", nil)
	ordinary.TierOddsMilli = map[string]int64{"hit2": 1000}
	if _, e = s.PlaceMarketOrder("member", ordinary, markSixV6Time()); e == nil {
		t.Fatal("non-tier extra map accepted")
	}
	book, e := s.MarketBook("lhc-tw")
	must(t, e)
	for i := range book.Quotes {
		if book.Quotes[i].MarketID == good.MarketID {
			book.Quotes[i].TierOddsMilli = map[string]int64{"hit2": 3500}
		}
	}
	before = markSixCombinedStoredState(t, s)
	if e = s.SaveQuotes("admin", "lhc-tw", book.Quotes); e == nil {
		t.Fatal("admin missing tier accepted")
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("invalid admin save changed storage")
	}
}

func TestMarkSixV6PreparedPayoutRecalculatesTierAndCountAfterRestart(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	markSixV6Prices(t, s)
	var inputs []MarketOrderInput
	var orders []Order
	for _, id := range []string{"lhc6.link.three_two", "lhc6.regular.zodiac.z6"} {
		var values []int
		if strings.Contains(id, "three_two") {
			values = []int{1, 13, 25}
		}
		in := markSixV6Input(t, s, id, values)
		o, e := s.PlaceMarketOrder("member", in, markSixV6Time())
		must(t, e)
		inputs = append(inputs, in)
		orders = append(orders, o)
	}
	early := marketRound(inputs[0], []int{1, 13, 2, 3, 4, 5, 6})
	reader := &markSixV6Reader{batchReader: batchReader{rounds: map[string]games.Round{early.SessionID: early}}}
	preview, e := s.UserStatus("member", markSixV6Time(), reader)
	must(t, e)
	for _, row := range preview.Sessions {
		if row.SessionID == "lhc-tw" {
			if row.Estimate.Payout != 1459 {
				t.Fatal("preview amount", row.Estimate)
			}
			for _, o := range row.Orders {
				if o.Payout == nil || o.PayoutMultiplier == 0 {
					t.Fatal("missing detailed preview", o)
				}
			}
		}
	}
	must(t, s.discoverSettlementPlans(early.DrawAt))
	plans, e := s.settlementPlans()
	must(t, e)
	if len(plans) != 1 {
		t.Fatal(plans)
	}
	p := plans[0]
	must(t, s.prepareUsers(p, planUsers(p), reader))
	if preparedCount(t, s) != 1 {
		t.Fatal("not prepared")
	}
	assertNoEarlyPayout(t, s)
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	published := marketRound(inputs[0], []int{1, 13, 25, 37, 49, 2, 14})
	must(t, s.commitUsers(p, planUsers(p), map[string]games.Round{published.SessionID: published}, published.DrawAt))
	must(t, s.commitUsers(p, planUsers(p), map[string]games.Round{published.SessionID: published}, published.DrawAt))
	for i, want := range []struct {
		tier          string
		count, payout int64
	}{{"hit3", 1, 1055}, {"", 5, 1890}} {
		o, e := s.Order(orders[i].ID)
		must(t, e)
		if o.PayoutTier != want.tier || o.PayoutMultiplier != want.count || o.Payout != want.payout {
			t.Fatal("prepared result bypassed current draw", o)
		}
	}
	assertFunds(t, s, "member", 100000-402+2945, 0, 0)
}

func TestMarkSixV6MaximumPayoutAndRounding(t *testing.T) {
	// Count is applied before cent rounding: truncating each hit first loses 4 cents.
	got, e := scaledMarketPayout(201, 1881, 5)
	must(t, e)
	if got != 1890 {
		t.Fatal(got)
	}
	for _, tc := range []struct{ cost, odds, count int64 }{{maxMoney, 1001, 1}, {maxMoney, 1000, 2}, {maxMoney, 1000000, math.MaxInt64}, {1, 1000, 0}, {1, math.MaxInt64, 1}} {
		if _, e := scaledMarketPayout(tc.cost, tc.odds, tc.count); e == nil {
			t.Fatal("overflow/out-of-range accepted", tc)
		}
	}
	if got, e := scaledMarketPayout(maxMoney, 1000, 1); e != nil || got != maxMoney {
		t.Fatal("valid limit", got, e)
	}
	s, _, _ := fundsFixture(t)
	defer s.Close()
	book, e := s.MarketBook("lhc-tw")
	must(t, e)
	for i := range book.Quotes {
		q := &book.Quotes[i]
		if q.MarketID == "lhc6.link.three_two" {
			q.TierOddsMilli = map[string]int64{"hit2": 1000, "hit3": 1000000}
		}
		if q.MarketID == "lhc6.regular.zodiac.z6" {
			q.OddsMilli = 1000000
		}
	}
	must(t, s.SaveQuotes("admin", "lhc-tw", book.Quotes))
	cfg, e := s.Settings()
	must(t, e)
	for i := range cfg.Plays {
		if cfg.Plays[i].SessionID == "lhc-tw" {
			cfg.Plays[i].MaxStakeCents = 100000000
			cfg.Plays[i].UserIssueLimitCents = 100000000
		}
	}
	must(t, s.SaveSettings("admin", cfg))
	q := Quote{OddsMilli: 1000, TierOddsMilli: map[string]int64{"hit2": 1000, "hit3": 1000000}}
	if maximumQuoteOdds(q) != 1000000 {
		t.Fatal("omitted unhit tier from maximum risk")
	}
	if payout, e := scaledMarketPayout(100000000, maximumQuoteOdds(q), 1); e != nil || payout != maxMoney {
		t.Fatal("maximum tier risk boundary", payout, e)
	}
	in := markSixV6Input(t, s, "lhc6.regular.zodiac.z6", nil)
	amount := int64(20000001)
	in.AmountCents = &amount
	if _, e = s.PlaceMarketOrder("member", in, markSixV6Time()); e == nil || !strings.Contains(e.Error(), "预计返还") {
		t.Fatal("count risk ignored", e)
	}

}

func TestMarkSixV6OldSnapshotsStayImmutable(t *testing.T) {
	g, _ := games.Find("lhc-tw")
	at := g.Next(markSixV6Time())
	issue := at.Format(model.IssueLayout)
	var legacyCombined markets.Market
	for _, m := range markets.LegacyMarkSixCatalog() {
		if m.ID == "lhc5.combined_zodiac.in.2" {
			legacyCombined = m
		}
	}
	// Bind the archived v5 template directly; current Find deliberately excludes it.
	old, err := markets.BindSelection(legacyCombined, []int{5, 6}, at)
	must(t, err)
	order := Order{SessionID: g.ID, Issue: issue, MarketID: old.ID, OddsMilli: 1880, Cost: 200, RuleSnapshot: &old}
	round := games.Round{SessionID: g.ID, Issue: issue, SourceIssue: issue, SourceGroup: g.Group, Origin: "scheduled", DrawAt: at, Numbers: []int{1, 2, 3, 4, 5, 6, 49}}
	detail, err := calculateMarketDetailed(order, round)
	must(t, err)
	if detail.Outcome != "win" || detail.Payout != 376 {
		t.Fatal("v5 special49 semantics changed", detail)
	}
	current, _ := markets.Find("lhc", "lhc6.combined_zodiac.in.2")
	current, err = markets.BindSelection(current, []int{5, 6}, at)
	must(t, err)
	order.RuleSnapshot = &current
	order.MarketID = current.ID
	detail, err = calculateMarketDetailed(order, round)
	must(t, err)
	if detail.Outcome != "push" || detail.Payout != 200 {
		t.Fatal("v6 special49 must refund", detail)
	}
	for _, m := range markets.LegacyMarkSixCatalog() {
		if m.ID != "size2.p7.big" && m.ID != "parity2.p7.odd" {
			continue
		}
		o := Order{SessionID: g.ID, Issue: issue, MarketID: m.ID, OddsMilli: 1960, Cost: 200, RuleSnapshot: &m}
		r := round
		r.Numbers = []int{1, 2, 3, 4, 5, 6, 25}
		out, e := calculateMarketDetailed(o, r)
		must(t, e)
		want := "win"
		if m.ID == "size2.p7.big" {
			want = "push"
		}
		if out.Outcome != want {
			t.Fatal("v2 changed", m.ID, out)
		}
	}
	// New optional fields must not enter old order JSON or its prepared digest.
	order.TierOddsMilli = nil
	order.PayoutTier = ""
	order.PayoutMultiplier = 0
	order.PayoutOddsMilli = 0
	raw, e := json.Marshal(order)
	must(t, e)
	for _, field := range []string{"tier_odds_milli", "payout_tier", "payout_multiplier", "payout_odds_milli"} {
		if strings.Contains(string(raw), field) {
			t.Fatal("old optional field leaked", field)
		}
	}
}

type markSixV6Reader struct{ batchReader }

func (r *markSixV6Reader) History(string, int, string) (games.History, error) {
	return games.History{}, nil
}
