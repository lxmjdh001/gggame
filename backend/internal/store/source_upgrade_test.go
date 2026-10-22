package store

import (
	"path/filepath"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestCacheExtensionPreservesMacauAndRejectsRewrites(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 10, 13, 21, 30, 5, 0, model.Beijing)
	old := draftSource(at)
	old.SchemaVersion = 4
	for _, group := range []string{"g9", "g10", "g11"} {
		delete(old.Groups, group)
	}
	model.Display(&old)
	if err = db.CacheSources([]model.Draw{old}); err != nil {
		t.Fatal(err)
	}
	macau, _ := games.Find("lhc-daily")
	before, err := db.PreviewRound(macau, at)
	if err != nil {
		t.Fatal(err)
	}
	upgraded := draftSource(at)
	upgraded.GroupGeneratedAt = map[string]time.Time{}
	for _, group := range []string{"g9", "g10", "g11"} {
		upgraded.GroupGeneratedAt[group] = at.Add(-time.Minute)
	}
	needs, err := db.SourceNeedsUpgrade(upgraded)
	if err != nil || !needs {
		t.Fatal("upgrade not detected", err)
	}
	bad := draftSource(at)
	bad.GroupGeneratedAt = upgraded.GroupGeneratedAt
	bad.Groups["g7"][0], bad.Groups["g7"][1] = bad.Groups["g7"][1], bad.Groups["g7"][0]
	if err = db.CacheSources([]model.Draw{bad}); err == nil {
		t.Fatal("existing numbers rewritten")
	}
	if err = db.CacheSources([]model.Draw{upgraded}); err != nil {
		t.Fatal(err)
	}
	after, err := db.PreviewRound(macau, at)
	if err != nil || !reflect.DeepEqual(before.Numbers, after.Numbers) || before.SourceGroup != after.SourceGroup || before.Issue != after.Issue || !before.DrawAt.Equal(after.DrawAt) {
		t.Fatal("Macau changed", err)
	}
	needs, err = db.SourceNeedsUpgrade(upgraded)
	if err != nil || needs {
		t.Fatal("upgrade repeats", err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" || !g.Matches(at) {
			continue
		}
		r, err := db.PreviewRound(g, at)
		if err != nil || r == nil || len(r.Numbers) != g.NumberRuleAt(at).Count {
			t.Fatal(g.ID, "missing preview", err)
		}
	}
	if err = db.CacheSources([]model.Draw{bad}); err == nil {
		t.Fatal("post-upgrade rewrite accepted")
	}
}

func TestMarkSixRegionalAdoptionIsolation(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 10, 13, 21, 29, 20, 0, model.Beijing)
	hk, _ := games.Find("lhc-hk")
	at := hk.Next(now)
	d := draftSource(at)
	if err = db.CacheSources([]model.Draw{d}); err != nil {
		t.Fatal(err)
	}
	before := map[string][]int{}
	for _, g := range games.All() {
		if g.GameID == "lhc" && g.Matches(at) {
			r, err := db.PreviewRound(g, at)
			if err != nil {
				t.Fatal(err)
			}
			before[g.ID] = r.Numbers
		}
	}
	drafts, err := db.RandomPreviews(hk, d.Issue, nil, 5, now)
	if err != nil || len(drafts) != 5 {
		t.Fatal(err)
	}
	adopted, err := db.ApplyPreview(hk, d.Issue, drafts[2].Candidate.ID, "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	rounds := []games.Round{}
	for _, g := range games.All() {
		if g.GameID != "lhc" || !g.Matches(at) {
			continue
		}
		r, err := db.PreviewRound(g, at)
		if err != nil {
			t.Fatal(err)
		}
		want := before[g.ID]
		if g.ID == hk.ID {
			want = adopted.Numbers
		}
		if !reflect.DeepEqual(r.Numbers, want) {
			t.Fatal("cross-region modification", g.ID)
		}
		projected, err := games.Project(g, d, "scheduled")
		if err != nil {
			t.Fatal(err)
		}
		rounds = append(rounds, projected)
	}
	if err = db.CommitLive(rounds, d.Issue); err != nil {
		t.Fatal(err)
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" || !g.Matches(at) {
			continue
		}
		r, err := db.Get(g.ID, d.Issue)
		if err != nil {
			t.Fatal(err)
		}
		want := before[g.ID]
		if g.ID == hk.ID {
			want = adopted.Numbers
		}
		if !reflect.DeepEqual(r.Numbers, want) {
			t.Fatal("publication mismatch", g.ID)
		}
	}
}
