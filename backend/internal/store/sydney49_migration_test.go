package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestSydney49MigrationArchivesOnlyIncompatibleUnpublishedNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := games.Find("lhc-au")
	start := games.Sydney49Start()
	oldAt := start.Add(-15 * time.Minute)
	oldSource, newSource := draftSource(oldAt), draftSource(start)
	if err = db.CacheSources([]model.Draw{oldSource, newSource}); err != nil {
		t.Fatal(err)
	}
	historical, err := games.Project(g, oldSource, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	current, err := games.Project(g, newSource, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveMany([]games.Round{historical}); err != nil {
		t.Fatal(err)
	}
	stale := current
	stale.Numbers = []int{1, 2, 3, 4, 5, 6, 7, 8}
	stale.Positions = []int{8, 9, 10, 11, 12, 13, 14, 15}
	next := stale
	next.Issue = start.Add(15 * time.Minute).Format(model.IssueLayout)
	next.DrawAt = start.Add(15 * time.Minute)
	var oldRaw, sourceRaw, logRaw []byte
	originals := map[string][]byte{}
	if err = db.db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{drawCandidates, drawOverrides, drawChangeLog} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		for _, c := range []DrawCandidate{{ID: "stale", Round: stale}, {ID: "current", Round: current}, {ID: "historical", Round: historical}} {
			raw, err := json.Marshal(c)
			if err != nil {
				return err
			}
			originals["candidate:"+c.ID] = raw
			if err = tx.Bucket(drawCandidates).Put([]byte(c.ID), raw); err != nil {
				return err
			}
		}
		for _, r := range []games.Round{historical, stale, next} {
			raw, err := json.Marshal(drawOverride{Round: r, Version: 2})
			if err != nil {
				return err
			}
			key := drawKey(r.SessionID, r.Issue)
			originals["override:"+string(key)] = raw
			if err = tx.Bucket(drawOverrides).Put(key, raw); err != nil {
				return err
			}
		}
		logRaw = []byte(`{"actor":"admin","note":"keep original audit"}`)
		if err := tx.Bucket(drawChangeLog).Put([]byte("stale"), logRaw); err != nil {
			return err
		}
		oldRaw = bytes.Clone(tx.Bucket(rounds).Bucket([]byte(g.ID)).Get([]byte(historical.Issue)))
		sourceRaw = bytes.Clone(tx.Bucket(sourceCache).Get([]byte(current.Issue)))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		if err = db.db.Update(migrateSydney49); err != nil {
			t.Fatal(err)
		}
		if err = db.db.View(func(tx *bolt.Tx) error {
			if !bytes.Equal(oldRaw, tx.Bucket(rounds).Bucket([]byte(g.ID)).Get([]byte(historical.Issue))) {
				t.Fatal("published history rewritten")
			}
			if !bytes.Equal(sourceRaw, tx.Bucket(sourceCache).Get([]byte(current.Issue))) {
				t.Fatal("source cache rewritten")
			}
			if !bytes.Equal(logRaw, tx.Bucket(drawChangeLog).Get([]byte("stale"))) {
				t.Fatal("adoption audit changed")
			}
			archive := tx.Bucket(sydney45UnpublishedArchive)
			for _, key := range []string{"candidate:stale", "override:" + string(drawKey(g.ID, stale.Issue)), "override:" + string(drawKey(g.ID, next.Issue))} {
				if !bytes.Equal(archive.Get([]byte(key)), originals[key]) {
					t.Fatal("stale private data not archived exactly", key)
				}
			}
			for _, id := range []string{"current", "historical"} {
				if !bytes.Equal(tx.Bucket(drawCandidates).Get([]byte(id)), originals["candidate:"+id]) {
					t.Fatal("compatible draft removed", id)
				}
			}
			if tx.Bucket(drawCandidates).Get([]byte("stale")) != nil || tx.Bucket(drawOverrides).Get(drawKey(g.ID, stale.Issue)) != nil || tx.Bucket(drawOverrides).Get(drawKey(g.ID, next.Issue)) != nil {
				t.Fatal("incompatible future numbers survived")
			}
			if !bytes.Equal(tx.Bucket(drawOverrides).Get(drawKey(g.ID, historical.Issue)), originals["override:"+string(drawKey(g.ID, historical.Issue))]) {
				t.Fatal("old adopted result changed")
			}
			if string(tx.Bucket(metadata).Get(sydney49StartKey)) != start.Format(time.RFC3339) {
				t.Fatal("cutover not persisted")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := db.PreviewRound(g, start)
	if err != nil || preview == nil || !reflect.DeepEqual(preview.Numbers, current.Numbers) || len(preview.Numbers) != 7 {
		t.Fatal("new source projection not restored", err, preview)
	}
	if _, err = db.RandomPreview(g, current.Issue, "stale", false, start.Add(-2*time.Minute)); err == nil {
		t.Fatal("archived 8-ball candidate remained usable")
	}
}

func TestSydney49RandomAndManualFollowIssueRules(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, _ := games.Find("lhc-au")
	for _, at := range []time.Time{games.Sydney49Start().Add(-15 * time.Minute), games.Sydney49Start(), games.Sydney49Start().Add(15 * time.Minute)} {
		source := draftSource(at)
		if err = db.CacheSources([]model.Draw{source}); err != nil {
			t.Fatal(err)
		}
		now := at.Add(-2 * time.Minute)
		rule := g.NumberRuleAt(at)
		previews, err := db.RandomPreviews(g, source.Issue, nil, 5, now)
		if err != nil {
			t.Fatal(err)
		}
		seenSets := map[string]bool{}
		for _, preview := range previews {
			numbers := preview.Candidate.Round.Numbers
			key := fmt.Sprint(numbers)
			if len(numbers) != rule.Count || seenSets[key] {
				t.Fatal("wrong/random duplicate result", numbers)
			}
			seenSets[key] = true
			seen := map[int]bool{}
			for _, n := range numbers {
				if n < 1 || n > rule.Max || seen[n] {
					t.Fatal("illegal random", numbers)
				}
				seen[n] = true
			}
		}
		values := []int{1, 2, 3, 4, 5, 6, 49}
		if g.IsLegacySydney(at) {
			values = []int{1, 2, 3, 4, 5, 6, 23, 45}
		}
		preview, err := db.ManualPreview(g, source.Issue, values, previews[0].Current.Numbers, now)
		if err != nil {
			t.Fatal(err)
		}
		adopted, err := db.ApplyPreview(g, source.Issue, preview.Candidate.ID, "admin", now)
		if err != nil || !reflect.DeepEqual(adopted.Numbers, values) {
			t.Fatal("manual adoption failed", err)
		}
		if !g.IsLegacySydney(at) {
			for _, bad := range [][]int{{1, 2, 3, 4, 5, 6, 7, 8}, {1, 2, 3, 4, 5, 6, 50}, {1, 2, 3, 4, 5, 6, 6}} {
				if _, err = db.ManualPreview(g, source.Issue, bad, values, now); err == nil {
					t.Fatal("bad new-format input accepted", bad)
				}
			}
		}
	}
}

func TestSydney49StoredCutoverCannotChangeAndInvalidOverrideCannotPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := games.Find("lhc-au")
	d := draftSource(games.Sydney49Start())
	round, err := games.Project(g, d, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	bad := round
	bad.Numbers = []int{1, 2, 3, 4, 5, 6, 7, 8}
	if err = db.db.Update(func(tx *bolt.Tx) error {
		b, e := tx.CreateBucketIfNotExists(drawOverrides)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(drawOverride{Round: bad, Version: 1})
		if e != nil {
			return e
		}
		return b.Put(drawKey(g.ID, d.Issue), raw)
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.CommitLive([]games.Round{round}, d.Issue); err == nil {
		t.Fatal("invalid 8-ball override published into new issue")
	}
	if _, err = db.Get(g.ID, d.Issue); err != ErrNotFound {
		t.Fatal("failed publication left a result", err)
	}
	if err = db.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(metadata).Put(sydney49StartKey, []byte("2026-10-17T00:00:05+08:00"))
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("stored cutover was silently moved")
	}
}

func TestSydney49OpenPreservesAllHistoryWithoutOldMigrationMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := games.Find("lhc-au")
	at := games.Sydney49Start().Add(-time.Hour)
	source := draftSource(at)
	historical, err := games.Project(g, source, "simulated_history")
	if err != nil {
		t.Fatal(err)
	}
	// Include pre-45 seven-ball seed data and 45-number eight-ball data.
	// A missing obsolete migration marker must not authorize rewriting either.
	historical.Numbers = historical.Numbers[:7]
	historical.Positions = historical.Positions[:7]
	historical.DisplayNumbers = historical.DisplayNumbers[:7]
	secondSource := draftSource(at.Add(15 * time.Minute))
	second, err := games.Project(g, secondSource, "simulated_history")
	if err != nil {
		t.Fatal(err)
	}
	real := second
	real.Issue = at.Add(30 * time.Minute).Format(model.IssueLayout)
	real.SourceIssue = real.Issue
	real.DrawAt = at.Add(30 * time.Minute)
	real.Origin = "scheduled"
	if err = db.SaveMany([]games.Round{historical, second, real}); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveSeedSource(source); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	if err = db.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(metadata).Delete([]byte("au45-history-v2")); err != nil {
			return err
		}
		return tx.Bucket(rounds).Bucket([]byte(g.ID)).ForEach(func(k, v []byte) error { before[string(k)] = bytes.Clone(v); return nil })
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("au49_simulated_history_archive")) != nil {
			t.Fatal("retired history rewrite ran during new startup")
		}
		b := tx.Bucket(rounds).Bucket([]byte(g.ID))
		if b.Stats().KeyN != len(before) {
			t.Fatal("history count changed")
		}
		for issue, raw := range before {
			if !bytes.Equal(raw, b.Get([]byte(issue))) {
				t.Fatal("historical result rewritten", issue)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
