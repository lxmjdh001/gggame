package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/accounts"
)

func TestOfflineReleaseMigratesAndPreservesAccounting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := accounts.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Bootstrap("releaseadmin", "ValidPassword123"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("market_quotes"))
		return b.ForEach(func(k, v []byte) error {
			if !strings.HasPrefix(string(k), "lhc-") {
				return nil
			}
			var qs []accounts.Quote
			if err := json.Unmarshal(v, &qs); err != nil {
				return err
			}
			old := make([]accounts.Quote, 0, len(qs))
			for _, q := range qs {
				if strings.HasPrefix(q.MarketID, "lhc6.position_special.") || strings.HasPrefix(q.MarketID, "lhc6.special.halfdigit.") || strings.HasPrefix(q.MarketID, "lhc6.miss_zodiac.") || q.MarketID == "lhc6.miss.12" {
					continue
				}
				if q.MarketID == "lhc6.regular.zodiac.z0" {
					q.OddsMilli = 7890
					q.Enabled = false
				}
				if q.MarketID == "lhc6.p1.big" {
					q.Enabled = false
				}
				old = append(old, q)
			}
			data, err := json.Marshal(old)
			if err != nil {
				return err
			}
			return b.Put(k, data)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = run(path); err != nil {
		t.Fatal(err)
	}
	after, err := inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = verify(before, after); err != nil {
		t.Fatal(err)
	}
	if len(after.Quotes["lhc-tw"]) != 718 {
		t.Fatal("new catalog not initialized")
	}
	if err = run(path); err != nil {
		t.Fatal(err)
	}
	again, err := inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, again) {
		t.Fatal("idempotent deployment changed stored state")
	}
}
