package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"log/slog"
	"sort"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"time"
)

// 整分后10ms开始预计算，05秒公布结果后才允许入账。
const settlementPrepareOffset = 10 * time.Millisecond

type settlementUser struct {
	ID     string
	Orders []string
}
type settlementPlan struct {
	Issue   string
	DrawAt  time.Time
	Batches [][]settlementUser
}
type preparedItem struct {
	OrderID, OrderDigest, ResultDigest, Outcome string
	Payout                                      int64
}
type preparedUser struct {
	Items []preparedItem
	Done  bool
}
type preparedRoundReader interface {
	PreviewRound(games.Session, time.Time) (*games.Round, error)
}

func privateKey(issue, user string) string { return issue + ":" + user }
func digestOrder(o Order) string           { b, _ := json.Marshal(o); return tokenKey(string(b)) }
func digestResult(r games.Round) string {
	b, _ := json.Marshal([]any{r.SessionID, r.Issue, r.SourceIssue, r.SourceGroup, r.Positions, r.Numbers, r.DrawAt.UnixNano(), r.Origin})
	return tokenKey(string(b))
}
func prepareAt(p settlementPlan) time.Time {
	return p.DrawAt.Add(-time.Duration(games.DrawSecond)*time.Second + settlementPrepareOffset)
}

// Keep the persisted Batches field readable for plans created by the old scheduler.
// All old groups now execute together, while per-user prepared/done markers survive.
func planUsers(p settlementPlan) []settlementUser {
	users := []settlementUser{}
	for _, batch := range p.Batches {
		users = append(users, batch...)
	}
	return users
}

// Freeze membership once betting is closed, including every session sharing the issue.
// Refunds may remove obligations later; membership remains stable across restarts.
func (s *Store) discoverSettlementPlans(now time.Time) error {
	// Most ticks have no newly closed orders; avoid an unnecessary write/fsync.
	needed := false
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("pending_markets")).ForEach(func(k, v []byte) error {
			if needed {
				return nil
			}
			o, e := get[Order](tx, "orders", string(k))
			if e != nil {
				return e
			}
			if tx.Bucket([]byte("settlement_plans")).Get([]byte(o.Issue)) != nil {
				return nil
			}
			at, e := model.ParseIssue(o.Issue)
			if e != nil {
				return e
			}
			needed = !now.Before(at.Add(games.SourcePublishDelay - 15*time.Second))
			return nil
		})
	}); err != nil {
		return err
	}
	if !needed {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		groups := map[string]map[string][]string{}
		times := map[string]time.Time{}
		err := tx.Bucket([]byte("pending_markets")).ForEach(func(k, v []byte) error {
			o, e := get[Order](tx, "orders", string(k))
			if e != nil {
				return e
			}
			if o.Status != "pending" || tx.Bucket([]byte("settlement_plans")).Get([]byte(o.Issue)) != nil {
				return nil
			}
			g, ok := games.Find(o.SessionID)
			if !ok {
				return fmt.Errorf("unknown pending session")
			}
			at, e := model.ParseIssue(o.Issue)
			if e != nil {
				return e
			}
			at = at.Add(games.SourcePublishDelay)
			if !g.Matches(at) {
				return fmt.Errorf("invalid pending issue boundary")
			}
			if now.Before(g.CloseAt(at)) {
				return nil
			}
			if groups[o.Issue] == nil {
				groups[o.Issue] = map[string][]string{}
				times[o.Issue] = at
			}
			groups[o.Issue][o.UserID] = append(groups[o.Issue][o.UserID], o.ID)
			return nil
		})
		if err != nil {
			return err
		}
		for issue, users := range groups {
			ids := make([]string, 0, len(users))
			for id := range users {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			p := settlementPlan{Issue: issue, DrawAt: times[issue], Batches: make([][]settlementUser, 1)}
			for _, id := range ids {
				p.Batches[0] = append(p.Batches[0], settlementUser{id, users[id]})
			}
			if err = put(tx, "settlement_plans", issue, p); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) settlementPlans() ([]settlementPlan, error) {
	var p []settlementPlan
	err := s.db.View(func(tx *bolt.Tx) error { var e error; p, e = list[settlementPlan](tx, "settlement_plans"); return e })
	sort.Slice(p, func(i, j int) bool { return p[i].Issue < p[j].Issue })
	return p, err
}

// Load private rounds once per pass; none of these are returned by member APIs.
func preparedRound(reader RoundReader, g games.Session, at time.Time) (games.Round, error) {
	if preview, ok := reader.(preparedRoundReader); ok {
		r, err := preview.PreviewRound(g, at)
		if err != nil {
			return games.Round{}, err
		}
		if r != nil {
			return *r, nil
		}
	}
	return reader.Get(g.ID, at.In(model.Beijing).Format(model.IssueLayout))
}
func (s *Store) prepareUsers(p settlementPlan, users []settlementUser, reader RoundReader) error {
	rounds := map[string]games.Round{}
	// Read number storage outside the account write transaction.
	orders := map[string]Order{}
	missing := false
	err := s.db.View(func(tx *bolt.Tx) error {
		for _, u := range users {
			if tx.Bucket([]byte("prepared_payouts")).Get([]byte(privateKey(p.Issue, u.ID))) != nil {
				continue
			}
			missing = true
			for _, id := range u.Orders {
				o, e := get[Order](tx, "orders", id)
				if e != nil {
					return e
				}
				if o.Status == "pending" {
					orders[id] = o
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !missing {
		return nil
	}
	for _, o := range orders {
		if _, ok := rounds[o.SessionID]; ok {
			continue
		}
		g, _ := games.Find(o.SessionID)
		r, e := preparedRound(reader, g, p.DrawAt)
		if e != nil {
			return e
		}
		rounds[g.ID] = r
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("settlement_plans")).Get([]byte(p.Issue)) == nil {
			return nil
		}

		for _, u := range users {
			key := privateKey(p.Issue, u.ID)
			if tx.Bucket([]byte("prepared_payouts")).Get([]byte(key)) != nil {
				continue
			}
			prepared := preparedUser{Items: []preparedItem{}}
			for _, id := range u.Orders {
				o, e := get[Order](tx, "orders", id)
				if e != nil {
					return e
				}
				if o.Status != "pending" {
					continue
				}
				r, ok := rounds[o.SessionID]
				if !ok {
					return errors.New("prepared source unavailable")
				}
				if o.UserID != u.ID || o.Issue != p.Issue {
					return errors.New("settlement membership mismatch")
				}
				outcome, payout, e := calculateMarket(o, r)
				if e != nil {
					return e
				}
				prepared.Items = append(prepared.Items, preparedItem{id, digestOrder(o), digestResult(r), outcome, payout})
			}
			if e := put(tx, "prepared_payouts", key, prepared); e != nil {
				return e
			}
		}
		return nil
	})
}

func (s *Store) commitUsers(p settlementPlan, users []settlementUser, published map[string]games.Round, now time.Time) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("settlement_plans")).Get([]byte(p.Issue)) == nil {
			return nil
		}

		// The runtime clock is sampled after acquiring the write lock.
		if now.IsZero() {
			now = time.Now()
		}
		if now.Before(p.DrawAt) {
			return errors.New("开奖前不可入账")
		}
		for _, u := range users {
			key := privateKey(p.Issue, u.ID)
			prepared, e := get[preparedUser](tx, "prepared_payouts", key)
			if errors.Is(e, ErrNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			if prepared.Done {
				continue
			}
			for i := range prepared.Items {
				item := &prepared.Items[i]
				o, e := get[Order](tx, "orders", item.OrderID)
				if e != nil {
					return e
				}
				if o.Status != "pending" {
					continue
				}
				if o.UserID != u.ID || o.Issue != p.Issue || digestOrder(o) != item.OrderDigest {
					return errors.New("订单快照变更，暂停该用户入账")
				}
				r, ok := published[o.SessionID]
				if !ok || r.DrawAt.After(now) || !r.DrawAt.Equal(p.DrawAt) || r.SessionID != o.SessionID || r.Issue != p.Issue {
					return errors.New("公开结果尚未就绪或场次期号不匹配，暂停入账")
				}
				if digestResult(r) != item.ResultDigest {
					// Recover only pending orders with unchanged order snapshots, using
					// the published history. Recalculation, audit and posting are atomic;
					// completed/refunded orders are never paid again.
					outcome, payout, err := calculateMarket(o, r)
					if err != nil {
						return err
					}
					item.Outcome, item.Payout, item.ResultDigest = outcome, payout, digestResult(r)
					if err = audit(tx, "system", "order.payout_recalculate", o.ID, "按已公布开奖结果重新计算未入账赔付"); err != nil {
						return err
					}
				}
				if e = applyMarket(tx, o, r, item.Outcome, item.Payout, now); e != nil {
					return e
				}
			}
			prepared.Done = true
			if e = put(tx, "prepared_payouts", key, prepared); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Store) cleanupPlan(p settlementPlan) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, batch := range p.Batches {
			for _, u := range batch {
				prepared, e := get[preparedUser](tx, "prepared_payouts", privateKey(p.Issue, u.ID))
				if errors.Is(e, ErrNotFound) {
					return nil
				}
				if e != nil {
					return e
				}
				if !prepared.Done {
					return nil
				}
			}
		}
		for _, batch := range p.Batches {
			for _, u := range batch {
				if e := tx.Bucket([]byte("prepared_payouts")).Delete([]byte(privateKey(p.Issue, u.ID))); e != nil {
					return e
				}
			}
		}
		return tx.Bucket([]byte("settlement_plans")).Delete([]byte(p.Issue))
	})
}

func (s *Store) processSettlementPlans(ctx context.Context, reader RoundReader, now time.Time, logger *slog.Logger) error {
	plans, err := s.settlementPlans()
	if err != nil {
		return err
	}
	for _, p := range plans {
		published := map[string]games.Round{}
		if !now.Before(p.DrawAt) {
			for _, g := range games.All() {
				if r, e := reader.Get(g.ID, p.Issue); e == nil {
					published[g.ID] = r
				}
			}
		}
		if now.Before(prepareAt(p)) {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		users := []settlementUser{}
		if err := s.db.View(func(tx *bolt.Tx) error {
			for _, u := range planUsers(p) {
				prepared, e := get[preparedUser](tx, "prepared_payouts", privateKey(p.Issue, u.ID))
				if e != nil && !errors.Is(e, ErrNotFound) {
					return e
				}
				if !prepared.Done {
					users = append(users, u)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if len(users) > 0 {
			// One calculation for the entire issue; individual retries only on failure.
			if e := s.prepareUsers(p, users, reader); e != nil {
				for _, u := range users {
					if e := s.prepareUsers(p, []settlementUser{u}, reader); e != nil {
						logger.Warn("payout preparation pending", "issue", p.Issue, "user", u.ID, "error", e)
					}
				}
			}
			if !now.Before(p.DrawAt) {
				if e := s.commitUsers(p, users, published, time.Time{}); e != nil {
					for _, u := range users {
						if e := s.commitUsers(p, []settlementUser{u}, published, time.Time{}); e != nil {
							logger.Error("payout posting pending", "issue", p.Issue, "user", u.ID, "error", e)
						}
					}
				}
			}
		}
		if !now.Before(p.DrawAt) {
			if err := s.cleanupPlan(p); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) RunSettlement(ctx context.Context, reader RoundReader, logger *slog.Logger) {
	logger.Info("settlement worker started", "mode", "single_pass", "prepare_start_second", 0.01, "target_finish_second", 5, "publish_second", 5)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastDiscovery time.Time
	for ctx.Err() == nil {
		now := time.Now()
		if lastDiscovery.IsZero() || now.Sub(lastDiscovery) >= time.Second {
			if err := s.discoverSettlementPlans(now); err != nil {
				logger.Error("settlement planning pending", "error", err)
			}
			lastDiscovery = now
		}
		if err := s.processSettlementPlans(ctx, reader, now, logger); err != nil && ctx.Err() == nil {
			logger.Error("settlement batches pending", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
