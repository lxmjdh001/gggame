package engine

import (
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestTwentyMinuteMarkSixPublishesPreparedSourcesOnNewBoundaries(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := games.TwentyMinuteMarkSixStart()
	for _, delta := range []time.Duration{-30 * time.Minute, 0, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute} {
		d, err := synthetic(start.Add(delta - games.SourcePublishDelay))
		if err != nil {
			t.Fatal(err)
		}
		if err = db.CacheSources([]model.Draw{d}); err != nil {
			t.Fatal(err)
		}
	}
	e := Engine{Store: db}
	if err = e.publishDue(start.Add(40 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lhc-tw", "lhc-daily", "lhc-au"} {
		h, err := db.History(id, 100, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(h.Items) != 4 {
			t.Fatal(id, "incorrect published rounds", len(h.Items))
		}
		if h.Items[0].Issue != "202610142240" || h.Items[3].Issue != "202610142130" {
			t.Fatal(id, "incorrect issues")
		}
		want := 7
		if id == "lhc-au" {
			want = 8
		}
		for _, r := range h.Items {
			if len(r.Numbers) != want || r.Origin != "scheduled" {
				t.Fatal(id, "invalid result")
			}
		}
	}
}
