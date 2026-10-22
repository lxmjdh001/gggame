package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestRandomPreviewsComparisonAndInvalidation(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, _ := games.Find("lhc-daily")
	now := time.Date(2026, 10, 11, 21, 29, 20, 0, model.Beijing)
	at := g.Next(now)
	source := draftSource(at)
	if err := db.CacheSources([]model.Draw{source}); err != nil {
		t.Fatal(err)
	}
	drafts, err := db.RandomPreviews(g, source.Issue, nil, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 5 {
		t.Fatal("missing candidates")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, draft := range drafts {
		if draft.Candidate == nil || seen[draft.Candidate.ID] {
			t.Fatal("missing or duplicate candidate")
		}
		seen[draft.Candidate.ID] = true
		ids = append(ids, draft.Candidate.ID)
		if !reflect.DeepEqual(draft.Current.Numbers, drafts[0].Current.Numbers) || draft.Candidate.BaseVersion != drafts[0].Candidate.BaseVersion {
			t.Fatal("mixed comparison versions")
		}
	}
	refresh, err := db.RandomPreviews(g, source.Issue, ids, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	for i, draft := range refresh {
		if draft.Candidate.ID != drafts[i].Candidate.ID || !reflect.DeepEqual(draft.Candidate.Round.Numbers, drafts[i].Candidate.Round.Numbers) {
			t.Fatal("refresh rerolled candidate")
		}
	}
	for _, bad := range []struct {
		count int
		ids   []string
	}{{2, nil}, {6, nil}, {-1, nil}, {5, ids}, {0, []string{""}}, {0, []string{ids[0], ids[0]}}, {0, []string{"missing"}}} {
		if _, err := db.RandomPreviews(g, source.Issue, bad.ids, bad.count, now); err == nil {
			t.Fatalf("accepted invalid batch %+v", bad)
		}
	}
	if _, err := db.RandomPreviews(g, source.Issue, nil, 5, at.Add(-5*time.Second)); err == nil {
		t.Fatal("batch allowed at cutoff")
	}
	manual, err := db.ManualPreview(g, source.Issue, []int{7, 6, 5, 4, 3, 2, 1}, drafts[0].Current.Numbers, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApplyPreview(g, source.Issue, manual.Candidate.ID, "admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RandomPreviews(g, source.Issue, ids, 0, now); err == nil {
		t.Fatal("stale comparison refreshed")
	}
	for _, id := range ids {
		if _, err := db.ApplyPreview(g, source.Issue, id, "admin", now); err == nil {
			t.Fatal("stale batch adopted after manual edit")
		}
	}
	if _, err := db.Get(g.ID, source.Issue); err != ErrNotFound {
		t.Fatal("preview published early")
	}
}
