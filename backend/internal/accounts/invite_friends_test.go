package accounts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

func TestInviteFriendsTeamBalancesAndBeijingDailyStakes(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, model.Beijing)
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, model.Beijing)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, u := range []User{
			{ID: "friend1", Username: "Friend1", Status: "active", InviterID: "member", Balance: 1987700},
			{ID: "friend2", Username: "Friend2", Status: "active", InviterID: "friend1", Balance: 2304400},
			{ID: "friend3", Username: "Friend3", Status: "active", InviterID: "friend2", Balance: 1550400},
			{ID: "friend4", Username: "OutsideFourthLevel", Status: "active", InviterID: "friend3", Balance: 999999},
			{ID: "unrelated", Username: "Unrelated", Status: "active", Balance: 888888},
		} {
			u.CreatedAt = now
			u.Phone = "13900139000"
			u.Wechat = "private_wechat"
			u.Name = "私密姓名"
			if err := put(tx, "users", u.ID, storedUser{User: u, Hash: "private_hash", SecuritySalt: "private_salt"}); err != nil {
				return err
			}
		}
		orders := []Order{
			{ID: "start", UserID: "friend1", Status: "pending", Cost: 100, CreatedAt: start.UTC()},
			{ID: "settled", UserID: "friend1", Status: "settled", Cost: 200, CreatedAt: now},
			{ID: "last", UserID: "friend1", Status: "pending", Cost: 400, CreatedAt: start.AddDate(0, 0, 1).Add(-time.Nanosecond)},
			{ID: "yesterday", UserID: "friend1", Status: "pending", Cost: 800, CreatedAt: start.Add(-time.Nanosecond)},
			{ID: "tomorrow", UserID: "friend1", Status: "pending", Cost: 1600, CreatedAt: start.AddDate(0, 0, 1)},
			{ID: "refunded", UserID: "friend1", Status: "refunded", Cost: 3200, CreatedAt: now},
			{ID: "third", UserID: "friend3", Status: "settled", Cost: 23000, CreatedAt: now},
			{ID: "outside", UserID: "unrelated", Status: "pending", Cost: 999900, CreatedAt: now},
		}
		for i := 0; i < 205; i++ {
			orders = append(orders, Order{ID: fmt.Sprintf("many-%03d", i), UserID: "friend2", Status: "pending", Cost: 100, CreatedAt: now})
		}
		for _, o := range orders {
			if err := put(tx, "orders", o.ID, o); err != nil {
				return err
			}
		}
		return nil
	}))
	before := registrationCounts(t, s)
	result, err := s.accountAt("member", now)
	must(t, err)
	members := result["members"].([]Member)
	if len(members) != 3 || result["team_counts"] != [3]int{1, 1, 1} {
		t.Fatalf("team scope wrong: %+v", members)
	}
	byID := map[string]Member{}
	for _, m := range members {
		byID[m.ID] = m
	}
	for _, want := range []struct {
		id             string
		level          int
		balance, stake int64
	}{{"friend1", 1, 1987700, 700}, {"friend2", 2, 2304400, 20500}, {"friend3", 3, 1550400, 23000}} {
		got := byID[want.id]
		if got.Level != want.level || got.Balance != want.balance || got.TodayStake != want.stake {
			t.Fatalf("member %s mismatch: %+v", want.id, got)
		}
	}
	encoded, err := json.Marshal(members)
	must(t, err)
	for _, secret := range []string{"OutsideFourthLevel", "Unrelated", "13900139000", "private_wechat", "私密姓名", "private_hash", "private_salt"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("unrelated or private data exposed", secret)
		}
	}
	assertRegistrationUnchanged(t, s, before)
	nextDay, err := s.accountAt("member", start.AddDate(0, 0, 1))
	must(t, err)
	for _, m := range nextDay["members"].([]Member) {
		if m.ID == "friend1" && m.TodayStake != 1600 {
			t.Fatal("Beijing midnight did not reset daily stake", m.TodayStake)
		}
	}
	unrelated, err := s.accountAt("unrelated", now)
	must(t, err)
	if len(unrelated["members"].([]Member)) != 0 {
		t.Fatal("unrelated account received team records")
	}
}
