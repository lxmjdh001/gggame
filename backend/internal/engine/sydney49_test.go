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

func TestSydney49CachedPublicationPreservesOldEightAndStartsSeven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	g, _ := games.Find("lhc-au")
	start := games.Sydney49Start()
	sources := []model.Draw{}
	for _, at := range []time.Time{start.Add(-15 * time.Minute), start, start.Add(15 * time.Minute)} {
		d, err := synthetic(at.Add(-games.SourcePublishDelay))
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, d)
	}
	if err = db.CacheSources(sources); err != nil {
		t.Fatal(err)
	}
	e := Engine{Store: db}
	if err = e.publishDue(start.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	old, err := db.Get(g.ID, sources[0].Issue)
	if err != nil || len(old.Numbers) != 8 {
		t.Fatal("old projection was lost", err, old)
	}
	if _, err = db.Get(g.ID, sources[1].Issue); err != store.ErrNotFound {
		t.Fatal("first new issue published early", err)
	}
	preview, err := db.PreviewRound(g, start)
	if err != nil || preview == nil || len(preview.Numbers) != 7 {
		t.Fatal("new private preview not seven balls", err)
	}
	if err = e.publishDue(start); err != nil {
		t.Fatal(err)
	}
	first, err := db.Get(g.ID, sources[1].Issue)
	if err != nil || !reflect.DeepEqual(first.Numbers, sources[1].Groups[g.Group][7:14]) || len(first.Positions) != 7 {
		t.Fatal("new result does not use g10 positions8-14", err, first)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e.Store = db
	if err = e.publishDue(start.Add(15 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = e.publishDue(start.Add(15 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	oldAgain, err := db.Get(g.ID, old.Issue)
	if err != nil || !reflect.DeepEqual(oldAgain, old) {
		t.Fatal("restart rewrote old eight-ball history", err)
	}
	history, err := db.History(g.ID, 100, "")
	if err != nil || len(history.Items) != 3 {
		t.Fatal("missing/duplicate transition history", err, history)
	}
	for _, r := range history.Items {
		if len(r.Numbers) != g.NumberRuleAt(r.DrawAt).Count {
			t.Fatal("wrong issue rule", r)
		}
	}
}
