package accounts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

// User profit excludes referral commissions; platform profit includes their cost.
type UserEstimate struct {
	NextEstimate
	UserProfit *int64 `json:"user_profit_cents"`
}

func (e *UserEstimate) finish(available bool) {
	e.NextEstimate.finish(available)
	if e.Net != nil {
		profit := e.Payout - e.Stake
		e.UserProfit = &profit
	}
}

type UserStatusOrder struct {
	ID               string           `json:"id"`
	Username         string           `json:"username"`
	MarketName       string           `json:"market_name"`
	Numbers          []int            `json:"numbers"`
	OddsMilli        int64            `json:"odds_milli"`
	TierOddsMilli    map[string]int64 `json:"tier_odds_milli,omitempty"`
	PayoutTier       string           `json:"estimated_payout_tier,omitempty"`
	PayoutMultiplier int64            `json:"estimated_payout_multiplier,omitempty"`
	PayoutOddsMilli  int64            `json:"estimated_payout_odds_milli,omitempty"`
	Cost             int64            `json:"cost_cents"`
	CreatedAt        time.Time        `json:"created_at"`
	Outcome          string           `json:"estimated_outcome"`
	Payout           *int64           `json:"estimated_payout_cents"`
	Profit           *int64           `json:"user_profit_cents"`
}
type DrawNumberRule struct {
	Count       int  `json:"count"`
	Min         int  `json:"min"`
	Max         int  `json:"max"`
	AllowRepeat bool `json:"allow_repeat"`
}
type UserStatusSession struct {
	NumberRule DrawNumberRule    `json:"number_rule"`
	SessionID  string            `json:"session_id"`
	Name       string            `json:"name"`
	Issue      string            `json:"issue"`
	DrawAt     time.Time         `json:"draw_at"`
	Round      *games.Round      `json:"round"`
	Estimate   UserEstimate      `json:"estimate"`
	Orders     []UserStatusOrder `json:"orders"`
}
type UserStatus struct {
	AllUsers      bool                `json:"all_users"`
	BettingFrozen int64               `json:"betting_frozen_cents"`
	AsOf          time.Time           `json:"as_of"`
	User          *User               `json:"user"`
	Summary       UserEstimate        `json:"summary"`
	Sessions      []UserStatusSession `json:"sessions"`
}

// Only pending orders for each session's upcoming issue count. An empty username
// includes all users; otherwise the username must match exactly.
// Estimates read the effective preview, including an administrator's adopted draw.
func (s *Store) UserStatus(username string, now time.Time, draws GameControlDrawReader) (UserStatus, error) {
	out := UserStatus{Sessions: []UserStatusSession{}}
	username = strings.ToLower(strings.TrimSpace(username))
	out.AllUsers = username == ""
	err := s.db.View(func(tx *bolt.Tx) error {
		var id string
		var err error
		if !out.AllUsers {
			id = string(tx.Bucket([]byte("usernames")).Get([]byte(username)))
			if id == "" {
				return fmt.Errorf("用户不存在: %w", ErrNotFound)
			}
			user, err := get[storedUser](tx, "users", id)
			if err != nil {
				return err
			}
			out.User = &user.User
		}
		if now.IsZero() {
			now = time.Now()
		}
		now = now.In(model.Beijing)
		out.AsOf = now
		for _, g := range games.All() {
			at := g.Next(now)
			row := UserStatusSession{SessionID: g.ID, Name: g.GameName + " · " + g.Name, Issue: at.Format(model.IssueLayout), DrawAt: at, Orders: []UserStatusOrder{}}
			rule := g.NumberRuleAt(at)
			row.NumberRule = DrawNumberRule{Count: rule.Count, Min: rule.Min, Max: rule.Max, AllowRepeat: rule.AllowRepeat}
			if draws != nil {
				row.Round, err = draws.PreviewRound(g, at)
				if err != nil {
					return err
				}
			}
			out.Sessions = append(out.Sessions, row)
		}
		rows := map[string]*UserStatusSession{}
		for i := range out.Sessions {
			rows[out.Sessions[i].SessionID] = &out.Sessions[i]
		}
		err = tx.Bucket([]byte("orders")).ForEach(func(_, raw []byte) error {
			var o Order
			if err := json.Unmarshal(raw, &o); err != nil {
				return err
			}
			if (!out.AllUsers && o.UserID != id) || o.Status != "pending" {
				return nil
			}
			// All unsettled stakes remain unavailable, including older issues awaiting processing.
			out.BettingFrozen += o.Cost
			row := rows[o.SessionID]
			if row == nil || o.Issue != row.Issue {
				return nil
			}
			row.Estimate.add(o, row.Round)
			item := UserStatusOrder{ID: o.ID, Username: o.Username, MarketName: o.MarketName, Numbers: o.Numbers, OddsMilli: o.OddsMilli, TierOddsMilli: o.TierOddsMilli, Cost: o.Cost, CreatedAt: o.CreatedAt, Outcome: "unavailable"}
			if row.Round != nil {
				detail, err := calculateMarketDetailed(o, *row.Round)
				if err == nil {
					payout := detail.Payout
					profit := payout - o.Cost
					item.Outcome = detail.Outcome
					item.Payout = &payout
					if o.RuleSnapshot.RuleVersion == 6 {
						item.PayoutTier, item.PayoutMultiplier, item.PayoutOddsMilli = detail.Tier, detail.Multiplier, detail.OddsMilli
					}
					item.Profit = &profit
				} else {
					item.Outcome = "manual"
				}
			}
			row.Orders = append(row.Orders, item)
			return nil
		})
		if err != nil {
			return err
		}
		for i := range out.Sessions {
			row := &out.Sessions[i]
			row.Estimate.finish(row.Round != nil || row.Estimate.Orders == 0)
			sort.Slice(row.Orders, func(i, j int) bool { return row.Orders[i].CreatedAt.After(row.Orders[j].CreatedAt) })
			out.Summary.Orders += row.Estimate.Orders
			out.Summary.Stake += row.Estimate.Stake
			out.Summary.Payout += row.Estimate.Payout
			out.Summary.Commission += row.Estimate.Commission
			out.Summary.UnresolvedOrders += row.Estimate.UnresolvedOrders
		}
		out.Summary.finish(true)
		return nil
	})
	return out, err
}
