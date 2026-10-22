// Verify the one-time withdrawal policy initialization while the service is
// stopped. Never prints credentials, member data or receiving addresses.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/accounts"
)

type snapshot struct {
	Digests   map[string][32]byte
	Receiving string
	Admin     string
}

func inspect(path string) (snapshot, error) {
	out := snapshot{Digests: map[string][32]byte{}}
	db, e := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if e != nil {
		return out, e
	}
	defer db.Close()
	e = db.View(func(tx *bolt.Tx) error {
		for _, name := range []string{"users", "ledger", "requests", "orders", "keys", "settings"} {
			b := tx.Bucket([]byte(name))
			if b == nil {
				return errors.New("required accounting bucket missing")
			}
			h := sha256.New()
			e := b.ForEach(func(k, v []byte) error {
				h.Write(k)
				h.Write([]byte{0})
				h.Write(v)
				h.Write([]byte{0})
				if name == "users" {
					var u accounts.User
					if err := json.Unmarshal(v, &u); err != nil {
						return err
					}
					if u.Role == "admin" && u.Status == "active" {
						out.Admin = u.ID
					}
				}
				return nil
			})
			if e != nil {
				return e
			}
			var sum [32]byte
			copy(sum[:], h.Sum(nil))
			out.Digests[name] = sum
		}
		b := tx.Bucket([]byte("payment_settings"))
		if b == nil {
			return errors.New("payment settings missing")
		}
		var p accounts.PaymentSettings
		if e := json.Unmarshal(b.Get([]byte("usdt")), &p); e != nil {
			return e
		}
		p.Version = 0
		p.RateYuan = ""
		data, e := json.Marshal(p)
		out.Receiving = string(data)
		return e
	})
	return out, e
}
func run(path string) error {
	before, e := inspect(path)
	if e != nil {
		return e
	}
	if before.Admin == "" {
		return errors.New("active administrator missing")
	}
	store, e := accounts.Open(path)
	if e != nil {
		return e
	}
	cfg, e := store.WithdrawalSettings(before.Admin, true)
	closeErr := store.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if cfg.FeeBPS != 1000 || cfg.RateYuan != "6.9" || !cfg.USDTEnabled {
		return errors.New("withdrawal policy differs from requested initialization")
	}
	after, e := inspect(path)
	if e != nil {
		return e
	}
	for name, digest := range before.Digests {
		if after.Digests[name] != digest {
			return fmt.Errorf("accounting records unexpectedly changed: %s", name)
		}
	}
	if after.Receiving != before.Receiving {
		return errors.New("USDT receiving network/address/limits changed")
	}
	fmt.Printf("policy_verified fee_percent=10 rate_yuan=6.9 usdt_withdrawal=true accounting_records_unchanged=true receiving_details_unchanged=true config_version=%d rate_version=%d\n", cfg.Version, cfg.RateVersion)
	return nil
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: withdraw-policy-check <accounts.db>")
		os.Exit(2)
	}
	if e := run(os.Args[1]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
