package store

import (
	"io"
	"log"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

// Pause bbolt after the Update callback has succeeded, before physical Commit.
// This reproduces fsync/scheduling delay that a callback clock check cannot cover.
type pausedDrawCommit struct {
	bolt.Logger
	armed            atomic.Bool
	entered, release chan struct{}
}

func (l *pausedDrawCommit) Debugf(format string, args ...interface{}) {
	if format == "Committing transaction %d" && l.armed.CompareAndSwap(true, false) {
		close(l.entered)
		<-l.release
	}
}

func TestAdoptionCommitSerializesPayoutPreview(t *testing.T) {
	logger := &pausedDrawCommit{Logger: &bolt.DefaultLogger{Logger: log.New(io.Discard, "", 0)}, entered: make(chan struct{}), release: make(chan struct{})}
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := bolt.Open(path, 0600, &bolt.Options{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	db = &Store{db: raw}
	defer func() { db.Close() }()
	var release sync.Once
	defer release.Do(func() { close(logger.release) })
	g, _ := games.Find("ffc-1")
	at := time.Date(2026, 10, 12, 12, 5, 5, 0, model.Beijing)
	cutoff := at.Truncate(time.Minute)
	source := draftSource(at)
	if err = db.CacheSources([]model.Draw{source}); err != nil {
		t.Fatal(err)
	}
	initial, err := db.PreviewRound(g, at)
	if err != nil {
		t.Fatal(err)
	}
	var draft DrawPreview
	for {
		draft, err = db.RandomPreview(g, source.Issue, "", true, cutoff.Add(-time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(draft.Candidate.Round.Numbers, initial.Numbers) {
			break
		}
	}
	var clock atomic.Int64
	clock.Store(cutoff.Add(-time.Millisecond).UnixNano())
	logger.armed.Store(true)
	adopted := make(chan error, 1)
	go func() {
		_, e := db.applyPreview(g, source.Issue, draft.Candidate.ID, "admin", func() time.Time { return time.Unix(0, clock.Load()) })
		adopted <- e
	}()
	select {
	case <-logger.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("adoption never reached physical commit")
	}
	clock.Store(cutoff.Add(20 * time.Millisecond).UnixNano())
	type result struct {
		round *games.Round
		err   error
	}
	read := make(chan result, 1)
	started := make(chan struct{})
	go func() { close(started); r, e := db.PreviewRound(g, at); read <- result{r, e} }()
	<-started
	select {
	case <-read:
		t.Fatal("payout preview read the old snapshot while adoption was committing")
	case <-time.After(50 * time.Millisecond):
	}
	release.Do(func() { close(logger.release) })
	select {
	case err = <-adopted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("adoption did not finish")
	}
	select {
	case got := <-read:
		if got.err != nil || got.round == nil || !reflect.DeepEqual(got.round.Numbers, draft.Candidate.Round.Numbers) {
			t.Fatalf("payout did not see committed candidate: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("payout preview remained blocked")
	}
	// Publication after a restart must use the exact same accepted numbers.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := games.Project(g, source, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CommitLive([]games.Round{original}, source.Issue); err != nil {
		t.Fatal(err)
	}
	history, err := db.History(g.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || !reflect.DeepEqual(history.Items[0].Numbers, draft.Candidate.Round.Numbers) {
		t.Fatal("history diverged from payout preview")
	}
}

func TestAdoptionRejectsDeadlineAfterQueueOrValidation(t *testing.T) {
	for _, stage := range []string{"arrival", "queued", "before_commit"} {
		t.Run(stage, func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			g, _ := games.Find("ffc-1")
			at := time.Date(2026, 10, 12, 12, 5, 5, 0, model.Beijing)
			cutoff := at.Truncate(time.Minute)
			before := cutoff.Add(-time.Millisecond)
			source := draftSource(at)
			if err = db.CacheSources([]model.Draw{source}); err != nil {
				t.Fatal(err)
			}
			initial, err := db.PreviewRound(g, at)
			if err != nil {
				t.Fatal(err)
			}
			draft, err := db.RandomPreview(g, source.Issue, "", true, before)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			clock := func() time.Time {
				calls++
				switch stage {
				case "arrival":
					return cutoff
				case "queued":
					if calls >= 2 {
						return cutoff
					}
				case "before_commit":
					if calls >= 3 {
						return cutoff.Add(time.Millisecond)
					}
				}
				return before
			}
			accepted, err := db.applyPreview(g, source.Issue, draft.Candidate.ID, "admin", clock)
			if err == nil || err.Error() != "已超过开奖时间不可修改" || accepted != nil {
				t.Fatalf("expired operation accepted: %v / %v", accepted, err)
			}
			current, err := db.PreviewRound(g, at)
			if err != nil {
				t.Fatal(err)
			}
			// Project stamps preview reads with their read time; it is not a draw change.
			current.PublishedAt = initial.PublishedAt
			if !reflect.DeepEqual(current, initial) {
				t.Fatal("rejected adoption changed effective result")
			}
			err = db.db.View(func(tx *bolt.Tx) error {
				if b := tx.Bucket(drawChangeLog); b != nil && b.Get([]byte(draft.Candidate.ID)) != nil {
					t.Fatal("rejected adoption left an audit entry")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueuedAdoptionRechecksServerClock(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, _ := games.Find("ffc-1")
	at := time.Date(2026, 10, 12, 12, 5, 5, 0, model.Beijing)
	cutoff := at.Truncate(time.Minute)
	before := cutoff.Add(-time.Millisecond)
	source := draftSource(at)
	if err = db.CacheSources([]model.Draw{source}); err != nil {
		t.Fatal(err)
	}
	draft, err := db.RandomPreview(g, source.Issue, "", true, before)
	if err != nil {
		t.Fatal(err)
	}
	// A different database write owns the lock while the request arrives before :00.
	blocker, err := db.db.Begin(true)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var current atomic.Int64
	current.Store(before.UnixNano())
	arrived := make(chan struct{})
	var once sync.Once
	clock := func() time.Time {
		value := time.Unix(0, current.Load())
		once.Do(func() { close(arrived) })
		return value
	}
	done := make(chan error, 1)
	go func() { _, e := db.applyPreview(g, source.Issue, draft.Candidate.ID, "admin", clock); done <- e }()
	<-arrived
	current.Store(cutoff.UnixNano())
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil || err.Error() != "已超过开奖时间不可修改" {
			t.Fatalf("queued expired request: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued request remained blocked")
	}
	if err = db.db.View(func(tx *bolt.Tx) error {
		value, e := override(tx, g.ID, source.Issue)
		if e != nil {
			return e
		}
		if value != nil {
			t.Fatal("queued expired request changed the result")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
