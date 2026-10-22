package accounts

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func pk10AmountBatch(t *testing.T, s *Store, now time.Time, ids ...string) MarketBatchInput {
	t.Helper()
	g, _ := games.Find("pk10-10")
	book, err := s.MarketBook(g.ID)
	must(t, err)
	in := MarketBatchInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Key: randomID()}
	for _, id := range ids {
		found := false
		for _, q := range book.Quotes {
			if q.MarketID == id {
				in.Selections = append(in.Selections, MarketSelection{MarketID: id, OddsMilli: q.OddsMilli})
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing test market", id)
		}
	}
	return in
}

func TestMarketBatchSelectionAmountsAndUniformFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fallback int64
		amounts  map[string]int64
		want     []int64
	}{
		{"uniform", 1000, nil, []int64{1000, 1000, 1000}},
		{"empty_map", 1000, map[string]int64{}, []int64{1000, 1000, 1000}},
		{"all_explicit", 0, map[string]int64{"p1.n1": 230, "p2.n2": 450, "p3.n3": 670}, []int64{230, 450, 670}},
		{"partial_with_fallback", 1200, map[string]int64{"p1.n1": 201, "p3.n3": 2400}, []int64{201, 1200, 2400}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, now := fundsFixture(t)
			defer s.Close()
			in := pk10AmountBatch(t, s, now, "p1.n1", "p2.n2", "p3.n3")
			in.AmountCents, in.SelectionAmounts = tc.fallback, tc.amounts
			out, err := s.PlaceMarketBatch("member", in, now)
			must(t, err)
			if len(out.Orders) != len(tc.want) {
				t.Fatal("missing orders")
			}
			total := int64(0)
			for i, cost := range tc.want {
				total += cost
				if out.Orders[i].Cost != cost || out.Orders[i].MarketID != in.Selections[i].MarketID {
					t.Fatal("wrong selection amount", out.Orders[i])
				}
			}
			if out.TotalCost != total {
				t.Fatal("wrong total", out.TotalCost, total)
			}
			assertFunds(t, s, "member", 100000-total, total, 0)
			must(t, s.db.View(func(tx *bolt.Tx) error {
				used, err := issueStake(tx, "member", in.SessionID, in.Issue)
				if err != nil {
					return err
				}
				if used != total {
					t.Fatal("issue reservation differs from frozen stakes", used, total)
				}
				return nil
			}))
		})
	}
}

func TestMarketBatchSelectionAmountsRejectAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		amounts  map[string]int64
		fallback int64
		limit    int64
	}{
		{"unselected_key", map[string]int64{"p4.n4": 1000}, 1000, 0},
		{"unknown_key", map[string]int64{"missing": 1000}, 1000, 0},
		{"explicit_zero", map[string]int64{"p2.n2": 0}, 1000, 0},
		{"negative", map[string]int64{"p2.n2": -1}, 1000, 0},
		{"below_minimum", map[string]int64{"p2.n2": 199}, 1000, 0},
		{"above_maximum", map[string]int64{"p2.n2": 100001}, 1000, 0},
		{"int64_input", map[string]int64{"p2.n2": math.MaxInt64}, 1000, 0},
		{"later_balance_failure", map[string]int64{"p1.n1": 2000, "p2.n2": 99000}, 1000, 0},
		{"later_issue_limit_failure", map[string]int64{"p1.n1": 1500, "p2.n2": 600}, 1000, 2000},
		{"invalid_fallback", map[string]int64{"p1.n1": 1000}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, now := fundsFixture(t)
			defer s.Close()
			in := pk10AmountBatch(t, s, now, "p1.n1", "p2.n2", "p3.n3")
			in.SelectionAmounts, in.AmountCents = tc.amounts, tc.fallback
			if tc.limit != 0 {
				cfg, err := s.Settings()
				must(t, err)
				for i := range cfg.Plays {
					if cfg.Plays[i].SessionID == in.SessionID {
						cfg.Plays[i].UserIssueLimitCents = tc.limit
					}
				}
				must(t, s.SaveSettings("admin", cfg))
			}
			out, err := s.PlaceMarketBatch("member", in, now)
			if err == nil || len(out.Orders) != 0 || out.TotalCost != 0 {
				t.Fatal("invalid batch returned partial success", out, err)
			}
			assertFunds(t, s, "member", 100000, 0, 0)
			must(t, s.db.View(func(tx *bolt.Tx) error {
				for _, name := range []string{"orders", "ledger", "pending_markets", "keys", issueStakeBucket} {
					if tx.Bucket([]byte(name)).Stats().KeyN != 0 {
						t.Fatal("failed batch persisted writes", name)
					}
				}
				return nil
			}))
			// A failed request must not reserve its idempotency key or issue limit.
			in.SelectionAmounts = map[string]int64{"p1.n1": 400, "p2.n2": 400, "p3.n3": 400}
			out, err = s.PlaceMarketBatch("member", in, now)
			must(t, err)
			if out.TotalCost != 1200 {
				t.Fatal("corrected batch cost wrong", out)
			}
			assertFunds(t, s, "member", 98800, 1200, 0)
		})
	}
}

func TestMarketBatchSelectionAmountsIdempotencyAcrossRestartAndConfiguration(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	in := pk10AmountBatch(t, s, now, "p1.n1", "p2.n2", "p3.n3")
	in.SelectionAmounts = map[string]int64{"p1.n1": 201, "p2.n2": 399}
	first, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	for _, changedMap := range []map[string]int64{
		{"p1.n1": 202, "p2.n2": 399},
		{"p1.n1": 201},
		{"p1.n1": 201, "p2.n2": 399, "p3.n3": 1000},
		nil,
	} {
		changed := in
		changed.SelectionAmounts = changedMap
		if _, err = s.PlaceMarketBatch("member", changed, now.Add(time.Hour)); err == nil {
			t.Fatal("same key accepted a different amount map")
		}
	}
	cfg, err := s.Settings()
	must(t, err)
	for i := range cfg.Plays {
		if cfg.Plays[i].SessionID == in.SessionID {
			cfg.Plays[i].Enabled = false
			cfg.Plays[i].UnitCents = 500
		}
	}
	must(t, s.SaveSettings("admin", cfg))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	retry := in
	retry.SelectionAmounts = map[string]int64{}
	retry.SelectionAmounts["p2.n2"] = 399
	retry.SelectionAmounts["p1.n1"] = 201
	again, err := s.PlaceMarketBatch("member", retry, now.Add(24*time.Hour))
	must(t, err)
	if again.TotalCost != first.TotalCost || len(again.Orders) != len(first.Orders) {
		t.Fatal("retry lost original batch", again)
	}
	for i, o := range again.Orders {
		if o.ID != first.Orders[i].ID || o.Cost != first.Orders[i].Cost || o.Issue != first.Orders[i].Issue {
			t.Fatal("retry changed original order", o)
		}
	}
	assertFunds(t, s, "member", 98400, 1600, 0)
}

func TestMarketBatchUniformLegacyJSONAndDigestRemainCompatible(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	in := batchInput(now)
	// This is the exact request shape used before selection_amounts existed.
	legacy := struct {
		SessionID   string            `json:"session_id"`
		Issue       string            `json:"issue"`
		UnitCents   int64             `json:"unit_cents"`
		AmountCents int64             `json:"amount_cents"`
		Selections  []MarketSelection `json:"selections"`
		Key         string            `json:"key"`
	}{in.SessionID, in.Issue, in.UnitCents, in.AmountCents, in.Selections, in.Key}
	oldJSON, err := json.Marshal(legacy)
	must(t, err)
	for _, amounts := range []map[string]int64{nil, {}} {
		in.SelectionAmounts = amounts
		newJSON, err := json.Marshal(in)
		must(t, err)
		if !bytes.Equal(oldJSON, newJSON) {
			t.Fatal("uniform request fingerprint changed", string(newJSON))
		}
	}
	in.SelectionAmounts = nil
	first, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	ids := make([]string, len(first.Orders))
	for i, o := range first.Orders {
		ids[i] = o.ID
	}
	encodedIDs, err := json.Marshal(ids)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return remember(tx, "market_batch:member", in.Key, string(encodedIDs), legacy)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	in.SelectionAmounts = map[string]int64{}
	again, err := s.PlaceMarketBatch("member", in, now.Add(24*time.Hour))
	must(t, err)
	for i, o := range again.Orders {
		if o.ID != ids[i] {
			t.Fatal("legacy retry generated new order")
		}
	}
	assertFunds(t, s, "member", 98000, 2000, 0)
}

func TestPK10UnequalAmountsSettleNewSumMarketsUsingLockedOdds(t *testing.T) {
	s, _, now := fundsFixture(t)
	defer s.Close()
	in := pk10AmountBatch(t, s, now, "sum.set_3_4_18_19", "sum.n3", "p1.n1")
	in.SelectionAmounts = map[string]int64{"sum.set_3_4_18_19": 201, "sum.n3": 399}
	batch, err := s.PlaceMarketBatch("member", in, now)
	must(t, err)
	if batch.TotalCost != 1600 {
		t.Fatal("unequal cost not applied", batch.TotalCost)
	}
	assertFunds(t, s, "member", 98400, 1600, 0)
	book, err := s.MarketBook(in.SessionID)
	must(t, err)
	for i := range book.Quotes {
		book.Quotes[i].OddsMilli += 1000
	}
	must(t, s.SaveQuotes("admin", in.SessionID, book.Quotes))
	round := marketRound(MarketOrderInput{SessionID: in.SessionID, Issue: in.Issue}, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	totalPayout := int64(0)
	for _, before := range batch.Orders {
		want := before.Cost * before.OddsMilli / 1000
		totalPayout += want
		must(t, s.SettleMarket(before.ID, round))
		must(t, s.SettleMarket(before.ID, round))
		after, err := s.Order(before.ID)
		must(t, err)
		if after.Status != "settled" || after.Outcome != "win" || after.Payout != want || after.OddsMilli != before.OddsMilli || !reflect.DeepEqual(after.RuleSnapshot, before.RuleSnapshot) {
			t.Fatal("settlement lost locked per-selection amount or odds", after)
		}
	}
	assertFunds(t, s, "member", 98400+totalPayout, 0, 0)
}
