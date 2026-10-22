package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestRegionalBackfillPreservesPublishedAndOverridesWithoutRewinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	start := games.RegionalMarkSixStart()
	draws := []model.Draw{draftSource(start), draftSource(start.Add(time.Minute))}
	if err = db.CacheSources(draws); err != nil {
		t.Fatal(err)
	}
	hk, _ := games.Find("lhc-hk")
	p, err := db.ManualPreview(hk, draws[0].Issue, []int{7, 6, 5, 4, 3, 2, 1}, []int{1, 2, 3, 4, 5, 6, 7}, start.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := db.ApplyPreview(hk, draws[0].Issue, p.Candidate.ID, "admin", start.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	tw, _ := games.Find("lhc-tw")
	existing, err := games.Project(tw, draws[0], "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	existing.PublishedAt = start
	if err = db.CommitLive([]games.Round{existing}, draws[1].Issue); err != nil {
		t.Fatal(err)
	}
	existing, err = db.Get(tw.ID, draws[0].Issue)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillRegionalMarkSix(start.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			continue
		}
		r, err := db.Get(g.ID, draws[0].Issue)
		if err != nil || r.Origin != "scheduled" {
			t.Fatal(g.ID, "missing recovery", err)
		}
		if g.ID == hk.ID && !reflect.DeepEqual(r.Numbers, adopted.Numbers) {
			t.Fatal("accepted draw lost")
		}
		if g.ID == tw.ID && !reflect.DeepEqual(r, existing) {
			t.Fatal("published history changed")
		}
	}
	cursor, _ := db.LiveCursor()
	if cursor != draws[1].Issue {
		t.Fatal("publication cursor rewound")
	}
	if after, err := db.RegionalBackfillCursor(); err != nil || after != "" {
		t.Fatal("migration incomplete", after, err)
	}
	before, _ := db.History(hk.ID, 100, "")
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.BackfillRegionalMarkSix(start.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, _ := db.History(hk.ID, 100, "")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart rewrote recovered history")
	}
}

func TestRegionalBackfillRetriesMissingSourceAndKeepsOriginalTarget(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := games.RegionalMarkSixStart()
	draws := []model.Draw{draftSource(start), draftSource(start.Add(time.Minute)), draftSource(start.Add(2 * time.Minute))}
	if err = db.CacheSources([]model.Draw{draws[0], draws[2]}); err != nil {
		t.Fatal(err)
	}
	if err = db.CommitLive(nil, draws[2].Issue); err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillRegionalMarkSix(start.Add(3 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if after, _ := db.RegionalBackfillCursor(); after != draws[0].Issue {
		t.Fatal("missing minute skipped", after)
	}
	if _, err = db.Get("lhc-hk", draws[2].Issue); !errors.Is(err, ErrNotFound) {
		t.Fatal("recovery jumped over gap")
	}
	// Publication may progress independently while recovery waits for the gap.
	later := start.Add(time.Hour).Format(model.IssueLayout)
	if err = db.CommitLive(nil, later); err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillRegionalMarkSix(start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, _ := db.RegionalBackfillCursor(); after != draws[0].Issue {
		t.Fatal("gap unexpectedly completed")
	}
	if err = db.CacheSources([]model.Draw{draws[1]}); err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillRegionalMarkSix(start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, _ := db.RegionalBackfillCursor(); after != "" {
		t.Fatal("original target was extended", after)
	}
	if cursor, _ := db.LiveCursor(); cursor != later {
		t.Fatal("live cursor changed during recovery")
	}
	if _, err = db.Get("lhc-hk", draws[2].Issue); err != nil {
		t.Fatal("gap retry failed", err)
	}
}

func TestRegionalBackfillBeforeActivationAndFutureCursorDoNotPublishEarly(t *testing.T) {
	for _, beforeActivation := range []bool{true, false} {
		t.Run(map[bool]string{true: "before_activation", false: "future_cursor"}[beforeActivation], func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			start := games.RegionalMarkSixStart()
			d := draftSource(start)
			if err = db.CacheSources([]model.Draw{d}); err != nil {
				t.Fatal(err)
			}
			cursor := d.Issue
			if beforeActivation {
				cursor = start.Add(-time.Minute).Format(model.IssueLayout)
			}
			if err = db.CommitLive(nil, cursor); err != nil {
				t.Fatal(err)
			}
			if err = db.BackfillRegionalMarkSix(start.Add(-time.Nanosecond)); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Get("lhc-hk", d.Issue); !errors.Is(err, ErrNotFound) {
				t.Fatal("published before05")
			}
		})
	}
}
