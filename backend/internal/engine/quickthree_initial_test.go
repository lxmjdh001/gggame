package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
)

var addedQuickThreeIDs = []string{"k3-zj", "k3-fj", "k3-gd", "k3-hn", "k3-tw", "k3-gx"}

func TestQuickThreeSeedUsesCachedCurrentAtAndAfterDrawMinute(t *testing.T) {
	for _, elapsed := range []time.Duration{5 * time.Second, 55 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
			now := at.Add(elapsed)
			added := map[string]bool{}
			for _, id := range addedQuickThreeIDs {
				added[id] = true
			}
			// Simulate an existing installation without generating unrelated seeds.
			for _, g := range games.All() {
				if !added[g.ID] {
					if err = db.CommitSeed(g.ID, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			oldSource, err := synthetic(at.Add(-20*time.Minute - games.SourcePublishDelay))
			if err != nil {
				t.Fatal(err)
			}
			old := map[string]games.History{}
			for _, id := range []string{"k3-1", "k3-3", "k3-5"} {
				g, _ := games.Find(id)
				r, err := games.Project(g, oldSource, "scheduled")
				if err != nil {
					t.Fatal(err)
				}
				if err = db.SaveMany([]games.Round{r}); err != nil {
					t.Fatal(err)
				}
				old[id], _ = db.History(id, 100, "")
			}
			d, err := synthetic(at.Add(-games.SourcePublishDelay))
			if err != nil {
				t.Fatal(err)
			}
			if err = db.CacheSources([]model.Draw{d}); err != nil {
				t.Fatal(err)
			}
			if err = db.CommitLive(nil, d.Issue); err != nil {
				t.Fatal(err)
			}
			e := Engine{Store: db}
			if err = e.Seed(now); err != nil {
				t.Fatal(err)
			}
			for _, id := range addedQuickThreeIDs {
				if _, err = db.Get(id, d.Issue); !errors.Is(err, store.ErrNotFound) {
					t.Fatal(id, "target occupied by simulated history")
				}
				h, err := db.History(id, 100, "")
				if err != nil || h.Total != 100 {
					t.Fatal(id, "older history missing", err)
				}
				for _, r := range h.Items {
					if !r.DrawAt.Before(at) {
						t.Fatal(id, "seed reached initial target")
					}
				}
			}
			if err = e.publishDue(now); err != nil {
				t.Fatal(err)
			}
			for _, id := range addedQuickThreeIDs {
				g, _ := games.Find(id)
				r, err := db.Get(id, d.Issue)
				want, _ := games.Project(g, d, "scheduled")
				if err != nil || r.Origin != "scheduled" || !reflect.DeepEqual(r.Numbers, want.Numbers) {
					t.Fatal(id, "current source not recovered", err)
				}
			}
			for id, want := range old {
				got, _ := db.History(id, 100, "")
				if !reflect.DeepEqual(got, want) {
					t.Fatal(id, "existing history changed")
				}
			}
			if cursor, _ := db.LiveCursor(); cursor != d.Issue {
				t.Fatal("live cursor changed")
			}
		})
	}
}

func TestQuickThreeInitializationFetchesMissingTargetAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	e := Engine{Store: db}
	// The independent earlier Mark Six migration has already completed.
	if err = e.publishDue(at); err != nil {
		t.Fatal(err)
	}
	for _, id := range addedQuickThreeIDs {
		g, _ := games.Find(id)
		if _, err = db.QuickThreeSeedTarget(g, at); err != nil {
			t.Fatal(err)
		}
	}
	source := &regionalSource{}
	for _, delta := range []time.Duration{0, time.Minute} {
		d, err := synthetic(at.Add(delta - games.SourcePublishDelay))
		if err != nil {
			t.Fatal(err)
		}
		d.GeneratedAt = at.Add(-time.Hour)
		source.draws = append(source.draws, d)
	}
	if err = db.CacheSources(source.draws[1:]); err != nil {
		t.Fatal(err)
	}
	if err = db.CommitLive(nil, source.draws[1].Issue); err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillQuickThree(at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if cursor, _ := db.QuickThreeBackfillCursor(); cursor != "202610161219" {
		t.Fatal("missing target discarded", cursor)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e.Store, e.Source = db, source
	if err = e.syncAt(context.Background(), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, id := range addedQuickThreeIDs {
		r, err := db.Get(id, source.draws[0].Issue)
		if err != nil || r.Origin != "scheduled" {
			t.Fatal(id, "initial target lost across restart", err)
		}
	}
	if cursor, _ := db.QuickThreeBackfillCursor(); cursor != "" {
		t.Fatal("target not completed", cursor)
	}
	if err = e.syncAt(context.Background(), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatal("completed initialization kept fetching history", source.calls)
	}
}
