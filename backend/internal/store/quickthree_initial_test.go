package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestQuickThreeInitializationPreservesAdoptedAndPublishedNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	d := draftSource(at)
	if err = db.CacheSources([]model.Draw{d}); err != nil {
		t.Fatal(err)
	}
	zj, _ := games.Find("k3-zj")
	fj, _ := games.Find("k3-fj")
	for _, g := range []games.Session{zj, fj} {
		if target, err := db.QuickThreeSeedTarget(g, at); err != nil || !target.Equal(at) {
			t.Fatal("wrong initial target", target, err)
		}
	}
	base, err := db.PreviewRound(zj, at)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.ManualPreview(zj, d.Issue, []int{6, 6, 6}, base.Numbers, at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := db.ApplyPreview(zj, d.Issue, p.Candidate.ID, "admin", at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	existing, err := games.Project(fj, d, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CommitLive([]games.Round{existing}, d.Issue); err != nil {
		t.Fatal(err)
	}
	existing, _ = db.Get(fj.ID, d.Issue)
	if err = db.BackfillQuickThree(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get(zj.ID, d.Issue)
	if err != nil || !reflect.DeepEqual(got.Numbers, adopted.Numbers) {
		t.Fatal("accepted numbers replaced", err)
	}
	unchanged, _ := db.Get(fj.ID, d.Issue)
	if !reflect.DeepEqual(unchanged, existing) {
		t.Fatal("published record replaced")
	}
	before, _ := db.History(zj.ID, 100, "")
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if target, err := db.QuickThreeSeedTarget(zj, at.Add(24*time.Hour)); err != nil || !target.Equal(at) {
		t.Fatal("restart moved original target", err)
	}
	if err = db.BackfillQuickThree(at.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, _ := db.History(zj.ID, 100, "")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart rewrote target")
	}
	for _, id := range []string{"k3-1", "k3-3", "k3-5"} {
		g, _ := games.Find(id)
		if target, err := db.QuickThreeSeedTarget(g, at); err != nil || !target.IsZero() {
			t.Fatal("old session enrolled in initialization")
		}
	}
}

func TestQuickThreeInitialTargetBeforePublicationUsesPreviousIssue(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, _ := games.Find("k3-zj")
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	target, err := db.QuickThreeSeedTarget(g, at.Add(-time.Nanosecond))
	if err != nil || !target.Equal(at.Add(-20*time.Minute)) {
		t.Fatal("unpublished issue reserved", target, err)
	}
}
