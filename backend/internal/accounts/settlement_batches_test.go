package accounts

import (
	"context"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"io"
	"log/slog"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"sync"
	"testing"
	"time"
)

type batchReader struct {
	rounds    map[string]games.Round
	published bool
}

func (r *batchReader) Get(session, issue string) (games.Round, error) {
	v, ok := r.rounds[session]
	if !ok || !r.published || v.Issue != issue {
		return games.Round{}, ErrNotFound
	}
	return v, nil
}
func (r *batchReader) PreviewRound(g games.Session, at time.Time) (*games.Round, error) {
	v, ok := r.rounds[g.ID]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

var batchLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// Fixture setup excludes registration/password hashing, but submissions use the real
// order path with balance debits, limit checks, idempotency and disk synchronization.
func batchFixture(t *testing.T, count, ordersPerUser int) (*Store, string, time.Time, *batchReader) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	at := time.Now().In(model.Beijing).Add(-time.Hour).Truncate(30 * time.Minute).Add(5 * time.Second)
	reader := &batchReader{rounds: map[string]games.Round{}}
	for _, id := range []string{"ffc-1", "ffc-3", "ffc-5"} {
		g, _ := games.Find(id)
		reader.rounds[id] = games.Round{SessionID: id, Issue: at.Format(model.IssueLayout), SourceIssue: at.Format(model.IssueLayout), SourceGroup: g.Group, Positions: g.Positions, Numbers: []int{8, 8, 8, 8, 8}, Origin: "scheduled", DrawAt: at}
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("u%03d", i)
			if e := put(tx, "users", id, storedUser{User: User{ID: id, Username: id, Role: "member", Status: "active", Balance: 1000000, InviterID: "parent1"}}); e != nil {
				return e
			}
		}
		for i := 1; i <= 3; i++ {
			id := fmt.Sprintf("parent%d", i)
			parent := ""
			if i < 3 {
				parent = fmt.Sprintf("parent%d", i+1)
			}
			if e := put(tx, "users", id, storedUser{User: User{ID: id, Username: id, Status: "active", Role: "member", InviterID: parent}}); e != nil {
				return e
			}
		}
		cfg, e := get[Settings](tx, "settings", "global")
		if e != nil {
			return e
		}
		cfg.CommissionBPS = [3]int64{100, 50, 25}
		return put(tx, "settings", "global", cfg)
	}))
	started := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < ordersPerUser; n++ {
				id := fmt.Sprintf("u%03d", i)
				session := []string{"ffc-1", "ffc-3", "ffc-5"}[n%3]
				market := []string{"p1.big", "p1.small", "dt.dragon"}[n%3]
				in := MarketOrderInput{SessionID: session, Issue: at.Format(model.IssueLayout), MarketID: market, OddsMilli: 1960, UnitCents: 200, Multiplier: 50, Key: fmt.Sprintf("fixture-%03d-%03d-key", i, n)}
				if _, e := s.PlaceMarketOrder(id, in, at.Add(-30*time.Second)); e != nil {
					t.Error(e)
				}
			}
		}(i)
	}
	wg.Wait()
	t.Logf("%d users x %d orders: concurrent submission %s", count, ordersPerUser, time.Since(started))
	return s, path, at, reader
}
func preparedCount(t *testing.T, s *Store) int {
	t.Helper()
	n := 0
	must(t, s.db.View(func(tx *bolt.Tx) error { n = tx.Bucket([]byte("prepared_payouts")).Stats().KeyN; return nil }))
	return n
}
func assertNoEarlyPayout(t *testing.T, s *Store) {
	t.Helper()
	must(t, s.db.View(func(tx *bolt.Tx) error {
		orders, e := list[Order](tx, "orders")
		if e != nil {
			return e
		}
		for _, o := range orders {
			if o.Status != "pending" || o.Payout != 0 || len(o.ResultNumbers) > 0 {
				t.Fatal("public order leaked result")
			}
		}
		if tx.Bucket([]byte("commissions")).Stats().KeyN != 0 {
			t.Fatal("early commission")
		}
		entries, e := list[Ledger](tx, "ledger")
		if e != nil {
			return e
		}
		for _, l := range entries {
			if l.Kind != "order" {
				t.Fatal("early payout ledger")
			}
		}
		return nil
	}))
}

func TestSinglePass30UsersConcurrentOrdersRestartAndPosting(t *testing.T) {
	s, path, at, reader := batchFixture(t, 30, 10)
	defer func() { s.Close() }()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second-time.Nanosecond)))
	plans, e := s.settlementPlans()
	must(t, e)
	if len(plans) != 0 {
		t.Fatal("planned before close")
	}
	started := time.Now()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	t.Logf("plan: %s", time.Since(started))
	plans, e = s.settlementPlans()
	must(t, e)
	if len(plans) != 1 || len(plans[0].Batches) != 1 {
		t.Fatal("must use one group regardless of user count")
	}
	p := plans[0]
	for _, u := range planUsers(p) {
		if len(u.Orders) != 10 {
			t.Fatal("cross-session user split")
		}
	}
	if len(planUsers(p)) != 30 {
		t.Fatal("missing users")
	}
	for _, now := range []time.Time{at.Add(-15 * time.Second), at.Add(-5 * time.Second), prepareAt(p).Add(-time.Nanosecond)} {
		must(t, s.processSettlementPlans(context.Background(), reader, now, batchLogger))
		if preparedCount(t, s) != 0 {
			t.Fatal("prepared before00.010")
		}
	}
	started = time.Now()
	must(t, s.processSettlementPlans(context.Background(), reader, prepareAt(p), batchLogger))
	t.Logf("single calculation (30 users / 300 orders): %s", time.Since(started))
	if preparedCount(t, s) != 30 {
		t.Fatal("not all users prepared in one pass")
	}
	assertNoEarlyPayout(t, s)
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	for _, now := range []time.Time{at.Add(-2 * time.Second), at.Add(-time.Nanosecond)} {
		must(t, s.processSettlementPlans(context.Background(), reader, now, batchLogger))
		if preparedCount(t, s) != 30 {
			t.Fatal("prepared work lost on restart")
		}
		assertNoEarlyPayout(t, s)
	}
	// A due timestamp alone cannot authorize posting before a public round exists.
	must(t, s.processSettlementPlans(context.Background(), reader, at, batchLogger))
	assertNoEarlyPayout(t, s)
	reader.published = true
	// Concurrent duplicate processing exercises atomic progress + wallet updates.
	started = time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.processSettlementPlans(context.Background(), reader, at, batchLogger); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	t.Logf("posting 30 users / 300 orders (4 duplicate workers): %s", time.Since(started))
	must(t, s.discoverSettlementPlans(at))
	plans, e = s.settlementPlans()
	must(t, e)
	if len(plans) != 0 {
		t.Fatal("completed plan retained")
	}
	if preparedCount(t, s) != 0 {
		t.Fatal("orphaned private payouts")
	}
	pending, e := s.PendingMarkets()
	must(t, e)
	if len(pending) != 0 {
		t.Fatal("pending orders remain", len(pending))
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for i := 0; i < 30; i++ {
			u, e := get[storedUser](tx, "users", fmt.Sprintf("u%03d", i))
			if e != nil {
				return e
			}
			if u.Balance != 1008400 {
				t.Fatalf("wrong/duplicate balance: %d", u.Balance)
			}
		}
		commissions, e := list[Commission](tx, "commissions")
		if e != nil {
			return e
		}
		if len(commissions) != 630 {
			t.Fatal("commission count", len(commissions))
		}
		var total int64
		for _, c := range commissions {
			total += c.Amount
		}
		if total != 36750 {
			t.Fatal("commission total", total)
		}
		ledgers, e := list[Ledger](tx, "ledger")
		if e != nil {
			return e
		}
		if len(ledgers) != 510 {
			t.Fatal("duplicate or missing ledgers", len(ledgers))
		}
		orders, e := list[Order](tx, "orders")
		if e != nil {
			return e
		}
		for _, o := range orders {
			if o.Status != "settled" || o.SettledAt == nil {
				t.Fatal("unsettled order")
			}
		}
		return nil
	}))
}

func TestBatchSmallUsersRefundAndResultMismatch(t *testing.T) {
	s, _, at, reader := batchFixture(t, 2, 3)
	defer s.Close()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, e := s.settlementPlans()
	must(t, e)
	p := plans[0]
	if len(p.Batches) != 1 {
		t.Fatal("small group should use one batch")
	}
	must(t, s.prepareUsers(p, p.Batches[0], reader))
	// A published result for the wrong source group must roll back all writes.
	published := map[string]games.Round{}
	for k, v := range reader.rounds {
		published[k] = v
	}
	bad := published["ffc-5"]
	bad.SourceGroup = "wrong-group"
	published["ffc-5"] = bad
	if e = s.commitUsers(p, p.Batches[0], published, at); e == nil {
		t.Fatal("invalid published result accepted")
	}
	assertNoEarlyPayout(t, s)
	if e = s.commitUsers(p, p.Batches[0], reader.rounds, at.Add(-time.Nanosecond)); e == nil {
		t.Fatal("early posting accepted")
	}
	// A changed order snapshot must roll back the whole user group as well.
	original, e := s.Order(p.Batches[0][1].Orders[0])
	must(t, e)
	altered := original
	altered.Cost++
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "orders", altered.ID, altered) }))
	if e = s.commitUsers(p, p.Batches[0], reader.rounds, at); e == nil {
		t.Fatal("changed order accepted")
	}
	assertNoEarlyPayout(t, s)
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "orders", original.ID, original) }))
	// Refund after precompute: use the existing administrative refund path.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "users", "admin-test", storedUser{User: User{ID: "admin-test", Role: "admin", Status: "active"}})
	}))
	refunded := p.Batches[0][0].Orders[0]
	must(t, s.SettleOrder("admin-test", refunded, "refunded", "测试预计算后退款", 0, false))
	must(t, s.commitUsers(p, p.Batches[0], reader.rounds, at))
	o, e := s.Order(refunded)
	must(t, e)
	if o.Status != "refunded" {
		t.Fatal("refund overwritten")
	}
	must(t, s.commitUsers(p, p.Batches[0], reader.rounds, at))
}

func TestBatchFailureIsolatesUserAndPreservesAtomicWallet(t *testing.T) {
	s, _, at, reader := batchFixture(t, 2, 3)
	defer s.Close()
	must(t, s.discoverSettlementPlans(at))
	plans, e := s.settlementPlans()
	must(t, e)
	p := plans[0]
	must(t, s.prepareUsers(p, p.Batches[0], reader))
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "u000")
		if e != nil {
			return e
		}
		u.Balance = maxMoney
		return put(tx, "users", u.ID, u)
	}))
	reader.published = true
	must(t, s.processSettlementPlans(context.Background(), reader, at, batchLogger))
	pending, e := s.PendingMarkets()
	must(t, e)
	if len(pending) != 3 {
		t.Fatal("failure affected other users", len(pending))
	}
	for _, o := range pending {
		if o.UserID != "u000" {
			t.Fatal("wrong blocked user")
		}
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "u000")
		if e != nil {
			return e
		}
		u.Balance = 970000
		return put(tx, "users", u.ID, u)
	}))
	must(t, s.processSettlementPlans(context.Background(), reader, at, batchLogger))
	pending, e = s.PendingMarkets()
	must(t, e)
	if len(pending) != 0 {
		t.Fatal("retry failed")
	}
}

func TestSinglePassLegacyPlanAndMidnightRecovery(t *testing.T) {
	s, path, at, reader := batchFixture(t, 12, 1)
	defer func() { s.Close() }()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, e := s.settlementPlans()
	must(t, e)
	p := plans[0]
	users := planUsers(p)
	p.Batches = make([][]settlementUser, 6)
	for i, u := range users {
		p.Batches[i%6] = append(p.Batches[i%6], u)
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "settlement_plans", p.Issue, p) }))
	must(t, s.prepareUsers(p, p.Batches[0], reader)) // previously prepared by old scheduler
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	must(t, s.processSettlementPlans(context.Background(), reader, prepareAt(p).Add(-time.Nanosecond), batchLogger))
	if preparedCount(t, s) != 2 {
		t.Fatal("legacy work changed before00.010")
	}
	must(t, s.processSettlementPlans(context.Background(), reader, prepareAt(p), batchLogger))
	if preparedCount(t, s) != 12 {
		t.Fatal("old groups not combined into one pass")
	}
	assertNoEarlyPayout(t, s)
	reader.published = true
	must(t, s.processSettlementPlans(context.Background(), reader, at, batchLogger))
	pending, e := s.PendingMarkets()
	must(t, e)
	if len(pending) != 0 {
		t.Fatal("legacy recovery incomplete")
	}
	midnight := time.Date(2027, 1, 1, 0, 0, 5, 0, model.Beijing)
	if got := prepareAt(settlementPlan{DrawAt: midnight}).Format(time.RFC3339Nano); got != "2027-01-01T00:00:00.01+08:00" {
		t.Fatal(got)
	}
}

func TestSinglePassSourceDelayedWithinWindow(t *testing.T) {
	s, _, at, reader := batchFixture(t, 2, 3)
	defer s.Close()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, e := s.settlementPlans()
	must(t, e)
	p := plans[0]
	saved := reader.rounds
	reader.rounds = map[string]games.Round{}
	for _, now := range []time.Time{prepareAt(p), at.Add(-2 * time.Second)} {
		must(t, s.processSettlementPlans(context.Background(), reader, now, batchLogger))
		if preparedCount(t, s) != 0 {
			t.Fatal("missing numbers must not be fabricated")
		}
		assertNoEarlyPayout(t, s)
	}
	reader.rounds = saved
	must(t, s.processSettlementPlans(context.Background(), reader, at.Add(-100*time.Millisecond), batchLogger))
	if preparedCount(t, s) != 2 {
		t.Fatal("04.9 second recovery did not calculate all users")
	}
	assertNoEarlyPayout(t, s)
	reader.published = true
	must(t, s.processSettlementPlans(context.Background(), reader, at.Add(2*time.Second), batchLogger))
	pending, e := s.PendingMarkets()
	must(t, e)
	if len(pending) != 0 {
		t.Fatal("late posting dropped orders")
	}
	must(t, s.processSettlementPlans(context.Background(), reader, at.Add(3*time.Second), batchLogger))
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, id := range []string{"u000", "u001"} {
			u, e := get[storedUser](tx, "users", id)
			if e != nil {
				return e
			}
			if u.Balance != 999600 {
				t.Fatal("duplicate or incorrect delayed payout", u.Balance)
			}
		}
		return nil
	}))
}
