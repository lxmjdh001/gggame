package accounts

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"time"
)

type ProfitPeriod struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Orders     int       `json:"orders"`
	Stake      int64     `json:"stake_cents"`
	Payout     int64     `json:"payout_cents"`
	Gross      int64     `json:"gross_profit_cents"`
	Commission int64     `json:"commission_cents"`
	Net        int64     `json:"net_profit_cents"`
}
type GameControlDrawReader interface {
	History(id string, limit int, before string) (games.History, error)
	PreviewRound(session games.Session, at time.Time) (*games.Round, error)
}
type NextEstimate struct {
	Orders           int    `json:"orders"`
	Stake            int64  `json:"stake_cents"`
	Payout           int64  `json:"payout_cents"`
	Commission       int64  `json:"commission_cents"`
	Net              *int64 `json:"net_profit_cents"`
	UnresolvedOrders int    `json:"unresolved_orders"`
}
type SessionExposure struct {
	Latest       *games.Round `json:"latest"`
	Next         *games.Round `json:"next"`
	NextDrawAt   time.Time    `json:"next_draw_at"`
	NextIssue    string       `json:"next_issue"`
	NextEstimate NextEstimate `json:"next_estimate"`

	UserIssueLimit int64        `json:"user_issue_limit_cents"`
	SessionID      string       `json:"session_id"`
	Name           string       `json:"name"`
	Group          string       `json:"group"`
	Positions      []int        `json:"positions"`
	Enabled        bool         `json:"enabled"`
	PendingAmount  int64        `json:"pending_amount_cents"`
	PendingOrders  int          `json:"pending_orders"`
	Today          ProfitPeriod `json:"today"`
}
type GameControl struct {
	AsOf          time.Time         `json:"as_of"`
	Today         ProfitPeriod      `json:"today"`
	Yesterday     ProfitPeriod      `json:"yesterday"`
	Month         ProfitPeriod      `json:"month"`
	PendingAmount int64             `json:"pending_amount_cents"`
	PendingOrders int               `json:"pending_orders"`
	UndatedOrders int               `json:"undated_settled_orders"`
	Sessions      []SessionExposure `json:"sessions"`
}

// Read orders, settings and commissions from one consistent snapshot; this never mutates draws or balances.
func (s *Store) GameControl(now time.Time) (GameControl, error) {
	return s.GameControlWithDraws(now, nil)
}

func (s *Store) GameControlWithDraws(now time.Time, draws GameControlDrawReader) (GameControl, error) {
	out := GameControl{Sessions: []SessionExposure{}}
	e := s.db.View(func(tx *bolt.Tx) error {
		if now.IsZero() {
			now = time.Now()
		}
		now = now.In(model.Beijing)
		out.AsOf = now
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, model.Beijing)
		out.Today = ProfitPeriod{Start: start, End: start.AddDate(0, 0, 1)}
		out.Yesterday = ProfitPeriod{Start: start.AddDate(0, 0, -1), End: start}
		out.Month = ProfitPeriod{Start: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, model.Beijing), End: time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, model.Beijing)}
		cfg, e := get[Settings](tx, "settings", "global")
		if e != nil {
			return e
		}
		enabled := map[string]bool{}
		limits := map[string]int64{}
		for _, p := range cfg.Plays {
			enabled[p.SessionID] = p.Enabled
			limits[p.SessionID] = p.UserIssueLimitCents
		}
		for _, g := range games.All() {
			row := SessionExposure{UserIssueLimit: limits[g.ID], SessionID: g.ID, Name: g.GameName + " · " + g.Name, Group: g.Group, Positions: g.Positions, Enabled: enabled[g.ID], Today: ProfitPeriod{Start: out.Today.Start, End: out.Today.End}, NextDrawAt: g.Next(now)}
			row.NextIssue = row.NextDrawAt.Format(model.IssueLayout)
			if draws != nil {
				history, err := draws.History(g.ID, 1, "")
				if err != nil {
					return err
				}
				if len(history.Items) > 0 {
					row.Latest = &history.Items[0]
				}
				row.Next, err = draws.PreviewRound(g, row.NextDrawAt)
				if err != nil {
					return err
				}
			}
			out.Sessions = append(out.Sessions, row)
		}
		rows := map[string]*SessionExposure{}
		for i := range out.Sessions {
			rows[out.Sessions[i].SessionID] = &out.Sessions[i]
		}
		targets := map[string][]*ProfitPeriod{}
		e = tx.Bucket([]byte("orders")).ForEach(func(_, v []byte) error {
			var o Order
			if e := json.Unmarshal(v, &o); e != nil {
				return e
			}
			row := rows[o.SessionID]
			if o.Status == "pending" {
				out.PendingAmount += o.Cost
				out.PendingOrders++
				if row != nil {
					row.PendingAmount += o.Cost
					row.PendingOrders++
					if o.Issue == row.NextIssue {
						row.NextEstimate.add(o, row.Next)
					}
				}
				return nil
			}
			if o.Status != "settled" {
				return nil
			}
			if o.SettledAt == nil {
				out.UndatedOrders++
				return nil
			}
			if o.SettledAt.After(now) {
				return nil
			}
			periods := []*ProfitPeriod{&out.Today, &out.Yesterday, &out.Month}
			if row != nil {
				periods = append(periods, &row.Today)
			}
			for _, p := range periods {
				if !o.SettledAt.Before(p.Start) && o.SettledAt.Before(p.End) {
					p.Orders++
					p.Stake += o.Cost
					p.Payout += o.Payout
					targets[o.ID] = append(targets[o.ID], p)
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		e = tx.Bucket([]byte("commissions")).ForEach(func(_, v []byte) error {
			var c Commission
			if e := json.Unmarshal(v, &c); e != nil {
				return e
			}
			for _, p := range targets[c.OrderID] {
				p.Commission += c.Amount
			}
			return nil
		})
		if e != nil {
			return e
		}
		finish := func(p *ProfitPeriod) { p.Gross = p.Stake - p.Payout; p.Net = p.Gross - p.Commission }
		finish(&out.Today)
		finish(&out.Yesterday)
		finish(&out.Month)
		for i := range out.Sessions {
			row := &out.Sessions[i]
			finish(&row.Today)
			row.NextEstimate.finish(row.Next != nil)
		}
		return nil
	})
	return out, e
}

// The dashboard and random preview use the same locked-order calculation as payouts.
func (estimate *NextEstimate) add(o Order, round *games.Round) {
	estimate.Orders++
	estimate.Stake += o.Cost
	if round == nil {
		estimate.UnresolvedOrders++
		return
	}
	outcome, payout, err := calculateMarket(o, *round)
	if err != nil {
		estimate.UnresolvedOrders++
		return
	}
	estimate.Payout += payout
	if outcome != "push" {
		for level := range o.Ancestors {
			if level >= 3 {
				break
			}
			amount := o.Cost * o.CommissionBPS[level] / 10000
			if amount > 0 {
				estimate.Commission += amount
			}
		}
	}
}
func (estimate *NextEstimate) finish(available bool) {
	estimate.Net = nil
	if available && estimate.UnresolvedOrders == 0 {
		net := estimate.Stake - estimate.Payout - estimate.Commission
		estimate.Net = &net
	}
}

func (s *Store) EstimateRound(round games.Round) (NextEstimate, error) {
	estimates, err := s.EstimateRounds([]games.Round{round})
	return estimates[0], err
}

// Compare the active and candidate results against one consistent order snapshot.
func (s *Store) EstimateRounds(rounds []games.Round) ([]NextEstimate, error) {
	estimates := make([]NextEstimate, len(rounds))
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("orders")).ForEach(func(_, value []byte) error {
			var order Order
			if err := json.Unmarshal(value, &order); err != nil {
				return err
			}
			if order.Status != "pending" {
				return nil
			}
			for i := range rounds {
				round := &rounds[i]
				if order.SessionID == round.SessionID && order.Issue == round.Issue {
					estimates[i].add(order, round)
				}
			}
			return nil
		})
	})
	for i := range estimates {
		estimates[i].finish(true)
	}
	return estimates, err
}
