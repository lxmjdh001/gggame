package engine

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
)

func TestHongKongHistoryMigrationPreservesPublishedAndSharedSources(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e := Engine{Store: db}
	hk, _ := games.Find("lhc-hk")
	old := make([]games.Round, 0, 100)
	for i := 0; i < 100; i++ {
		at := time.Date(2026, 10, 12-i, 21, 30, 1, 0, model.Beijing)
		r, err := e.simulatedRound(hk, at)
		if err != nil {
			t.Fatal(err)
		}
		// Preserve both an off-cycle actual draw and an actual draw sharing a new history slot.
		if i < 2 {
			r.Origin = "scheduled"
		}
		old = append(old, r)
	}
	if err = db.CommitSeed(hk.ID, old); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		old[i], err = db.Get(hk.ID, old[i].Issue)
		if err != nil {
			t.Fatal(err)
		}
	}
	tw, _ := games.Find("lhc-tw")
	other, err := e.simulatedRound(tw, old[0].DrawAt.Add(-games.SourcePublishDelay))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CommitSeed(tw.ID, []games.Round{other}); err != nil {
		t.Fatal(err)
	}
	other, err = db.Get(tw.ID, other.Issue)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.Beijing)
	if err = e.Seed(now); err != nil {
		t.Fatal(err)
	}
	for _, r := range old[:2] {
		got, err := db.Get(hk.ID, r.Issue)
		if err != nil || !reflect.DeepEqual(got, r) {
			t.Fatal("published draw changed", err)
		}
	}
	gotOther, err := db.Get(tw.ID, other.Issue)
	if err != nil || !reflect.DeepEqual(gotOther, other) {
		t.Fatal("other region changed", err)
	}
	h, err := db.History(hk.ID, 100, "")
	if err != nil || h.Total != 101 {
		t.Fatal("wrong total", h.Total, err)
	}
	for i, r := range h.Items[1:] {
		want := games.HongKongScheduleStart().Add(-time.Duration(i+1) * 48 * time.Hour)
		if !r.DrawAt.Equal(want) {
			t.Fatal("wrong historical cadence", r.DrawAt, want)
		}
		if i > 0 && r.Origin != "simulated_history" {
			t.Fatal("simulation label lost")
		}
		if len(r.Numbers) != 7 {
			t.Fatal("wrong number count")
		}
		seen := map[int]bool{}
		for _, n := range r.Numbers {
			if n < 1 || n > 49 || seen[n] {
				t.Fatal("invalid number", n)
			}
			seen[n] = true
		}
		for _, before := range old {
			if before.Issue == r.Issue && !reflect.DeepEqual(before.Numbers, r.Numbers) {
				t.Fatal("existing source numbers changed")
			}
		}
	}
	if err = e.Seed(now.Add(72 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, _ := db.History(hk.ID, 100, "")
	if !reflect.DeepEqual(h, after) {
		t.Fatal("restart regenerated history")
	}
}
