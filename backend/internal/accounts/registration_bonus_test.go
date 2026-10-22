package accounts

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestRegistrationBonusAtomicAndOnce(t *testing.T) {
	s, admin := setup(t)
	existing := register(t, s, "existing_member", "")
	cfg, e := s.Settings()
	must(t, e)
	cfg.RegistrationBonusCents = 1000000
	must(t, s.SaveSettings(admin.ID, cfg))
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, token, e := s.Register("bonus_member", testPassword, existing.InviteCode, "13800138000", "")
			if e == nil {
				successes.Add(1)
				if u.Balance != 1000000 || token == "" {
					t.Error("registration response missing bonus or login")
				}
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("duplicate registration succeeded")
	}
	u, _, e := s.Login("bonus_member", testPassword)
	must(t, e)
	if u.Balance != 1000000 || u.InviterID != existing.ID {
		t.Fatal("wrong bonus or invitation")
	}
	_, _, e = s.Login("bonus_member", testPassword)
	must(t, e)
	account, e := s.Account(u.ID)
	must(t, e)
	entries := account["ledger"].([]Ledger)
	if len(entries) != 1 || entries[0].Kind != "registration_bonus" || entries[0].Amount != 1000000 || account["user"].(User).Balance != 1000000 {
		t.Fatal("missing or duplicate bonus ledger", entries)
	}
	parent, e := s.Account(existing.ID)
	must(t, e)
	if parent["user"].(User).Balance != 0 || len(parent["commissions"].([]Commission)) != 0 {
		t.Fatal("existing account changed or bonus generated commission")
	}
	cfg.InviteRequired = true
	must(t, s.SaveSettings(admin.ID, cfg))
	if _, _, e = s.Register("failed_bonus", testPassword, "", "13800138000", ""); e == nil {
		t.Fatal("missing invitation accepted")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("usernames")).Get([]byte("failed_bonus")) != nil || tx.Bucket([]byte("ledger")).Stats().KeyN != 1 {
			t.Fatal("failed registration wrote account or bonus")
		}
		return nil
	}))
	cfg.InviteRequired = false
	cfg.RegistrationBonusCents = 0
	must(t, s.SaveSettings(admin.ID, cfg))
	disabled := register(t, s, "bonus_disabled", "")
	if disabled.Balance != 0 {
		t.Fatal("disabled bonus credited")
	}
	after, e := s.Account(u.ID)
	must(t, e)
	if after["user"].(User).Balance != 1000000 {
		t.Fatal("disabling bonus changed existing balance")
	}
	for _, invalid := range []int64{-1, 100000001} {
		cfg.RegistrationBonusCents = invalid
		if s.SaveSettings(admin.ID, cfg) == nil {
			t.Fatal("invalid bonus accepted")
		}
	}
}

func TestRegistrationBonusPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	_, e = s.Bootstrap("admin", testPassword)
	must(t, e)
	admin, _, e := s.Login("admin", testPassword)
	must(t, e)
	cfg, e := s.Settings()
	must(t, e)
	cfg.RegistrationBonusCents = 1000000
	must(t, s.SaveSettings(admin.ID, cfg))
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	u := register(t, s, "persist_bonus", "")
	if u.Balance != 1000000 {
		t.Fatal("bonus config not persisted")
	}
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	defer s.Close()
	u, _, e = s.Login(u.Username, testPassword)
	must(t, e)
	if u.Balance != 1000000 {
		t.Fatal("balance lost or repeated after restart")
	}
}
