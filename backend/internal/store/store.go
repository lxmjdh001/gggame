package store

import (
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var rounds = []byte("rounds_v1")
var metadata = []byte("metadata_v1")
var seedSources = []byte("seed_sources_v1")

type Store struct {
	db *bolt.DB
	// Keep payout readers behind the entire adoption commit, including fsync.
	drawMu sync.RWMutex
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, key := range [][]byte{rounds, metadata, seedSources} {
			if _, err := tx.CreateBucketIfNotExists(key); err != nil {
				return err
			}
		}
		// The old 45-number simulated-history rewrite is retired. Existing
		// published and simulated rounds are immutable across this upgrade.
		return migrateSydney49(tx)
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func sessionKey(id string) string {
	if g, ok := games.Find(id); ok && g.GameID == "k3" {
		return id + ":g8"
	}
	return id
}
func (s *Store) Close() error { return s.db.Close() }
func put(tx *bolt.Tx, r games.Round) error {
	b, err := tx.Bucket(rounds).CreateBucketIfNotExists([]byte(sessionKey(r.SessionID)))
	if err != nil {
		return err
	}
	if b.Get([]byte(r.Issue)) != nil {
		return nil
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return b.Put([]byte(r.Issue), raw)
}
func (s *Store) SaveMany(items []games.Round) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, r := range items {
			if err := put(tx, r); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) Seeded(id string) (bool, error) {
	done := false
	err := s.db.View(func(tx *bolt.Tx) error {
		done = tx.Bucket(metadata).Get([]byte("seed:"+sessionKey(id))) != nil
		return nil
	})
	return done, err
}

// Sources are shared across all seeded sessions; equal source issues always use equal numbers.
func (s *Store) SeedSource(issue string) (model.Draw, error) {
	var d model.Draw
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(seedSources).Get([]byte(issue))
		if raw == nil {
			return ErrNotFound
		}
		return json.Unmarshal(raw, &d)
	})
	return d, err
}
func (s *Store) SaveSeedSource(d model.Draw) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(seedSources)
		if b.Get([]byte(d.Issue)) != nil {
			return nil
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		return b.Put([]byte(d.Issue), raw)
	})
}
func (s *Store) CommitSeed(id string, items []games.Round) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(metadata).Get([]byte("seed:"+sessionKey(id))) != nil {
			return nil
		}
		for _, r := range items {
			if err := put(tx, r); err != nil {
				return err
			}
		}
		return tx.Bucket(metadata).Put([]byte("seed:"+sessionKey(id)), []byte("100"))
	})
}
func (s *Store) Get(id, issue string) (games.Round, error) {
	var r games.Round
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rounds).Bucket([]byte(sessionKey(id)))
		if b == nil {
			return ErrNotFound
		}
		raw := b.Get([]byte(issue))
		if raw == nil {
			return ErrNotFound
		}
		return json.Unmarshal(raw, &r)
	})
	return r, err
}
func (s *Store) History(id string, limit int, before string) (games.History, error) {
	result := games.History{Items: []games.Round{}}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rounds).Bucket([]byte(sessionKey(id)))
		if b == nil {
			return nil
		}
		result.Total = b.Stats().KeyN
		c := b.Cursor()
		k, v := c.Last()
		if before != "" {
			k, v = c.Seek([]byte(before))
			if k == nil {
				k, v = c.Last()
			} else {
				k, v = c.Prev()
			}
		}
		for k != nil && len(result.Items) < limit {
			var r games.Round
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			result.Items = append(result.Items, r)
			k, v = c.Prev()
		}
		if k != nil && len(result.Items) > 0 {
			result.NextBefore = result.Items[len(result.Items)-1].Issue
		}
		return nil
	})
	return result, err
}
func (s *Store) LiveCursor() (string, error) {
	var cursor string
	err := s.db.View(func(tx *bolt.Tx) error { cursor = string(tx.Bucket(metadata).Get([]byte("live_cursor"))); return nil })
	return cursor, err
}
func (s *Store) CommitLive(items []games.Round, issue string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, r := range items {
			if err := putPublished(tx, r); err != nil {
				return err
			}
		}
		return tx.Bucket(metadata).Put([]byte("live_cursor"), []byte(issue))
	})
}

// Live publication and historical recovery must honor the same accepted result.
func putPublished(tx *bolt.Tx, r games.Round) error {
	replacement, err := override(tx, r.SessionID, r.Issue)
	if err != nil {
		return err
	}
	if replacement != nil {
		if r.SessionID == "lhc-au" {
			g, _ := games.Find(r.SessionID)
			if _, err := setNumbers(g, replacement.Round, replacement.Round.Numbers); err != nil {
				return errors.New("悉尼号码格式与该期开奖规则不符")
			}
		}
		publishedAt := r.PublishedAt
		r = replacement.Round
		r.PublishedAt = publishedAt
	}
	return put(tx, r)
}

// Extend simulated source records with new groups while preserving every existing group.
func (s *Store) ExtendSeedSource(d model.Draw) (model.Draw, error) {
	var out model.Draw
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(seedSources)
		raw := b.Get([]byte(d.Issue))
		if raw == nil {
			out = d
		} else {
			if e := json.Unmarshal(raw, &out); e != nil {
				return e
			}
			for id, n := range d.Groups {
				if _, exists := out.Groups[id]; !exists {
					out.Groups[id] = n
				}
			}
			if d.SchemaVersion > out.SchemaVersion {
				out.SchemaVersion = d.SchemaVersion
			}
			model.Display(&out)
		}
		if e := model.Validate(out); e != nil {
			return e
		}
		encoded, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return b.Put([]byte(d.Issue), encoded)
	})
	return out, err
}
