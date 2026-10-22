package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

type sourceStub struct {
	draw model.Draw
	err  error
}

func (s sourceStub) Latest(context.Context) (model.Draw, error) { return s.draw, s.err }
func (s sourceStub) History(context.Context, int, string) (model.Page, error) {
	return model.Page{Items: []model.Draw{s.draw}}, s.err
}
func TestSeed100PerSessionSharedSourcesAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().In(model.Beijing).Add(-20 * time.Minute).Truncate(10 * time.Minute).Add(time.Second)
	e := Engine{Store: db}
	if err = e.Seed(at); err != nil {
		t.Fatal(err)
	}
	snapshot := map[string][]games.Round{}
	for _, session := range games.All() {
		h, err := db.History(session.ID, 100, "")
		if err != nil || h.Total != 100 || len(h.Items) != 100 {
			t.Fatalf("seed %s: %d %v", session.ID, h.Total, err)
		}
		snapshot[session.ID] = h.Items
		for i, r := range h.Items {
			if r.Origin != "simulated_history" || !r.DrawAt.Before(at) || !session.Matches(r.DrawAt) {
				t.Fatal("bad seed metadata")
			}
			if i > 0 && !session.SimulatedDue(h.Items[i-1].DrawAt.Add(-time.Nanosecond)).Equal(r.DrawAt) {
				t.Fatal("seed cadence wrong")
			}
			d, err := db.SeedSource(r.SourceIssue)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := games.Project(session, d, "simulated_history")
			if err != nil || !reflect.DeepEqual(expected.Numbers, r.Numbers) {
				t.Fatal("seed source mismatch")
			}
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e.Store = db
	if err = e.Seed(at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for id, records := range snapshot {
		h, _ := db.History(id, 100, "")
		if !reflect.DeepEqual(records, h.Items) || h.Total != 100 {
			t.Fatal("restart regenerated history")
		}
	}
	d, err := synthetic(at)
	if err != nil {
		t.Fatal(err)
	}
	e.Source = sourceStub{draw: d}
	if err = e.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = e.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, s := range games.All() {
		h, _ := db.History(s.ID, 100, "")
		want := 100
		if s.MatchesSource(at) {
			want = 101
			if h.Items[0].Origin != "scheduled" {
				t.Fatal("live result mislabeled")
			}
		}
		if h.Total != want {
			t.Fatalf("%s count=%d want %d", s.ID, h.Total, want)
		}
	}
	cursor, _ := db.LiveCursor()
	e.Source = sourceStub{err: errors.New("source offline")}
	if e.Sync(context.Background()) == nil {
		t.Fatal("source failure not surfaced")
	}
	after, _ := db.LiveCursor()
	if after != cursor {
		t.Fatal("source outage advanced cursor")
	}
}

func TestHoldSourceUntilSecond05(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "timing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 10, 10, 21, 30, 1, 0, model.Beijing)
	d, err := synthetic(at)
	if err != nil {
		t.Fatal(err)
	}
	d.GeneratedAt = at
	e := Engine{Store: db, Source: sourceStub{draw: d}}
	for _, offset := range []time.Duration{0, time.Second, 4*time.Second - time.Nanosecond} {
		if err := e.syncAt(context.Background(), at.Add(offset)); err != nil {
			t.Fatal(err)
		}
		cursor, _ := db.LiveCursor()
		if cursor != "" {
			t.Fatal("cursor advanced before publication")
		}
		for _, s := range games.All() {
			if _, err := db.Get(s.ID, d.Issue); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("%s result exposed before05", s.ID)
			}
		}
	}
	if err := e.syncAt(context.Background(), at.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, s := range games.All() {
		if !s.MatchesSource(at) {
			continue
		}
		r, err := db.Get(s.ID, d.Issue)
		if err != nil || !r.DrawAt.Equal(at.Add(4*time.Second)) {
			t.Fatalf("%s not published at05: %v", s.ID, err)
		}
	}
	if err := e.syncAt(context.Background(), at.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, s := range games.All() {
		h, _ := db.History(s.ID, 100, "")
		want := 0
		if s.MatchesSource(at) {
			want = 1
		}
		if h.Total != want {
			t.Fatal("duplicate publication")
		}
	}
}

func TestDailyPrefetchPrivateOfflineRestartAndExactPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daily.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Cache a next-day midnight batch before any of its results are publishable.
	at := time.Date(2026, 10, 12, 0, 0, 1, 0, model.Beijing)
	d, err := synthetic(at)
	if err != nil {
		t.Fatal(err)
	}
	d.GeneratedAt = at.Add(-time.Hour)
	e := Engine{Store: db, Source: sourceStub{draw: d}}
	if err = e.syncAt(context.Background(), at.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, s := range games.All() {
		h, _ := db.History(s.ID, 100, "")
		if h.Total != 0 {
			t.Fatal("private numbers leaked", s.ID)
		}
	}
	db.Close()
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e.Store = db
	e.Source = sourceStub{err: errors.New("offline")}
	if err = e.syncAt(context.Background(), at.Add(4*time.Second-time.Nanosecond)); err == nil {
		t.Fatal("outage hidden")
	}
	for _, s := range games.All() {
		h, _ := db.History(s.ID, 100, "")
		if h.Total != 0 {
			t.Fatal("published before05")
		}
	}
	if err = e.syncAt(context.Background(), at.Add(4*time.Second)); err == nil {
		t.Fatal("outage hidden")
	}
	for _, s := range games.All() {
		if !s.MatchesSource(at) {
			continue
		}
		r, err := db.Get(s.ID, d.Issue)
		if err != nil {
			t.Fatal(s.ID, err)
		}
		selection, _ := model.Select(d, s.Group, s.Positions)
		if !reflect.DeepEqual(selection.Numbers, r.Numbers) || r.SourceIssue != r.Issue || r.Issue != d.Issue {
			t.Fatal("wrong fixed projection", s.ID)
		}
	}
	_ = e.syncAt(context.Background(), at.Add(time.Minute))
	for _, s := range games.All() {
		if !s.MatchesSource(at) {
			continue
		}
		h, _ := db.History(s.ID, 100, "")
		if h.Total != 1 {
			t.Fatal("duplicate result")
		}
	}
	changed := d
	changed.Groups = map[string][]int{}
	for k, v := range d.Groups {
		changed.Groups[k] = append([]int(nil), v...)
	}
	changed.Groups["g1"][0] = (changed.Groups["g1"][0] + 1) % 10
	if err = db.CacheSources([]model.Draw{changed}); err == nil {
		t.Fatal("cache allowed replacement")
	}
}

type blockingSource struct{ started chan struct{} }

func (s blockingSource) Latest(ctx context.Context) (model.Draw, error) {
	close(s.started)
	<-ctx.Done()
	return model.Draw{}, ctx.Err()
}
func (s blockingSource) History(ctx context.Context, _ int, _ string) (model.Page, error) {
	<-ctx.Done()
	return model.Page{}, ctx.Err()
}

func TestPublicationWorkerDoesNotWaitForNetwork(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "workers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Now().Add(-time.Minute).Truncate(time.Minute).Add(time.Second)
	d, err := synthetic(at)
	if err != nil {
		t.Fatal(err)
	}
	source := blockingSource{started: make(chan struct{})}
	e := Engine{Store: db, Source: source, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); e.Run(ctx) }()
	defer func() { cancel(); <-done }()
	<-source.started
	// Add a due batch after the network worker is already blocked.
	if err = db.CacheSources([]model.Draw{d}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = db.Get("ffc-1", d.Issue); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("network timeout blocked publication")
}

func TestRefreshSchemaFourCacheForNewRegions(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 10, 13, 21, 30, 1, 0, model.Beijing)
	now := at.Add(-time.Hour)
	upgraded, err := synthetic(at)
	if err != nil {
		t.Fatal(err)
	}
	upgraded.GeneratedAt = now.Add(-time.Hour)
	upgraded.GroupGeneratedAt = map[string]time.Time{}
	old := upgraded
	old.SchemaVersion = 4
	old.Groups = map[string][]int{}
	for group, numbers := range upgraded.Groups {
		if group == "g9" || group == "g10" || group == "g11" {
			upgraded.GroupGeneratedAt[group] = now.Add(-time.Minute)
		} else {
			old.Groups[group] = numbers
		}
	}
	old.GroupGeneratedAt = nil
	if err = db.CacheSources([]model.Draw{old}); err != nil {
		t.Fatal(err)
	}
	e := Engine{Store: db, Source: sourceStub{draw: upgraded}}
	if err = e.prefetch(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" || !g.MatchesSource(at) {
			continue
		}
		r, err := db.PreviewRound(g, at.Add(games.SourcePublishDelay))
		if err != nil || r == nil || len(r.Numbers) != g.NumberRuleAt(at.Add(games.SourcePublishDelay)).Count {
			t.Fatal(g.ID, "not upgraded", err)
		}
	}
	if err = e.prefetch(context.Background(), now); err != nil {
		t.Fatal("repeat refresh", err)
	}
	if err = e.publishDue(at.Add(games.SourcePublishDelay)); err != nil {
		t.Fatal(err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" || !g.MatchesSource(at) {
			continue
		}
		r, err := db.Get(g.ID, upgraded.Issue)
		if err != nil || r.Origin != "scheduled" {
			t.Fatal(g.ID, "not published", err)
		}
	}
}
