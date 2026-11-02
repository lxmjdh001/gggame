package accounts

import (
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

func TestOverviewVisibleAccountsAndNewMembersBeijingDay(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	start := time.Date(2026, 11, 3, 0, 0, 0, 0, model.Beijing)
	end := start.AddDate(0, 0, 1)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, u := range []User{
			{ID: "at-start", Role: "user", Status: "active", CreatedAt: start.UTC()},
			{ID: "at-end-inside", Role: "user", Status: "disabled", CreatedAt: end.Add(-time.Nanosecond).UTC()},
			{ID: "before", Role: "user", Status: "active", CreatedAt: start.Add(-time.Nanosecond)},
			{ID: "next-day", Role: "user", Status: "active", CreatedAt: end},
			{ID: "deleted-today", Role: "user", Status: "disabled", Deleted: true, CreatedAt: start, Balance: 321, Frozen: 44},
			{ID: "today-admin", Role: "admin", Status: "active", CreatedAt: start},
			{ID: "no-date", Role: "user", Status: "active"},
		} {
			u.Username = u.ID
			if err := put(tx, "users", u.ID, storedUser{User: u}); err != nil {
				return err
			}
		}
		if err := put(tx, "orders", "archived-order", Order{ID: "archived-order", UserID: "deleted-today", Status: "settled", Cost: 100}); err != nil {
			return err
		}
		return put(tx, "commissions", "archived-commission", Commission{ID: "archived-commission", UserID: "deleted-today", Amount: 23})
	}))
	before := markSixCombinedStoredState(t, s)
	for _, now := range []time.Time{start.UTC(), start.Add(12 * time.Hour).UTC(), end.Add(-time.Nanosecond).UTC()} {
		got, err := s.adminDataAt("admin", "overview", "", 1, now)
		must(t, err)
		if got["users"] != 8 || got["today_new_members"] != 2 {
			t.Fatal(now, got)
		}
		if got["balance_cents"] != int64(100321) || got["frozen_cents"] != int64(44) || got["valid_stake_cents"] != int64(100) || got["commission_cents"] != int64(23) || got["orders"] != 1 {
			t.Fatal("historical finance changed", got)
		}
	}
	next, err := s.adminDataAt("admin", "overview", "", 1, end.UTC())
	must(t, err)
	if next["today_new_members"] != 1 {
		t.Fatal("day did not roll over at Beijing midnight", next)
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("overview read mutated data")
	}
	// Deletion and restoration affect counts, never the member's registration date.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "at-start")
		if err != nil {
			return err
		}
		u.Deleted = true
		u.Status = "disabled"
		return put(tx, "users", u.ID, u)
	}))
	deleted, err := s.adminDataAt("admin", "overview", "", 1, start)
	must(t, err)
	if deleted["users"] != 7 || deleted["today_new_members"] != 1 {
		t.Fatal(deleted)
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range []string{"at-start", "deleted-today"} {
			u, err := get[storedUser](tx, "users", id)
			if err != nil {
				return err
			}
			u.Deleted = false
			if err = put(tx, "users", u.ID, u); err != nil {
				return err
			}
		}
		return nil
	}))
	restored, err := s.adminDataAt("admin", "overview", "", 1, start)
	must(t, err)
	if restored["users"] != 9 || restored["today_new_members"] != 3 {
		t.Fatal(restored)
	}
	restoredNextDay, err := s.adminDataAt("admin", "overview", "", 1, end)
	must(t, err)
	if restoredNextDay["today_new_members"] != 1 {
		t.Fatal("restored members treated as new registrations", restoredNextDay)
	}
	if _, err = s.adminDataAt("member", "overview", "", 1, start); err == nil {
		t.Fatal("non-admin could view overview")
	}
}
