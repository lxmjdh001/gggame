package main

import (
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"starlight/backend/internal/accounts"
	"testing"
)

func TestPolicyCheckPreservesAccountingAndReceiving(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := accounts.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Bootstrap("checkeradmin", "ValidPassword123"); e != nil {
		t.Fatal(e)
	}
	u, _, e := s.Login("checkeradmin", "ValidPassword123")
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := s.PaymentSettings(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Enabled = true
	cfg.Network = "TRC20"
	cfg.Address = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	cfg.RateYuan = "7"
	cfg.MinAmountCents = 100
	cfg.MaxAmountCents = 100000000
	if _, e = s.SavePaymentSettings(u.ID, cfg); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	db, e := bolt.Open(path, 0600, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte("withdrawal_settings")).Delete([]byte("current")) }); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if e = run(path); e != nil {
		t.Fatal(e)
	}
	if e = run(path); e != nil {
		t.Fatal(e)
	}
}
