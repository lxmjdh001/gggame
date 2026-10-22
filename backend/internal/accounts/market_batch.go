package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

type MarketSelection struct {
	MarketID      string           `json:"market_id"`
	OddsMilli     int64            `json:"odds_milli"`
	Values        []int            `json:"values,omitempty"`
	TierOddsMilli map[string]int64 `json:"tier_odds_milli,omitempty"`
}
type MarketBatchInput struct {
	SessionID        string            `json:"session_id"`
	Issue            string            `json:"issue"`
	UnitCents        int64             `json:"unit_cents"`
	AmountCents      int64             `json:"amount_cents"`
	Selections       []MarketSelection `json:"selections"`
	Key              string            `json:"key"`
	SelectionAmounts map[string]int64  `json:"selection_amounts,omitempty"`
}
type MarketBatchResult struct {
	Orders    []Order `json:"orders"`
	TotalCost int64   `json:"total_cost_cents"`
}

// A multi-selection bet is one atomic transaction. Each selection remains an
// independent order and uses the same snapshots, limits and settlement as a single bet.
func (s *Store) PlaceMarketBatch(id string, in MarketBatchInput, now time.Time) (MarketBatchResult, error) {
	out := MarketBatchResult{Orders: []Order{}}
	err := s.db.Update(func(tx *bolt.Tx) error {
		if now.IsZero() {
			now = time.Now()
		}
		if _, err := active(tx, id); err != nil {
			return err
		}
		prior, err := idem(tx, "market_batch:"+id, in.Key, in)
		if err != nil {
			return err
		}
		if prior != "" {
			var ids []string
			if err = json.Unmarshal([]byte(prior), &ids); err != nil {
				return err
			}
			for _, orderID := range ids {
				o, err := get[Order](tx, "orders", orderID)
				if err != nil {
					return err
				}
				out.Orders = append(out.Orders, o)
				out.TotalCost += o.Cost
			}
			return nil
		}
		if len(in.Selections) < 1 || len(in.Selections) > 500 {
			return errors.New("请选择1–500个投注选项")
		}
		seen, selectedMarkets := map[string]bool{}, map[string]bool{}
		for _, selection := range in.Selections {
			// Do not mutate the request: its original JSON remains the idempotency
			// fingerprint. Equivalent unordered tuples still cannot occur twice.
			values := append([]int(nil), selection.Values...)
			sort.Ints(values)
			selectionKey := fmt.Sprintf("%s:%v", selection.MarketID, values)
			if seen[selectionKey] {
				return errors.New("同一投注选项不能重复提交")
			}
			seen[selectionKey], selectedMarkets[selection.MarketID] = true, true
		}
		for marketID, amount := range in.SelectionAmounts {
			if !selectedMarkets[marketID] {
				return errors.New("单项金额包含未选择的投注选项")
			}
			if amount <= 0 {
				return errors.New("单项投注金额必须大于零")
			}
		}
		ids := make([]string, 0, len(in.Selections))
		for i, selection := range in.Selections {
			amount, overridden := in.SelectionAmounts[selection.MarketID]
			if !overridden {
				amount = in.AmountCents
			}
			item := MarketOrderInput{SessionID: in.SessionID, Issue: in.Issue, MarketID: selection.MarketID, OddsMilli: selection.OddsMilli, UnitCents: in.UnitCents, AmountCents: &amount, Key: tokenKey(fmt.Sprintf("%s:%d", in.Key, i)), Values: selection.Values, TierOddsMilli: selection.TierOddsMilli}
			o, err := placeMarketOrder(tx, id, item, now, "market_batch_item:")
			if err != nil {
				return fmt.Errorf("第%d注：%w；本次投注未提交", i+1, err)
			}
			out.Orders = append(out.Orders, o)
			out.TotalCost += o.Cost
			ids = append(ids, o.ID)
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		return remember(tx, "market_batch:"+id, in.Key, string(encoded), in)
	})
	if err != nil {
		return MarketBatchResult{Orders: []Order{}}, err
	}
	return out, nil
}
