package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
)

func TestSeedAfterRegionalChangeNeverSimulatesLivePeriods(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := games.RegionalMarkSixStart()
	now := start.Add(3 * time.Minute)
	e := Engine{Store: db}
	if err = e.Seed(now); err != nil {
		t.Fatal(err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			continue
		}
		h, err := db.History(g.ID, 100, "")
		if err != nil || h.Total != 100 {
			t.Fatal(g.ID, "seed failed", err)
		}
		for _, r := range h.Items {
			if !r.DrawAt.Before(start) || r.Origin != "simulated_history" {
				t.Fatal(g.ID, "live issue occupied by seed")
			}
		}
	}
	for i := 0; i < 3; i++ {
		d, err := synthetic(start.Add(time.Duration(i)*time.Minute - games.SourcePublishDelay))
		if err != nil {
			t.Fatal(err)
		}
		if err = db.CacheSources([]model.Draw{d}); err != nil {
			t.Fatal(err)
		}
	}
	if err = e.publishDue(now); err != nil {
		t.Fatal(err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			continue
		}
		r, err := db.Get(g.ID, start.Format(model.IssueLayout))
		if err != nil || r.Origin != "scheduled" {
			t.Fatal(g.ID, "first live issue was not recovered", err)
		}
	}
	if after, err := db.RegionalBackfillCursor(); err != nil || after != "" {
		t.Fatal("fresh database recovery incomplete", after, err)
	}
}

type regionalSource struct {
	draws []model.Draw
	calls int
}

func (s *regionalSource) Latest(context.Context) (model.Draw, error) {
	return s.draws[len(s.draws)-1], nil
}
func (s *regionalSource) History(context.Context, int, string) (model.Page, error) {
	s.calls++
	items := make([]model.Draw, len(s.draws))
	for i := range s.draws {
		items[i] = s.draws[len(s.draws)-1-i]
	}
	return model.Page{Items: items}, nil
}

func TestRegionalRecoveryFetchesGapBehindExistingSourceAndLiveCursors(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := games.RegionalMarkSixStart()
	now := start.Add(3 * time.Minute)
	source := &regionalSource{}
	for i := 0; i < 3; i++ {
		d, err := synthetic(start.Add(time.Duration(i)*time.Minute - games.SourcePublishDelay))
		if err != nil {
			t.Fatal(err)
		}
		d.GeneratedAt = start.Add(-time.Hour)
		source.draws = append(source.draws, d)
	}
	if err = db.CacheSources(source.draws[2:]); err != nil {
		t.Fatal(err)
	}
	if err = db.CommitLive(nil, source.draws[2].Issue); err != nil {
		t.Fatal(err)
	}
	e := Engine{Store: db, Source: source}
	if err = e.syncAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatal("historical gap was not fetched")
	}
	for _, d := range source.draws {
		if _, err = db.Get("lhc-hk", d.Issue); err != nil {
			t.Fatal("missing restored issue", d.Issue, err)
		}
	}
	if after, _ := db.RegionalBackfillCursor(); after != "" {
		t.Fatal("recovery incomplete", after)
	}
	if err = e.syncAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatal("completed recovery refetched history")
	}
}
