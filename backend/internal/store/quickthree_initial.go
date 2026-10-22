package store

import (
	"encoding/json"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

var newQuickThreeIDs = []string{"k3-zj", "k3-fj", "k3-gd", "k3-hn", "k3-tw", "k3-gx"}

type quickThreeInitial struct {
	Issue string `json:"issue"`
	Done  bool   `json:"done"`
}

func quickThreeInitialKey(id string) []byte { return []byte("k3_twenty_initial_v1:" + id) }

// Reserve the latest due issue before generating older simulated history. The
// saved target survives a failed seed or restart, even when the clock advances.
// Existing three K3 sessions and all other games retain their previous seeding.
func (s *Store) QuickThreeSeedTarget(g games.Session, now time.Time) (time.Time, error) {
	eligible := false
	for _, id := range newQuickThreeIDs {
		eligible = eligible || id == g.ID
	}
	if !eligible {
		return time.Time{}, nil
	}
	var state quickThreeInitial
	err := s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(metadata)
		key := quickThreeInitialKey(g.ID)
		if raw := meta.Get(key); raw != nil {
			return json.Unmarshal(raw, &state)
		}
		state.Issue = g.Due(now).Format(model.IssueLayout)
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return meta.Put(key, raw)
	})
	if err != nil {
		return time.Time{}, err
	}
	at, err := model.ParseIssue(state.Issue)
	return at.Add(games.SourcePublishDelay), err
}

// A cached later minute or a publication cursor beyond the target must not
// prevent retrieving its original source batch.
func (s *Store) QuickThreeBackfillCursor() (string, error) {
	var earliest string
	err := s.db.View(func(tx *bolt.Tx) error {
		for _, id := range newQuickThreeIDs {
			raw := tx.Bucket(metadata).Get(quickThreeInitialKey(id))
			if raw == nil {
				continue
			}
			var state quickThreeInitial
			if err := json.Unmarshal(raw, &state); err != nil {
				return err
			}
			if !state.Done && (earliest == "" || state.Issue < earliest) {
				earliest = state.Issue
			}
		}
		return nil
	})
	if err != nil || earliest == "" {
		return "", err
	}
	at, err := model.ParseIssue(earliest)
	return at.Add(-time.Minute).Format(model.IssueLayout), err
}

// Complete initialization only from the existing immutable source cache, using
// the same adopted-number precedence as normal publication. No global cursor
// changes, and absent sources leave a persistent retry target.
func (s *Store) BackfillQuickThree(now time.Time) error {
	states := map[string]quickThreeInitial{}
	items := []games.Round{}
	err := s.db.View(func(tx *bolt.Tx) error {
		for _, id := range newQuickThreeIDs {
			raw := tx.Bucket(metadata).Get(quickThreeInitialKey(id))
			if raw == nil {
				continue
			}
			var state quickThreeInitial
			if err := json.Unmarshal(raw, &state); err != nil {
				return err
			}
			if state.Done {
				continue
			}
			at, err := model.ParseIssue(state.Issue)
			if err != nil {
				return err
			}
			if at.Add(games.SourcePublishDelay).After(now) {
				continue
			}
			if b := tx.Bucket(rounds).Bucket([]byte(sessionKey(id))); b != nil && b.Get([]byte(state.Issue)) != nil {
				states[id] = state
				continue
			}
			cache := tx.Bucket(sourceCache)
			if cache == nil || cache.Get([]byte(state.Issue)) == nil {
				continue
			}
			var source model.Draw
			if err := json.Unmarshal(cache.Get([]byte(state.Issue)), &source); err != nil {
				return err
			}
			if err := model.Validate(source); err != nil {
				return err
			}
			g, ok := games.Find(id)
			if !ok {
				continue
			}
			if _, ok := source.Groups[g.Group]; !ok {
				continue
			}
			r, err := games.Project(g, source, "scheduled")
			if err != nil {
				return err
			}
			r.PublishedAt = now.In(model.Beijing)
			items = append(items, r)
			states[id] = state
		}
		return nil
	})
	if err != nil || len(states) == 0 {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, r := range items {
			if err := putPublished(tx, r); err != nil {
				return err
			}
		}
		for id, state := range states {
			state.Done = true
			raw, err := json.Marshal(state)
			if err != nil {
				return err
			}
			if err := tx.Bucket(metadata).Put(quickThreeInitialKey(id), raw); err != nil {
				return err
			}
		}
		return nil
	})
}
