package accounts

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestStalePayoutRecoveryAfterRestartIsAtomicAndIdempotent(t *testing.T) {
	s, path, at, reader := batchFixture(t, 3, 3)
	defer func() { s.Close() }()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, err := s.settlementPlans()
	must(t, err)
	p := plans[0]
	users := planUsers(p)
	must(t, s.prepareUsers(p, users, reader))
	// Already completed users must never be recalculated, even when other users
	// have an old preparation to recover. Refunds must likewise remain untouched.
	must(t, s.commitUsers(p, users[:1], reader.rounds, at))
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "users", "admin-test", storedUser{User: User{ID: "admin-test", Role: "admin", Status: "active"}})
	}))
	refunded := ""
	for _, id := range users[2].Orders {
		o, e := s.Order(id)
		must(t, e)
		if o.SessionID == "ffc-1" {
			refunded = id
		}
	}
	if refunded == "" {
		t.Fatal("missing refund order")
	}
	must(t, s.SettleOrder("admin-test", refunded, "refunded", "预计算后退款", 0, false))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	// Private numbers alone must not trigger recovery or posting, even after :05.
	for id, r := range reader.rounds {
		r.Numbers = []int{1, 1, 1, 1, 1}
		if id == "ffc-5" {
			r.Numbers = []int{9, 1, 2, 3, 1}
		}
		reader.rounds[id] = r
	}
	must(t, s.processSettlementPlans(context.Background(), reader, at, batchLogger))
	pending, err := s.PendingMarkets()
	must(t, err)
	if len(pending) != 5 {
		t.Fatalf("private result caused early recovery: %d pending", len(pending))
	}
	// Publication unlocks recovery. Concurrent scheduler retries must still credit once.
	reader.published = true
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
	must(t, s.processSettlementPlans(context.Background(), reader, at.Add(time.Second), batchLogger))
	pending, err = s.PendingMarkets()
	must(t, err)
	if len(pending) != 0 {
		t.Fatal("stale preparations stayed pending", len(pending))
	}
	plans, err = s.settlementPlans()
	must(t, err)
	if len(plans) != 0 || preparedCount(t, s) != 0 {
		t.Fatal("recovered plan was not cleaned up")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for id, want := range map[string]int64{"u000": 999600, "u001": 1009200, "u002": 1019200} {
			user, e := get[storedUser](tx, "users", id)
			if e != nil {
				return e
			}
			if user.Balance != want {
				t.Fatalf("%s balance=%d want=%d", id, user.Balance, want)
			}
		}
		orders, e := list[Order](tx, "orders")
		if e != nil {
			return e
		}
		for _, o := range orders {
			if o.ID == refunded {
				if o.Status != "refunded" {
					t.Fatal("refund overwritten")
				}
				continue
			}
			if o.Status != "settled" {
				t.Fatal("order remained pending")
			}
			wantNumbers := reader.rounds[o.SessionID].Numbers
			if o.UserID == "u000" {
				wantNumbers = []int{8, 8, 8, 8, 8}
			}
			if !reflect.DeepEqual(o.ResultNumbers, wantNumbers) {
				t.Fatalf("order %s used wrong result: %v", o.ID, o.ResultNumbers)
			}
			if o.UserID == "u001" {
				want := int64(19600)
				if o.SessionID == "ffc-1" {
					want = 0
				}
				if o.Payout != want {
					t.Fatalf("stale payout used: %d want=%d", o.Payout, want)
				}
			}
		}
		commissions, e := list[Commission](tx, "commissions")
		if e != nil {
			return e
		}
		var total int64
		for _, c := range commissions {
			total += c.Amount
		}
		if len(commissions) != 21 || total != 1225 {
			t.Fatalf("duplicate/missing commissions: count=%d total=%d", len(commissions), total)
		}
		ledgers, e := list[Ledger](tx, "ledger")
		if e != nil {
			return e
		}
		if len(ledgers) != 16 {
			t.Fatalf("duplicate/missing wallet entries: %d", len(ledgers))
		}
		return nil
	}))
}
