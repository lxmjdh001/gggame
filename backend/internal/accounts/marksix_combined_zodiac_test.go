package accounts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestMarkSixCombinedZodiacChargesOneStakeAndPaysOnceForTwoThroughEleven(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	g, _ := games.Find("lhc-tw")
	now := time.Date(2026, 10, 15, 13, 0, 10, 0, model.Beijing)
	book, err := s.MarketBook(g.ID)
	must(t, err)
	for count := 2; count <= 11; count++ {
		id := fmt.Sprintf("lhc6.combined_zodiac.in.%d", count)
		found := false
		for i := range book.Quotes {
			if book.Quotes[i].MarketID == id {
				book.Quotes[i].OddsMilli = 1880
				found = true
			}
		}
		if !found {
			t.Fatal("missing combined-zodiac quote", id)
		}
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	// Horse and snake are selected in every set; rat is omitted even from
	// the eleven-zodiac set. All six regular balls below match these two
	// selected zodiacs, but only the seventh ball may decide the result.
	values := []int{6, 5, 4, 3, 2, 1, 7, 8, 9, 10, 11}
	type accepted struct {
		order Order
		round games.Round
		win   bool
	}
	var all []accepted
	stake := int64(200)
	for count := 2; count <= 11; count++ {
		for _, won := range []bool{false, true} {
			in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: &stake, MarketID: fmt.Sprintf("lhc6.combined_zodiac.in.%d", count), OddsMilli: 1880, Values: append([]int(nil), values[:count]...), Key: randomID()}
			order, err := s.PlaceMarketOrder("member", in, now)
			must(t, err)
			wantValues := append([]int(nil), values[:count]...)
			sort.Ints(wantValues)
			if order.Cost != stake || order.OddsMilli != 1880 || order.RuleSnapshot == nil || !reflect.DeepEqual(order.RuleSnapshot.SelectionValues, wantValues) || order.RuleSnapshot.ZodiacYear != 2026 || !reflect.DeepEqual(order.RuleSnapshot.Positions, []int{7}) {
				t.Fatal("a zodiac set did not bind one stake and one seventh-position rule", order)
			}
			duplicate, err := s.PlaceMarketOrder("member", in, now)
			must(t, err)
			if duplicate.ID != order.ID {
				t.Fatal("idempotent set created another order")
			}
			special := 7 // Rat: outside every chosen set despite all regular balls matching.
			if won {
				special = 26 // Snake: a normal winning special ball, paid once.
			}
			round := marketRound(in, []int{1, 13, 25, 37, 2, 14, special})
			all = append(all, accepted{order, round, won})
		}
	}
	if len(all) != 20 {
		t.Fatal("expected one losing and one winning order for every supported set size")
	}
	assertFunds(t, s, "member", 96000, 4000, 0)
	// A later administrator price change must not change any accepted payout.
	for i := range book.Quotes {
		for count := 2; count <= 11; count++ {
			if book.Quotes[i].MarketID == fmt.Sprintf("lhc6.combined_zodiac.in.%d", count) {
				book.Quotes[i].OddsMilli = 5000
			}
		}
	}
	must(t, s.SaveQuotes("admin", g.ID, book.Quotes))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	// Replay concurrent settlement to verify that neither set size nor the
	// six regular matches cause another wallet credit.
	var wg sync.WaitGroup
	for _, entry := range all {
		for attempt := 0; attempt < 2; attempt++ {
			wg.Add(1)
			go func(entry accepted) {
				defer wg.Done()
				if err := s.SettleMarket(entry.order.ID, entry.round); err != nil {
					t.Error(err)
				}
			}(entry)
		}
	}
	wg.Wait()
	for _, entry := range all {
		settled, err := s.Order(entry.order.ID)
		must(t, err)
		outcome, payout := "lose", int64(0)
		if entry.win {
			outcome, payout = "win", int64(376) // 200 × 1.880 exactly once.
		}
		if settled.Status != "settled" || settled.Outcome != outcome || settled.Payout != payout || settled.OddsMilli != 1880 {
			t.Fatal("combined-zodiac settlement counted regular matches, set size, or a newer price", settled)
		}
	}
	assertFunds(t, s, "member", 99760, 0, 0)
}

func markSixCombinedBatch(now time.Time) MarketBatchInput {
	g, _ := games.Find("lhc-tw")
	return MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 200, Key: randomID(), Selections: []MarketSelection{
		{MarketID: "lhc6.combined_zodiac.in.2", OddsMilli: 1000, Values: []int{6, 5}},
		{MarketID: "lhc6.combined_zodiac.in.2", OddsMilli: 1000, Values: []int{1, 0}},
	}}
}

// Capture every stored record so failed batches cannot leave partial wallet,
// order, limit, audit or idempotency writes behind. Keep this helper independent
// of tests for other extended markets that are outside the combined-zodiac release.
func markSixCombinedStoredState(t *testing.T, s *Store) map[string]map[string][]byte {
	t.Helper()
	state := map[string]map[string][]byte{}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
			values := map[string][]byte{}
			state[string(name)] = values
			return bucket.ForEach(func(key, value []byte) error {
				values[string(key)] = append([]byte(nil), value...)
				return nil
			})
		})
	}))
	return state
}

func TestMarkSixCombinedZodiacBatchAcceptsDistinctSetsWithoutDuplicatingStake(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	now := time.Date(2026, 10, 15, 13, 0, 10, 0, model.Beijing)
	in := markSixCombinedBatch(now)
	out, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	if len(out.Orders) != 2 || out.TotalCost != 400 || out.Orders[0].ID == out.Orders[1].ID {
		t.Fatal("different sets of the same combined-zodiac template must remain two independent bets", out)
	}
	for i, wantValues := range [][]int{{5, 6}, {0, 1}} {
		order := out.Orders[i]
		if order.Cost != 200 || order.OddsMilli != 1000 || order.MarketID != "lhc6.combined_zodiac.in.2" || order.RuleSnapshot == nil || !reflect.DeepEqual(order.RuleSnapshot.SelectionValues, wantValues) {
			t.Fatal("combined-zodiac batch lost its per-set snapshot", order)
		}
	}
	if !reflect.DeepEqual(in.Selections[0].Values, []int{6, 5}) || !reflect.DeepEqual(in.Selections[1].Values, []int{1, 0}) {
		t.Fatal("binding changed the original request order and its idempotency fingerprint")
	}
	assertFunds(t, s, "member", 99600, 400, 0)
	beforeRetry := markSixCombinedStoredState(t, s)
	retry, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	firstJSON, err := json.Marshal(out)
	must(t, err)
	retryJSON, err := json.Marshal(retry)
	must(t, err)
	if !bytes.Equal(firstJSON, retryJSON) {
		t.Fatal("retried combined-zodiac batch changed its accepted orders")
	}
	if !reflect.DeepEqual(beforeRetry, markSixCombinedStoredState(t, s)) {
		t.Fatal("retried combined-zodiac batch changed stored orders or money")
	}
}

func TestMarkSixCombinedZodiacInvalidBatchesAreAtomic(t *testing.T) {
	for _, scenario := range []string{"equivalent_sets", "duplicate_value", "wrong_size", "out_of_range", "stale_odds", "disabled", "insufficient_balance"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _ := fundsFixture(t)
			defer s.Close()
			now := time.Date(2026, 10, 15, 13, 0, 10, 0, model.Beijing)
			in := markSixCombinedBatch(now)
			switch scenario {
			case "equivalent_sets":
				in.Selections[1].Values = []int{5, 6}
			case "duplicate_value":
				in.Selections[1].Values = []int{1, 1}
			case "wrong_size":
				in.Selections[1].Values = []int{0, 1, 2}
			case "out_of_range":
				in.Selections[1].Values = []int{0, 12}
			case "stale_odds":
				in.Selections[1].OddsMilli = 1880
			case "disabled":
				in.Selections[1] = MarketSelection{MarketID: "lhc6.combined_zodiac.in.3", OddsMilli: 1000, Values: []int{0, 1, 2}}
				book, err := s.MarketBook(in.SessionID)
				must(t, err)
				for i := range book.Quotes {
					if book.Quotes[i].MarketID == in.Selections[1].MarketID {
						book.Quotes[i].Enabled = false
					}
				}
				must(t, s.SaveQuotes("admin", in.SessionID, book.Quotes))
			case "insufficient_balance":
				in.AmountCents = 60000
			}
			before := markSixCombinedStoredState(t, s)
			out, err := s.PlaceMarketBatch("member", in, now)
			if err == nil || len(out.Orders) != 0 || out.TotalCost != 0 {
				t.Fatal("invalid combined-zodiac batch accepted or returned partial success", out, err)
			}
			if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
				t.Fatal("failed combined-zodiac batch left partial database writes")
			}
			assertFunds(t, s, "member", 100000, 0, 0)
		})
	}
}

func TestMarkSixCombinedZodiacUsesDrawLunarYearAcrossNewYear(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	g, _ := games.Find("lhc-tw")
	stake := int64(200)
	for _, tc := range []struct {
		day, lunarYear int
		want           string
		payout         int64
	}{
		{16, 2025, "win", 200}, // 01 is snake before 2026 Lunar New Year.
		{17, 2026, "lose", 0},  // 01 is horse after the Beijing midnight boundary.
	} {
		now := time.Date(2026, 2, tc.day, 12, 0, 0, 0, model.Beijing)
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: &stake, MarketID: "lhc6.combined_zodiac.in.2", OddsMilli: 1000, Values: []int{0, 5}, Key: randomID()}
		order, err := s.PlaceMarketOrder("member", in, now)
		must(t, err)
		if order.RuleSnapshot == nil || order.RuleSnapshot.ZodiacYear != tc.lunarYear {
			t.Fatal("combined-zodiac order uses calendar/current year instead of its draw's lunar year", order)
		}
		round := marketRound(in, []int{2, 3, 4, 5, 6, 7, 1})
		must(t, s.SettleMarket(order.ID, round))
		settled, err := s.Order(order.ID)
		must(t, err)
		if settled.Outcome != tc.want || settled.Payout != tc.payout {
			t.Fatal("combined-zodiac result drifted across Lunar New Year", settled)
		}
	}
	assertFunds(t, s, "member", 99800, 0, 0)
}
