// Offline, explicit deployment of the approved eight-session odds profile.
// Run only with the backend stopped, first on a consistent backup copy.
// Never prints member information or credentials.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"reflect"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/accounts"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
)

type snapshot struct {
	Buckets     map[string][32]byte
	Quotes      map[string][]accounts.Quote
	QuoteHashes map[string][32]byte
	Actor       string
}

func hashPart(h hash.Hash, data []byte) {
	var size [8]byte
	binary.LittleEndian.PutUint64(size[:], uint64(len(data)))
	h.Write(size[:])
	h.Write(data)
}
func hashBucket(h hash.Hash, b *bolt.Bucket) error {
	var sequence [8]byte
	binary.LittleEndian.PutUint64(sequence[:], b.Sequence())
	h.Write(sequence[:])
	return b.ForEach(func(k, v []byte) error {
		hashPart(h, k)
		if v == nil {
			h.Write([]byte{0})
			return hashBucket(h, b.Bucket(k))
		}
		h.Write([]byte{1})
		hashPart(h, v)
		return nil
	})
}
func inspect(path string) (snapshot, error) {
	out := snapshot{Buckets: map[string][32]byte{}, Quotes: map[string][]accounts.Quote{}, QuoteHashes: map[string][32]byte{}}
	db, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if err != nil {
		return out, err
	}
	defer db.Close()
	err = db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			h := sha256.New()
			if err := hashBucket(h, b); err != nil {
				return err
			}
			var sum [32]byte
			copy(sum[:], h.Sum(nil))
			out.Buckets[string(name)] = sum
			if string(name) == "users" {
				return b.ForEach(func(k, v []byte) error {
					var u accounts.User
					if err := json.Unmarshal(v, &u); err != nil {
						return err
					}
					if u.Role == "admin" && u.Status == "active" && (out.Actor == "" || u.Username == "admin") {
						out.Actor = u.ID
					}
					return nil
				})
			}
			if string(name) == "market_quotes" {
				return b.ForEach(func(k, v []byte) error {
					var q []accounts.Quote
					if err := json.Unmarshal(v, &q); err != nil {
						return err
					}
					out.Quotes[string(k)] = q
					out.QuoteHashes[string(k)] = sha256.Sum256(v)
					return nil
				})
			}
			return nil
		})
	})
	return out, err
}

func verify(before, after snapshot) error {
	catalog := map[string]markets.Market{}
	for _, m := range markets.Catalog("lhc") {
		catalog[m.ID] = m
	}
	if len(before.Buckets) != len(after.Buckets) {
		return errors.New("unexpected bucket count change")
	}
	for name, digest := range before.Buckets {
		if name != "market_quotes" && name != "audit" && after.Buckets[name] != digest {
			return fmt.Errorf("unrelated accounting bucket changed: %s", name)
		}
	}
	if len(before.Quotes) != len(after.Quotes) {
		return errors.New("unexpected session quote count change")
	}
	for id, qs := range before.Quotes {
		g, ok := games.Find(id)
		if !ok || g.GameID != "lhc" {
			if before.QuoteHashes[id] != after.QuoteHashes[id] {
				return fmt.Errorf("non-LHC prices changed: %s", id)
			}
			continue
		}
		actual := map[string]accounts.Quote{}
		for _, q := range after.Quotes[id] {
			actual[q.MarketID] = q
		}
		for _, old := range qs {
			expected := old
			if m, active := catalog[old.MarketID]; active {
				p, err := accounts.MarkSixPresetForBook(accounts.MarketBook{Markets: []markets.Market{m}, Quotes: []accounts.Quote{old}, MarkSixContext: &markets.MarkSixContext{}})
				if err != nil {
					return err
				}
				expected = p.Quotes[0]
			}
			if !reflect.DeepEqual(expected, actual[old.MarketID]) {
				return fmt.Errorf("existing quote/switch differs from approved profile: %s/%s", id, old.MarketID)
			}
		}
	}
	return nil
}

func run(path string) error {
	before, err := inspect(path)
	if err != nil {
		return err
	}
	if before.Actor == "" {
		return errors.New("active administrator required")
	}
	s, err := accounts.Open(path)
	if err != nil {
		return err
	}
	results, applyErr := s.ApplyMarkSixOddsPreset(before.Actor)
	if applyErr == nil {
		for _, r := range results {
			b, e := s.MarketBook(r.SessionID)
			if e != nil {
				applyErr = e
				break
			}
			p, e := accounts.MarkSixPresetForBook(b)
			if e != nil {
				applyErr = e
				break
			}
			if !reflect.DeepEqual(b.Quotes, p.Quotes) {
				applyErr = errors.New("saved profile readback mismatch")
				break
			}
		}
	}
	closeErr := s.Close()
	if applyErr != nil {
		return applyErr
	}
	if closeErr != nil {
		return closeErr
	}
	after, err := inspect(path)
	if err != nil {
		return err
	}
	if err = verify(before, after); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"profile_id": accounts.MarkSixOddsProfileID, "sessions": results, "unrelated_accounting_unchanged": true, "other_game_prices_unchanged": true, "switches_preserved": true})
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "--apply" {
		fmt.Fprintln(os.Stderr, "usage: marksix-odds-release --apply <stopped-accounts.db>")
		os.Exit(2)
	}
	if err := run(os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
