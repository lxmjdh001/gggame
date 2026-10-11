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
				if !reflect.DeepEqual(previous.Groups, d.Groups) || !previous.GeneratedAt.Equal(d.GeneratedAt) {
					return fmt.Errorf("source changed for immutable issue %s", d.Issue)
				}
				continue
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
