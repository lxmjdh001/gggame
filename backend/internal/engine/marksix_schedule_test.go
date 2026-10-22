package engine

import (
	"path/filepath"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestMarkSixLegacyDailySourcesRemainValidBeforeScheduleChange(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.CommitLive(nil, "202610122130"); err != nil {
		t.Fatal(err)
	}
	for day := 13; day <= 15; day++ {
		at := time.Date(2026, 10, day, 21, 30, 1, 0, model.Beijing)
		d, err := synthetic(at)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.CacheSources([]model.Draw{d}); err != nil {
			t.Fatal(err)
		}
	}
	e := Engine{Store: db}
	if err = e.publishDue(time.Date(2026, 10, 15, 21, 30, 5, 0, model.Beijing)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lhc-hk", "lhc-tw", "lhc-daily", "lhc-sg", "lhc-bj", "lhc-au"} {
		h, err := db.History(id, 100, "")
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if id == "lhc-hk" || id == "lhc-tw" {
			want = 2
		}
		if len(h.Items) != want {
			t.Fatal(id, "draw count", len(h.Items), want)
		}
		if id == "lhc-hk" && (h.Items[0].Issue != "202610152130" || h.Items[1].Issue != "202610132130") {
			t.Fatal("wrong Hong Kong dates")
		}
	}
}
