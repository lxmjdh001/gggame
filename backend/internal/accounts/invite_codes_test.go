package accounts

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestSevenDigitInviteMigrationPreservesUsersAndOldLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	must(t, err)
	_, err = s.Bootstrap("admin", testPassword)
	must(t, err)
	a, _, err := s.Login("admin", testPassword)
	must(t, err)
	child := register(t, s, "existing_child", a.InviteCode)
	before := map[string]storedUser{}
	oldCodes := map[string]string{a.ID: "ABCDEF123456", child.ID: "123456ABCDEF"}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for id, code := range oldCodes {
			u, e := get[storedUser](tx, "users", id)
			if e != nil {
				return e
			}
			if e = tx.Bucket([]byte("invites")).Delete([]byte(u.InviteCode)); e != nil {
				return e
			}
			u.InviteCode = code
			u.Balance = 12345
			u.Frozen = 678
			before[id] = u
			if e = put(tx, "users", id, u); e != nil {
				return e
			}
			if e = tx.Bucket([]byte("invites")).Put([]byte(code), []byte(id)); e != nil {
				return e
			}
		}
		return tx.Bucket([]byte("settings")).Delete(shortInviteMigration)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	codes := map[string]string{}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for id, want := range before {
			got, e := get[storedUser](tx, "users", id)
			if e != nil {
				return e
			}
			if !shortInvitePattern.MatchString(got.InviteCode) {
				t.Fatal("not seven digits")
			}
			codes[id] = got.InviteCode
			for _, code := range []string{oldCodes[id], got.InviteCode} {
				if string(tx.Bucket([]byte("invites")).Get([]byte(code))) != id {
					t.Fatal("invite ownership lost")
				}
			}
			got.InviteCode = want.InviteCode
			if !reflect.DeepEqual(got, want) {
				t.Fatal("migration changed account or relationship")
			}
		}
		return nil
	}))
	if codes[a.ID] == codes[child.ID] {
		t.Fatal("duplicate invite codes")
	}
	for i, code := range []string{codes[a.ID], " " + strings.ToLower(oldCodes[a.ID]) + " "} {
		u := register(t, s, fmt.Sprintf("via_code_%d", i), code)
		if u.InviterID != a.ID || !shortInvitePattern.MatchString(u.InviteCode) {
			t.Fatal("old/new code registration mismatch")
		}
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer s.Close()
	u, _, err := s.Login("admin", testPassword)
	must(t, err)
	if u.InviteCode != codes[a.ID] {
		t.Fatal("restart rotated invite")
	}
}

func TestSevenDigitInviteCollisionAndUniqueness(t *testing.T) {
	s, _ := setup(t)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, code := range []string{"9999999", "1000000"} {
			if e := tx.Bucket([]byte("invites")).Put([]byte(code), []byte("reserved")); e != nil {
				return e
			}
		}
		code, e := availableInviteCode(tx, inviteCodeSpace-1)
		if e != nil {
			return e
		}
		if code != "1000001" {
			t.Fatal("collision wraparound failed", code)
		}
		for i := 0; i < 200; i++ {
			u, e := createUser(tx, fmt.Sprintf("numeric_%03d", i), "hash", "salt", "", "user", "13800138000", "")
			if e != nil {
				return e
			}
			if !shortInvitePattern.MatchString(u.InviteCode) || string(tx.Bucket([]byte("invites")).Get([]byte(u.InviteCode))) != u.ID {
				t.Fatal("invalid or colliding invite")
			}
		}
		return nil
	}))
}
