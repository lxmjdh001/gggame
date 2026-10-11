package accounts

import (
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readIssueStake(t *testing.T, s *Store, user, session, issue string) int64 {
	t.Helper()
	var used int64
	must(t, s.db.View(func(tx *bolt.Tx) error { var e error; used, e = issueStake(tx, user, session, issue); return e }))
	return used
}
func TestUserIssueLimitConcurrentRoutesAndRefund(t *testing.T) {
	s, a := setup(t)
	u := register(t, s, "limit_member", "")
	other := register(t, s, "limit_other", "")
	must(t, s.Adjust(a.ID, u.ID, 4000000, "测试资金", randomID()))
	must(t, s.Adjust(a.ID, other.ID, 100000, "测试资金", randomID()))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	amount := int64(100000)
	in := marketInput(now, "p1.big")
	in.AmountCents = &amount
	in.Multiplier = 0
	first, e := s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var e error
			if i%2 == 0 {
				next := in
				next.Key = randomID()
				_, e = s.PlaceMarketOrder(u.ID, next, now)
			} else {
				_, e = s.PlaceOrder(u.ID, OrderInput{SessionID: in.SessionID, Issue: in.Issue, Numbers: []int{1, 2, 3, 4, 5}, UnitCents: 200, AmountCents: &amount, Key: randomID()}, now)
			}
			if e == nil {
				success.Add(1)
			} else if !strings.Contains(e.Error(), "单用户单期投注上限") {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if success.Load() != 19 || readIssueStake(t, s, u.ID, in.SessionID, in.Issue) != 2000000 {
		t.Fatal("concurrent limit exceeded or prematurely blocked", success.Load())
	}
	account, e := s.Account(u.ID)
	must(t, e)
	if account["user"].(User).Balance != 2000000 {
		t.Fatal("failed orders debited balance")
	}
	same, e := s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	if same.ID != first.ID || readIssueStake(t, s, u.ID, in.SessionID, in.Issue) != 2000000 {
		t.Fatal("retry consumed capacity")
	}
	independent := in
	independent.Key = randomID()
	_, e = s.PlaceMarketOrder(other.ID, independent, now)
	must(t, e)
	nextSession := in
	g, _ := games.Find("ffc-3")
	nextSession.SessionID = g.ID
	nextSession.Issue = g.Next(now).Format(model.IssueLayout)
	nextSession.Key = randomID()
	_, e = s.PlaceMarketOrder(u.ID, nextSession, now)
	must(t, e)
	nextIssue := in
	nextIssue.Issue = marketInput(now.Add(time.Minute), "p1.big").Issue
	nextIssue.Key = randomID()
	_, e = s.PlaceMarketOrder(u.ID, nextIssue, now.Add(time.Minute))
	must(t, e)
	must(t, s.SettleOrder(a.ID, first.ID, "refunded", "测试撤销", 0, false))
	if s.SettleOrder(a.ID, first.ID, "refunded", "重复撤销", 0, false) == nil {
		t.Fatal("duplicate refund accepted")
	}
	if readIssueStake(t, s, u.ID, in.SessionID, in.Issue) != 1900000 {
		t.Fatal("refund did not release exactly once")
	}
	replacement := in
	replacement.Key = randomID()
	_, e = s.PlaceMarketOrder(u.ID, replacement, now)
	must(t, e)
	cfg, e := s.Settings()
	must(t, e)
	for i := range cfg.Plays {
		cfg.Plays[i].UserIssueLimitCents = 1000000
	}
	must(t, s.SaveSettings(a.ID, cfg))
	excess := in
	excess.Key = randomID()
	if _, e = s.PlaceMarketOrder(u.ID, excess, now); e == nil {
		t.Fatal("reduced cap bypassed")
	}
	if readIssueStake(t, s, u.ID, in.SessionID, in.Issue) != 2000000 {
		t.Fatal("changing limit altered accepted stake")
	}
	broke := register(t, s, "no_funds", "")
	insufficient := in
	insufficient.Key = randomID()
	if _, e = s.PlaceMarketOrder(broke.ID, insufficient, now); e == nil {
		t.Fatal("insufficient funds accepted")
	}
	if readIssueStake(t, s, broke.ID, in.SessionID, in.Issue) != 0 {
		t.Fatal("failed debit consumed quota")
	}
	for _, v := range []int64{0, 199, 100000001} {
		cfg.Plays[0].UserIssueLimitCents = v
		if s.SaveSettings(a.ID, cfg) == nil {
			t.Fatal("invalid limit accepted")
		}
	}
}
func TestIssueStakeMigrationAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	cfg, e := s.Settings()
	must(t, e)
	cfg.RegistrationBonusCents = 1000000
	cfg.InviteRequired = true
	for i := range cfg.Plays {
		cfg.Plays[i].UserIssueLimitCents = 0
	}
	cfg.Plays[0].Enabled = false
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if e := tx.DeleteBucket([]byte(issueStakeBucket)); e != nil {
			return e
		}
		for _, o := range []Order{{ID: "a", UserID: "user", SessionID: "ffc-1", Issue: "202610112030", Status: "pending", Cost: 10000}, {ID: "b", UserID: "user", SessionID: "ffc-1", Issue: "202610112030", Status: "settled", Cost: 5000}, {ID: "c", UserID: "user", SessionID: "ffc-1", Issue: "202610112030", Status: "refunded", Cost: 3000}, {ID: "d", UserID: "other", SessionID: "ffc-1", Issue: "202610112030", Status: "pending", Cost: 8000}} {
			if e := put(tx, "orders", o.ID, o); e != nil {
				return e
			}
		}
		return put(tx, "settings", "global", cfg)
	}))
	must(t, s.Close())
	for i := 0; i < 2; i++ {
		s, e = Open(path)
		must(t, e)
		if readIssueStake(t, s, "user", "ffc-1", "202610112030") != 15000 || readIssueStake(t, s, "other", "ffc-1", "202610112030") != 8000 {
			t.Fatal("migration count wrong")
		}
		got, e := s.Settings()
		must(t, e)
		if got.RegistrationBonusCents != 1000000 || !got.InviteRequired || got.Plays[0].Enabled {
			t.Fatal("migration changed unrelated settings")
		}
		for _, p := range got.Plays {
			if p.UserIssueLimitCents != 2000000 {
				t.Fatal("wrong default cap")
			}
		}
		must(t, s.Close())
	}
}
