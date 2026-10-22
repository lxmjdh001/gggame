package store

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

var regionalBackfillKey = []byte("regional_marksix_schedule_v1")

type regionalBackfill struct {
	Target string `json:"target"`
	After  string `json:"after"`
	Done   bool   `json:"done"`
}

// The target is frozen at the original publication cursor. New publication can
// continue while missing historical sources are retrieved independently.
func (s *Store) RegionalBackfillCursor() (string, error) {
	var after string
	err := s.db.View(func(tx *bolt.Tx) error {
		if raw := tx.Bucket(metadata).Get(regionalBackfillKey); raw != nil {
			var state regionalBackfill
			if err := json.Unmarshal(raw, &state); err != nil {
				return err
			}
			if !state.Done {
				after = state.After
			}
		}
		return nil
	})
	return after, err
}

// Recover only cached, complete source minutes, never manufacture live history.
// A bounded pass avoids holding the number database while a long outage catches
// up. The result writes and recovery cursor advance commit atomically, and the
// regular publication cursor is never rewound or otherwise changed here.
func (s *Store) BackfillRegionalMarkSix(now time.Time) error {
	var before, next regionalBackfill
	var initialized, changed bool
	items := []games.Round{}
	err := s.db.View(func(tx *bolt.Tx) error {
		if raw := tx.Bucket(metadata).Get(regionalBackfillKey); raw != nil {
			if err := json.Unmarshal(raw, &before); err != nil {
				return err
			}
			initialized = true
			if before.Done {
				return nil
			}
		} else {
			before = regionalBackfill{
				Target: string(tx.Bucket(metadata).Get([]byte("live_cursor"))),
				After:  games.RegionalMarkSixStart().Add(-time.Minute).Format(model.IssueLayout),
			}
			changed = true
		}
		next = before
		if next.Target <= next.After {
			next.Done, changed = true, true
			return nil
		}
		at, err := model.ParseIssue(next.After)
		if err != nil {
			return err
		}
		cache := tx.Bucket(sourceCache)
		if cache == nil {
			return nil
		}
		for count := 0; count < 240; count++ {
			at = at.Add(time.Minute)
			issue := at.Format(model.IssueLayout)
			if issue > next.Target || at.Add(games.SourcePublishDelay).After(now) {
				break
			}
			raw := cache.Get([]byte(issue))
			if raw == nil {
				break
			}
			var source model.Draw
			if err := json.Unmarshal(raw, &source); err != nil {
				return err
			}
			if err := model.Validate(source); err != nil {
				return err
			}
			if source.Issue != issue || !source.ScheduledAt.Equal(at) {
				return fmt.Errorf("regional recovery source mismatch for %s", issue)
			}
			rounds := []games.Round{}
			for _, g := range games.All() {
				if g.GameID != "lhc" || !g.MatchesSource(at) {
					continue
				}
				if _, ok := source.Groups[g.Group]; !ok {
					return nil // Retry after the existing source-schema upgrade.
				}
				r, err := games.Project(g, source, "scheduled")
				if err != nil {
					return err
				}
				r.PublishedAt = now.In(model.Beijing)
				rounds = append(rounds, r)
			}
			items = append(items, rounds...)
			next.After, changed = issue, true
			if issue == next.Target {
				next.Done = true
				break
			}
		}
		return nil
	})
	if err != nil || !changed {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		raw := tx.Bucket(metadata).Get(regionalBackfillKey)
		if initialized {
			var current regionalBackfill
			if err := json.Unmarshal(raw, &current); err != nil {
				return err
			}
			if current != before {
				return nil // Another recovery pass already committed progress.
			}
		} else if raw != nil {
			return nil
		}
		for _, r := range items {
			if err := putPublished(tx, r); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		return tx.Bucket(metadata).Put(regionalBackfillKey, encoded)
	})
}
