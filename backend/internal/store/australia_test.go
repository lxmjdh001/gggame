package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestAustraliaRandomManualAndHistoryMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := games.Find("lhc-au")
	at := games.RegionalMarkSixStart().Add(15 * time.Minute)
	now := at.Add(-time.Minute)
	d := draftSource(at)
	if err = s.CacheSources([]model.Draw{d}); err != nil {
		t.Fatal(err)
	}
	previews, err := s.RandomPreviews(g, d.Issue, nil, 5, now)
	if err != nil || len(previews) != 5 {
		t.Fatal(err)
	}
	for _, p := range previews {
		seen := map[int]bool{}
		if len(p.Candidate.Round.Numbers) != 8 {
			t.Fatal("wrong random count")
		}
		for _, n := range p.Candidate.Round.Numbers {
			if n < 1 || n > 45 || seen[n] {
				t.Fatal("invalid random number")
			}
			seen[n] = true
		}
	}
	base := previews[0].Current
	if _, err = s.ManualPreview(g, d.Issue, []int{1, 2, 3, 4, 5, 6, 23, 46}, base.Numbers, now); err == nil {
		t.Fatal("manual accepted 46")
	}
	p, err := s.ManualPreview(g, d.Issue, []int{1, 2, 3, 4, 5, 6, 23, 45}, base.Numbers, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyPreview(g, d.Issue, p.Candidate.ID, "admin", now); err != nil {
		t.Fatal(err)
	}
	// Reconstruct a legacy database: simulated history is replaceable, real history is immutable.
	legacy := g
	legacy.ID = "legacy-projection"
	if at.Before(games.TwentyMinuteMarkSixStart()) {
		legacy.IntervalMinutes = 1440
		legacy.OffsetMinutes = 1290
	}
	legacy.Positions = legacy.Positions[:7]
	r, err := games.Project(legacy, d, "simulated_history")
	if err != nil {
		t.Fatal(err)
	}
	r.SessionID = g.ID
	if err = s.SaveSeedSource(d); err != nil {
		t.Fatal(err)
	}
	real := r
	real.Issue = g.Due(now.Add(-24 * time.Hour)).Format(model.IssueLayout)
	real.SourceIssue = real.Issue
	real.Origin = "scheduled"
	if err = s.SaveMany([]games.Round{r, real}); err != nil {
		t.Fatal(err)
	}
	if err = s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(metadata).Delete([]byte("au45-history-v2")) }); err != nil {
		t.Fatal(err)
	}
	// Exercise the historical 49-to-45 migration explicitly. Current Open
	// no longer runs it: the 45-to-49 upgrade preserves all existing history.
	if err = s.db.Update(migrateAustraliaHistory); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	unchanged, err := s.Get(g.ID, real.Issue)
	if err != nil || !reflect.DeepEqual(unchanged.Numbers, real.Numbers) {
		t.Fatal("published real history changed", err)
	}
	migrated, err := s.Get(g.ID, d.Issue)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.Numbers) != 8 || migrated.Origin != "simulated_history" {
		t.Fatal("history not migrated")
	}
	if err = s.db.View(func(tx *bolt.Tx) error {
		var archived games.Round
		if err := json.Unmarshal(tx.Bucket([]byte("au49_simulated_history_archive")).Get([]byte(d.Issue)), &archived); err != nil {
			return err
		}
		if !reflect.DeepEqual(archived.Numbers, r.Numbers) {
			t.Fatal("legacy history not archived")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
