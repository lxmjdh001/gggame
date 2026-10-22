package store

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"sync"
	"testing"
	"time"
)

func draftSource(at time.Time) model.Draw {
	d := model.Draw{SchemaVersion: model.SchemaVersion, Issue: at.Format(model.IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at.Add(-games.SourcePublishDelay), GeneratedAt: at.Add(-time.Hour), Groups: map[string][]int{}}
	for _, rule := range model.Rules() {
		for i := 0; i < rule.Count; i++ {
			d.Groups[rule.ID] = append(d.Groups[rule.ID], rule.Min+i%(rule.Max-rule.Min+1))
		}
	}
	model.Display(&d)
	return d
}
func TestRandomPreviewAdoptionCutoffAndPublication(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, _ := games.Find("ffc-1")
	other, _ := games.Find("ffc-5")
	at := time.Date(2026, 10, 12, 12, 5, 5, 0, model.Beijing)
	before := at.Truncate(time.Minute).Add(-time.Nanosecond)
	source := draftSource(at)
	if err = db.CacheSources([]model.Draw{source}); err != nil {
		t.Fatal(err)
	}
	initial, err := db.PreviewRound(g, at)
	if err != nil {
		t.Fatal(err)
	}
	otherBefore, _ := db.PreviewRound(other, at)
	draft, err := db.RandomPreview(g, source.Issue, "", true, before)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := db.RandomPreview(g, source.Issue, "", true, before)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := db.PreviewRound(g, at)
	if !reflect.DeepEqual(initial.Numbers, current.Numbers) {
		t.Fatal("random preview changed actual numbers")
	}
	if _, err = db.ApplyPreview(other, source.Issue, draft.Candidate.ID, "admin", before); err == nil {
		t.Fatal("cross-session candidate accepted")
	}
	for _, now := range []time.Time{at.Truncate(time.Minute), at.Truncate(time.Minute).Add(time.Millisecond), at, at.Add(time.Minute)} {
		if _, err = db.RandomPreview(g, source.Issue, "", true, now); err == nil {
			t.Fatal("random allowed after cutoff")
		}
		if _, err = db.RandomPreview(g, source.Issue, draft.Candidate.ID, false, now); err == nil {
			t.Fatal("preview recalculation allowed after cutoff")
		}
		if _, err = db.ApplyPreview(g, source.Issue, draft.Candidate.ID, "admin", now); err == nil {
			t.Fatal("adoption allowed after cutoff")
		}
	}
	accepted, err := db.ApplyPreview(g, source.Issue, draft.Candidate.ID, "admin", before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ApplyPreview(g, source.Issue, stale.Candidate.ID, "admin", before); err == nil {
		t.Fatal("stale candidate accepted")
	}
	if _, err = db.ApplyPreview(g, source.Issue, draft.Candidate.ID, "admin", before); err == nil {
		t.Fatal("duplicate adoption was not rejected")
	}
	current, _ = db.PreviewRound(g, at)
	if !reflect.DeepEqual(accepted.Numbers, current.Numbers) {
		t.Fatal("accepted candidate missing from payout preview")
	}
	otherAfter, _ := db.PreviewRound(other, at)
	if !reflect.DeepEqual(otherBefore.Numbers, otherAfter.Numbers) {
		t.Fatal("another session changed")
	}
	if err = db.CacheSources([]model.Draw{source}); err != nil {
		t.Fatal("immutable source cache changed", err)
	}
	if _, err = db.Get(g.ID, source.Issue); err != ErrNotFound {
		t.Fatal("candidate published early")
	}
	original, err := games.Project(g, source, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	original.PublishedAt = at.Add(time.Second)
	if err = db.CommitLive([]games.Round{original}, source.Issue); err != nil {
		t.Fatal(err)
	}
	published, err := db.Get(g.ID, source.Issue)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(published.Numbers, accepted.Numbers) || !published.PublishedAt.Equal(original.PublishedAt) {
		t.Fatal("publication differs from accepted preview")
	}
	err = db.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(drawChangeLog).Get([]byte(draft.Candidate.ID))
		var record struct {
			Actor         string
			Before, After games.Round
		}
		if e := json.Unmarshal(raw, &record); e != nil {
			return e
		}
		if record.Actor != "admin" || !reflect.DeepEqual(record.After.Numbers, accepted.Numbers) || !reflect.DeepEqual(record.Before.Numbers, initial.Numbers) {
			t.Fatal("atomic audit missing")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRandomPreviewRulesAndConcurrentAdoption(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 10, 12, 21, 29, 59, 0, model.Beijing)
	for _, g := range games.All() {
		at := g.Next(now)
		source := draftSource(at)
		if err = db.CacheSources([]model.Draw{source}); err != nil {
			t.Fatal(err)
		}
		draft, err := db.RandomPreview(g, source.Issue, "", true, now)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int]bool{}
		var rule model.Rule
		for _, r := range model.Rules() {
			if r.ID == g.Group {
				rule = r
			}
		}
		for _, n := range draft.Candidate.Round.Numbers {
			if n < rule.Min || n > rule.Max || !rule.AllowRepeat && seen[n] {
				t.Fatalf("invalid random result: %+v", draft.Candidate.Round)
			}
			seen[n] = true
		}
		if len(draft.Candidate.Round.Numbers) != g.NumberRuleAt(draft.Candidate.Round.DrawAt).Count {
			t.Fatal("incorrect result length")
		}
		if g.Format == "padded" || g.Format == "marksix" {
			for _, n := range draft.Candidate.Round.DisplayNumbers {
				if len(n) != 2 {
					t.Fatal("padding missing")
				}
			}
		}
	}
	g, _ := games.Find("ffc-1")
	issue := g.Next(now).Format(model.IssueLayout)
	first, err := db.RandomPreview(g, issue, "", true, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.RandomPreview(g, issue, "", true, now)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{first.Candidate.ID, second.Candidate.ID} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, e := db.ApplyPreview(g, issue, id, "admin", now); errs <- e }(id)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("concurrent candidates overwrote each other", success)
	}
	// Tomorrow has not been generated yet; random must not invent a missing source.
	tomorrow := now.AddDate(0, 0, 1)
	if _, err = db.RandomPreview(g, g.Next(tomorrow).Format(model.IssueLayout), "", true, tomorrow); err == nil {
		t.Fatal("missing source fabricated")
	}
}
