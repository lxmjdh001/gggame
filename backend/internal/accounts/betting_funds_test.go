package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// No password hashing is needed to exercise the real debit, withdrawal and settlement paths.
func fundsFixture(t *testing.T) (*Store, string, time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, u := range []User{{ID: "member", Username: "member", Role: "member", Status: "active", Balance: 100000}, {ID: "admin", Username: "admin", Role: "admin", Status: "active"}} {
			if err := put(tx, "users", u.ID, storedUser{User: u}); err != nil {
				return err
			}
			if err := tx.Bucket([]byte("usernames")).Put([]byte(u.Username), []byte(u.ID)); err != nil {
				return err
			}
		}
		return nil
	}))
	return s, path, time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
}
func assertFunds(t *testing.T, s *Store, id string, available, betting, withdrawal int64) {
	t.Helper()
	a, err := s.Account(id)
	must(t, err)
	u := a["user"].(User)
	if u.Balance != available || a["betting_frozen_cents"].(int64) != betting || u.Frozen != withdrawal {
		t.Fatalf("funds: available=%d betting=%v withdrawal=%d, want %d/%d/%d", u.Balance, a["betting_frozen_cents"], u.Frozen, available, betting, withdrawal)
	}
}
func TestBettingFundsLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, market   string
		numbers        []int
		payout         int64
		manual, refund bool
	}{
		{"win", "p1.big", []int{8, 1, 2, 3, 4}, 39200, false, false},
		{"lose", "p1.big", []int{1, 1, 2, 3, 4}, 0, false, false},
		{"push", "dt.dragon", []int{4, 1, 2, 3, 4}, 20000, false, false},
		{"refund", "p1.big", nil, 20000, false, true},
		{"manual_win", "", nil, 45000, true, false},
		{"manual_lose", "", nil, 0, true, false},
		{"manual_refund", "", nil, 20000, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path, now := fundsFixture(t)
			defer func() { s.Close() }()
			in := marketInput(now, tc.market)
			stake := int64(20000)
			in.AmountCents = &stake
			in.Multiplier = 0
			legacy := input(now)
			legacy.AmountCents = &stake
			legacy.Multiplier = 0
			place := func() (Order, error) {
				if tc.manual {
					return s.PlaceOrder("member", legacy, now)
				}
				return s.PlaceMarketOrder("member", in, now)
			}
			o, err := place()
			must(t, err)
			same, err := place()
			must(t, err)
			if same.ID != o.ID {
				t.Fatal("retry duplicated stake")
			}
			assertFunds(t, s, "member", 80000, 20000, 0)
			// The held 200 cannot fund a new 900 bet or a 900 withdrawal.
			tooMuch := marketInput(now, "p1.big")
			amount := int64(90000)
			tooMuch.AmountCents = &amount
			tooMuch.Multiplier = 0
			if _, err := s.PlaceMarketOrder("member", tooMuch, now); err == nil {
				t.Fatal("bet used locked stake")
			}
			tooMuchLegacy := input(now)
			tooMuchLegacy.AmountCents = &amount
			tooMuchLegacy.Multiplier = 0
			if _, err := s.PlaceOrder("member", tooMuchLegacy, now); err == nil {
				t.Fatal("legacy bet used locked stake")
			}
			if _, err := s.CreateRequest("member", RequestInput{Kind: "withdraw", Amount: 90000, Note: "测试提现", Key: randomID()}); err == nil {
				t.Fatal("withdrawal used locked stake")
			}
			assertFunds(t, s, "member", 80000, 20000, 0)
			request := RequestInput{Kind: "withdraw", Amount: 30000, Note: "测试提现", Key: randomID()}
			w, err := s.CreateRequest("member", request)
			must(t, err)
			_, err = s.CreateRequest("member", request)
			must(t, err)
			assertFunds(t, s, "member", 50000, 20000, 30000)
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			assertFunds(t, s, "member", 50000, 20000, 30000)
			if tc.refund {
				must(t, s.SettleOrder("admin", o.ID, "refunded", "撤销退本", 0, false))
			} else if tc.manual {
				if err := s.SettleOrder("admin", o.ID, "settled", "等待开奖", tc.payout, false); err == nil {
					t.Fatal("manual settled before publication")
				}
				assertFunds(t, s, "member", 50000, 20000, 30000)
				must(t, s.SettleOrder("admin", o.ID, "settled", "开奖结算", tc.payout, true))
			} else {
				must(t, s.SettleMarket(o.ID, marketRound(in, tc.numbers)))
				must(t, s.SettleMarket(o.ID, marketRound(in, tc.numbers)))
			}
			assertFunds(t, s, "member", 50000+tc.payout, 0, 30000)
			if err := s.SettleOrder("admin", o.ID, "refunded", "重复撤销", 0, false); err == nil {
				t.Fatal("duplicate release accepted")
			}
			decision := "rejected"
			available := 80000 + tc.payout
			if tc.name == "win" {
				decision = "approved"
				available = 50000 + tc.payout
			}
			must(t, s.ReviewRequest("admin", w.ID, decision, "测试审核"))
			assertFunds(t, s, "member", available, 0, 0)
		})
	}
}

func TestBettingFundsConcurrentSpend(t *testing.T) {
	s, _, now := fundsFixture(t)
	defer s.Close()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var err error
			if i%2 == 0 {
				in := marketInput(now, "p1.big")
				amount := int64(20000)
				in.AmountCents = &amount
				in.Multiplier = 0
				_, err = s.PlaceMarketOrder("member", in, now)
			} else {
				_, err = s.CreateRequest("member", RequestInput{Kind: "withdraw", Amount: 20000, Note: "并发提现", Key: randomID()})
			}
			if err == nil {
				accepted.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	a, err := s.Account("member")
	must(t, err)
	u := a["user"].(User)
	if accepted.Load() != 5 || u.Balance != 0 || u.Frozen+a["betting_frozen_cents"].(int64) != 100000 {
		t.Fatalf("overspend or lost hold: %+v", a)
	}
}

func TestBettingFundsPreparationWaitsForPublication(t *testing.T) {
	s, path, at, reader := batchFixture(t, 1, 1)
	defer func() { s.Close() }()
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, err := s.settlementPlans()
	must(t, err)
	p := plans[0]
	must(t, s.processSettlementPlans(context.Background(), reader, prepareAt(p), batchLogger))
	if preparedCount(t, s) != 1 {
		t.Fatal("precalculation missing")
	}
	assertNoEarlyPayout(t, s)
	assertFunds(t, s, "u000", 990000, 10000, 0)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	assertFunds(t, s, "u000", 990000, 10000, 0)
	if err := s.commitUsers(p, planUsers(p), reader.rounds, at.Add(-time.Nanosecond)); err == nil {
		t.Fatal("precalculation released funds before draw")
	}
	assertFunds(t, s, "u000", 990000, 10000, 0)
	must(t, s.commitUsers(p, planUsers(p), reader.rounds, at))
	must(t, s.commitUsers(p, planUsers(p), reader.rounds, at))
	assertFunds(t, s, "u000", 1009600, 0, 0)
}

func TestBettingFundsIncludesAllPendingIssuesBeforePagination(t *testing.T) {
	s, _, now := fundsFixture(t)
	defer s.Close()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 205; i++ {
			status := "pending"
			if i == 203 {
				status = "settled"
			}
			if i == 204 {
				status = "refunded"
			}
			o := Order{ID: fmt.Sprintf("%04d", i), UserID: "member", Username: "member", SessionID: "ffc-1", Issue: "202001010000", Status: status, Cost: 100, CreatedAt: now}
			if err := put(tx, "orders", o.ID, o); err != nil {
				return err
			}
		}
		return put(tx, "orders", "other", Order{ID: "other", UserID: "admin", Status: "pending", Cost: 999})
	}))
	a, err := s.Account("member")
	must(t, err)
	if len(a["orders"].([]Order)) != 200 {
		t.Fatal("fixture must exceed display limit")
	}
	assertFunds(t, s, "member", 100000, 20300, 0)
	status, err := s.UserStatus("member", now, nil)
	must(t, err)
	if status.BettingFrozen != 20300 || status.Summary.Orders != 0 {
		t.Fatal("old stakes missing from hold or mixed into current issue")
	}
	overview, err := s.AdminData("admin", "overview", "", 1)
	must(t, err)
	if overview["betting_frozen_cents"].(int64) != 21299 {
		t.Fatal("admin overview frozen total mismatch")
	}
	users, err := s.AdminData("admin", "users", "member", 1)
	must(t, err)
	raw, err := json.Marshal(users)
	must(t, err)
	var result struct {
		Items []struct {
			BettingFrozen int64 `json:"betting_frozen_cents"`
		}
	}
	must(t, json.Unmarshal(raw, &result))
	if len(result.Items) != 1 || result.Items[0].BettingFrozen != 20300 {
		t.Fatal("admin user hold mismatch")
	}
	// No draw schedule or private preview is required for locked funds to be correct.
}
