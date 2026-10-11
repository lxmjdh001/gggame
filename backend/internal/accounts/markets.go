package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
	"time"
)

type Quote struct {
	MarketID  string `json:"market_id"`
	OddsMilli int64  `json:"odds_milli"`
	Enabled   bool   `json:"enabled"`
}
type MarketBook struct {
	SessionID string           `json:"session_id"`
	Markets   []markets.Market `json:"markets"`
	Quotes    []Quote          `json:"quotes"`
}
type MarketOrderInput struct {
	AmountCents *int64 `json:"amount_cents,omitempty"`
	SessionID   string `json:"session_id"`
	Issue       string `json:"issue"`
	MarketID    string `json:"market_id"`
	OddsMilli   int64  `json:"odds_milli"`
	UnitCents   int64  `json:"unit_cents"`
	Multiplier  int    `json:"multiplier"`
	Key         string `json:"key"`
}

func defaultQuotes(g games.Session) []Quote {
	v := []Quote{}
	for _, m := range markets.Catalog(g.GameID) {
		v = append(v, Quote{m.ID, m.DefaultOdds, true})
	}
	return v
}
func (s *Store) MarketBook(id string) (MarketBook, error) {
	g, ok := games.Find(id)
	if !ok {
		return MarketBook{}, ErrNotFound
	}
	v := MarketBook{SessionID: id, Markets: markets.Catalog(g.GameID)}
	e := s.db.View(func(tx *bolt.Tx) error { var e error; v.Quotes, e = get[[]Quote](tx, "market_quotes", id); return e })
	return v, e
}
func (s *Store) SaveQuotes(actor, id string, quotes []Quote) error {
	g, ok := games.Find(id)
	if !ok {
		return ErrNotFound
	}
	all := markets.Catalog(g.GameID)
	if len(quotes) != len(all) {
		return errors.New("请提交该场次完整赔率表")
	}
	seen := map[string]bool{}
	for _, q := range quotes {
		if _, ok := markets.Find(g.GameID, q.MarketID); !ok || seen[q.MarketID] || q.OddsMilli < 1000 || q.OddsMilli > 1000000 {
			return errors.New("赔率必须为1.000–1000.000，玩法不可重复或缺失")
		}
		seen[q.MarketID] = true
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		old, e := get[[]Quote](tx, "market_quotes", id)
		if e != nil {
			return e
		}
		if e = put(tx, "market_quotes", id, quotes); e != nil {
			return e
		}
		detail, _ := json.Marshal(map[string]any{"before": old, "after": quotes})
		return audit(tx, actor, "odds.update", id, string(detail))
	})
}
func (s *Store) PlaceMarketOrder(id string, in MarketOrderInput, now time.Time) (Order, error) {
	var out Order
	g, ok := games.Find(in.SessionID)
	if !ok {
		return out, errors.New("场次不存在")
	}
	m, ok := markets.Find(g.GameID, in.MarketID)
	if !ok {
		return out, errors.New("玩法不存在")
	}
	e := s.db.Update(func(tx *bolt.Tx) error {
		// Production callers sample time after acquiring the write transaction.
		if now.IsZero() {
			now = time.Now()
		}
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		prior, e := idem(tx, "market_order:"+id, in.Key, in)
		if e != nil {
			return e
		}
		if prior != "" {
			out, e = get[Order](tx, "orders", prior)
			return e
		}
		cfg, e := get[Settings](tx, "settings", "global")
		if e != nil {
			return e
		}
		var p PlaySetting
		for _, v := range cfg.Plays {
			if v.SessionID == g.ID {
				p = v
			}
		}
		if !p.Enabled {
			return errors.New("场次已暂停接单")
		}
		if in.UnitCents != p.UnitCents {
			return errors.New("单注金额已变更，请重新确认")
		}
		cost, e := orderStake(p, in.AmountCents, in.Multiplier)
		if e != nil {
			return e
		}
		due := g.Next(now)
		if in.Issue != due.Format(model.IssueLayout) || !g.BettingOpen(now) {
			return errors.New("期号已更新或本期已封盘")
		}
		quotes, e := get[[]Quote](tx, "market_quotes", g.ID)
		if e != nil {
			return e
		}
		var q Quote
		for _, v := range quotes {
			if v.MarketID == in.MarketID {
				q = v
			}
		}
		if !q.Enabled {
			return errors.New("该投注选项已暂停")
		}
		if q.OddsMilli != in.OddsMilli {
			return errors.New("赔率已变更，请刷新并重新确认")
		}
		if cost*q.OddsMilli/1000 > maxMoney {
			return errors.New("本注预计返还超过账户限额，请降低投注金额")
		}
		if e = reserveIssueStake(tx, id, g.ID, in.Issue, cost, p.UserIssueLimitCents); e != nil {
			return e
		}
		out = Order{ID: seq(tx, "orders"), UserID: id, Username: u.Username, SessionID: g.ID, SessionName: g.Name, GameName: g.GameName, Issue: in.Issue, Numbers: []int{}, Multiplier: in.Multiplier, Cost: cost, Status: "pending", InviterID: u.InviterID, CommissionBPS: cfg.CommissionBPS, CreatedAt: now, MarketID: m.ID, MarketName: m.Group + " · " + m.Label, OddsMilli: q.OddsMilli, RuleSnapshot: &m}
		parent := u.InviterID
		for level := 0; level < 3 && parent != ""; level++ {
			up, e := get[storedUser](tx, "users", parent)
			if e != nil {
				return e
			}
			out.Ancestors = append(out.Ancestors, up.ID)
			parent = up.InviterID
		}
		if e = ledger(tx, &u, -out.Cost, 0, "order", out.ID, g.GameName+g.Name+" "+out.MarketName); e != nil {
			return e
		}
		if e = put(tx, "orders", out.ID, out); e != nil {
			return e
		}
		if e = tx.Bucket([]byte("pending_markets")).Put([]byte(out.ID), []byte(out.ID)); e != nil {
			return e
		}
		return remember(tx, "market_order:"+id, in.Key, out.ID, in)
	})
	return out, e
}

// Pending is indexed separately, so the scheduler does not scan completed order history.
func (s *Store) PendingMarkets() ([]Order, error) {
	items := []Order{}
	e := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("pending_markets")).ForEach(func(k, v []byte) error {
			o, e := get[Order](tx, "orders", string(k))
			if e != nil {
				return e
			}
			items = append(items, o)
			return nil
		})
	})
	return items, e
}
func createCommissions(tx *bolt.Tx, o Order) error {
	for level, ancestor := range o.Ancestors {
		if level >= 3 {
			break
		}
		amount := o.Cost * o.CommissionBPS[level] / 10000
		if amount <= 0 {
			continue
		}
		c := Commission{Level: level + 1, ID: seq(tx, "commissions"), UserID: ancestor, FromUserID: o.UserID, FromUsername: o.Username, OrderID: o.ID, ValidStake: o.Cost, Rate: o.CommissionBPS[level], Amount: amount, Status: "available", CreatedAt: time.Now()}
		if e := put(tx, "commissions", c.ID, c); e != nil {
			return e
		}
	}
	return nil
}

// calculateMarket is pure: precomputation must not change public order or wallet state.
func calculateMarket(o Order, r games.Round) (string, int64, error) {
	if o.RuleSnapshot == nil || o.MarketID == "" {
		return "", 0, errors.New("旧版订单需人工结算")
	}
	if o.Issue != r.Issue || r.Issue != r.DrawAt.In(model.Beijing).Format(model.IssueLayout) || r.SourceIssue != r.Issue {
		return "", 0, errors.New("开奖结果期号不匹配")
	}
	g, ok := games.Find(o.SessionID)
	if !ok {
		return "", 0, errors.New("场次不存在")
	}
	if r.SourceGroup != g.Group {
		return "", 0, errors.New("开奖源组不匹配")
	}
	outcome, e := markets.Evaluate(*o.RuleSnapshot, g, r)
	if e != nil {
		return "", 0, e
	}
	payout := int64(0)
	if outcome == "win" {
		payout = o.Cost * o.OddsMilli / 1000
	}
	if outcome == "push" {
		payout = o.Cost
	}
	return outcome, payout, nil
}

// All money, order, commission, and pending-index writes share the caller's transaction.
func applyMarket(tx *bolt.Tx, o Order, r games.Round, outcome string, payout int64, now time.Time) error {
	u, e := get[storedUser](tx, "users", o.UserID)
	if e != nil {
		return e
	}
	if payout > 0 {
		if e = ledger(tx, &u, payout, 0, "order_settled", o.ID, o.MarketName+" 自动判奖："+outcome); e != nil {
			return e
		}
	}
	o.Status = "settled"
	o.Outcome = outcome
	o.Payout = payout
	o.ResultNumbers = append([]int{}, r.Numbers...)
	o.SettledAt = &now
	o.Note = fmt.Sprintf("自动结算；锁定赔率 %.3f；%s", float64(o.OddsMilli)/1000, outcome)
	if e = put(tx, "orders", o.ID, o); e != nil {
		return e
	}
	if outcome != "push" {
		if e = createCommissions(tx, o); e != nil {
			return e
		}
	}
	if e = tx.Bucket([]byte("pending_markets")).Delete([]byte(o.ID)); e != nil {
		return e
	}
	return audit(tx, "system", "order.auto_settle", o.ID, o.Note)
}
func (s *Store) SettleMarket(id string, r games.Round) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		o, e := get[Order](tx, "orders", id)
		if e != nil {
			return e
		}
		if o.Status != "pending" {
			return nil
		}
		now := time.Now()
		if r.DrawAt.After(now) {
			return errors.New("未来开奖结果不可结算")
		}
		outcome, payout, e := calculateMarket(o, r)
		if e != nil {
			return e
		}
		return applyMarket(tx, o, r, outcome, payout, now)
	})
}

type RoundReader interface {
	Get(session, issue string) (games.Round, error)
}
