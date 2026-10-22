package store

import (
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"time"
)

var sourceCache = []byte("private_source_cache_v1")

// Refresh only a cached day whose source has gained the schema5 groups.
// This does not rewind the publication cursor or replace any published results.
func (s *Store) SourceNeedsUpgrade(latest model.Draw) (bool, error) {
	upgrade := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(sourceCache)
		if b == nil {
			return nil
		}
		if raw := b.Get([]byte(latest.Issue)); raw != nil {
			var previous model.Draw
			if err := json.Unmarshal(raw, &previous); err != nil {
				return err
			}
			upgrade = previous.SchemaVersion == 4 && latest.SchemaVersion == 5
		}
		return nil
	})
	return upgrade, err
}

func sourceExtension(previous, next model.Draw) bool {
	if previous.SchemaVersion != 4 || next.SchemaVersion != 5 || len(previous.Groups) != 8 || len(next.Groups) != 11 {
		return false
	}
	if !previous.GeneratedAt.Equal(next.GeneratedAt) || !previous.ScheduledAt.Equal(next.ScheduledAt) || previous.Timezone != next.Timezone {
		return false
	}
	for group, numbers := range previous.Groups {
		if !reflect.DeepEqual(numbers, next.Groups[group]) || !previous.GroupGeneratedAt[group].Equal(next.GroupGeneratedAt[group]) {
			return false
		}
	}
	for _, group := range []string{"g9", "g10", "g11"} {
		if next.GroupGeneratedAt[group].IsZero() {
			return false
		}
	}
	return true
}

func (s *Store) SourceCursor() (string, error) {
	var cursor string
	err := s.db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(sourceCache); b != nil {
			k, _ := b.Cursor().Last()
			cursor = string(k)
		}
		return nil
	})
	return cursor, err
}

// Prepared numbers are deliberately kept outside the public round buckets.
func (s *Store) CacheSources(items []model.Draw) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(sourceCache)
		if err != nil {
			return err
		}
		for _, d := range items {
			if err = model.Validate(d); err != nil {
				return err
			}
			if raw := b.Get([]byte(d.Issue)); raw != nil {
				var previous model.Draw
				if err = json.Unmarshal(raw, &previous); err != nil {
					return err
				}
				if sourceExtension(previous, d) {
					// Append-only schema upgrade; existing groups must remain byte-for-byte equal.
				} else if !reflect.DeepEqual(previous.Groups, d.Groups) || !previous.GeneratedAt.Equal(d.GeneratedAt) || !previous.ScheduledAt.Equal(d.ScheduledAt) || !reflect.DeepEqual(previous.GroupGeneratedAt, d.GroupGeneratedAt) {
					return fmt.Errorf("source changed for immutable issue %s", d.Issue)
				} else {
					continue
				}
			}
			raw, err := json.Marshal(d)
			if err != nil {
				return err
			}
			if err = b.Put([]byte(d.Issue), raw); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) DueSources(after string, publishBefore time.Time) ([]model.Draw, error) {
	items := []model.Draw{}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(sourceCache)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		k, v := c.Seek([]byte(after))
		if string(k) == after {
			k, v = c.Next()
		}
		for ; k != nil; k, v = c.Next() {
			var d model.Draw
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			if d.ScheduledAt.After(publishBefore) {
				break
			}
			items = append(items, d)
		}
		return nil
	})
	return items, err
}

// PreviewRound reads a prepared result for administrators without publishing it.
// A missing source (including tomorrow before midnight) is not generated on demand.
func (s *Store) PreviewRound(g games.Session, at time.Time) (*games.Round, error) {
	s.drawMu.RLock()
	defer s.drawMu.RUnlock()
	var round *games.Round
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		round, _, err = previewRound(tx, g, at)
		return err
	})
	return round, err
}
