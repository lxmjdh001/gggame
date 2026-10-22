package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestManualPreviewRulesAndAdoption(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 10, 11, 21, 29, 20, 0, model.Beijing)
	for _, g := range games.All() {
		t.Run(g.ID, func(t *testing.T) {
			at := g.Next(now)
			source := draftSource(at)
			if err := db.CacheSources([]model.Draw{source}); err != nil {
				t.Fatal(err)
			}
			before, err := db.PreviewRound(g, at)
			if err != nil {
				t.Fatal(err)
			}
			numbers := slices.Clone(before.Numbers)
			slices.Reverse(numbers)
			// A repeat-allowed source slice may be a palindrome. The stale-editor
			// assertion needs an actual number change, not merely a new version.
			if slices.Equal(numbers, before.Numbers) && g.NumberRuleAt(at).AllowRepeat {
				rule := g.NumberRuleAt(at)
				numbers[0] = rule.Min + (numbers[0]-rule.Min+1)%(rule.Max-rule.Min+1)
			}
			wrongRange := slices.Clone(numbers)
			wrongRange[0] = 100
			for _, bad := range [][]int{nil, {}, numbers[:len(numbers)-1], wrongRange} {
				if _, err := db.ManualPreview(g, source.Issue, bad, before.Numbers, now); err == nil {
					t.Fatalf("accepted invalid numbers %v", bad)
				}
			}
			if _, err := db.ManualPreview(g, source.Issue, numbers, nil, now); err == nil {
				t.Fatal("accepted missing original numbers")
			}
			repeat := slices.Clone(numbers)
			repeat[1] = repeat[0]
			_, err = db.ManualPreview(g, source.Issue, repeat, before.Numbers, now)
			repeatAllowed := g.Group == "g1" || g.Group == "g8"
			if (err == nil) != repeatAllowed {
				t.Fatalf("incorrect duplicate rule for %s: %v", g.ID, err)
			}
			draft, err := db.ManualPreview(g, source.Issue, numbers, before.Numbers, now)
			if err != nil {
				t.Fatal(err)
			}
			unchanged, _ := db.PreviewRound(g, at)
			if !reflect.DeepEqual(unchanged.Numbers, before.Numbers) {
				t.Fatal("draft changed effective draw")
			}
			if !reflect.DeepEqual(draft.Candidate.Round.Numbers, numbers) {
				t.Fatal("draft differs from entered numbers")
			}
			if g.Format == "sum" || g.Format == "triple" {
				sum := 0
				for _, n := range numbers {
					sum += n
				}
				if draft.Candidate.Round.Sum == nil || *draft.Candidate.Round.Sum != sum {
					t.Fatal("sum not rebuilt")
				}
			}
			if g.Format == "padded" || g.Format == "marksix" {
				for _, n := range draft.Candidate.Round.DisplayNumbers {
					if len(n) != 2 {
						t.Fatal("display padding lost")
					}
				}
			}
			if _, err := db.ManualPreview(g, source.Issue, numbers, before.Numbers, at.Truncate(time.Minute)); err == nil {
				t.Fatal("manual preview allowed at cutoff")
			}
			stale, err := db.ManualPreview(g, source.Issue, numbers, before.Numbers, now)
			if err != nil {
				t.Fatal(err)
			}
			result, err := db.ApplyPreview(g, source.Issue, draft.Candidate.ID, "admin", now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Numbers, numbers) {
				t.Fatal("adoption differs from entered numbers")
			}
			if _, err := db.ManualPreview(g, source.Issue, before.Numbers, before.Numbers, now); err == nil {
				t.Fatal("stale editor overwrote new numbers")
			}
			if _, err := db.ApplyPreview(g, source.Issue, stale.Candidate.ID, "admin", now); err == nil {
				t.Fatal("stale candidate adopted")
			}
			history, _ := db.History(g.ID, 1, "")
			if history.Total != 0 {
				t.Fatal("manual preview published early")
			}
			err = db.db.View(func(tx *bolt.Tx) error {
				var record struct {
					Actor         string
					Before, After games.Round
				}
				if err := json.Unmarshal(tx.Bucket(drawChangeLog).Get([]byte(draft.Candidate.ID)), &record); err != nil {
					return err
				}
				if record.Actor != "admin" || !reflect.DeepEqual(record.Before.Numbers, before.Numbers) || !reflect.DeepEqual(record.After.Numbers, numbers) {
					t.Fatal("manual change missing audit")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	// The editor must not invent source numbers for an unsynchronized issue.
	g, _ := games.Find("lhc-daily")
	tomorrow := now.AddDate(0, 0, 1)
	if _, err := db.ManualPreview(g, g.Next(tomorrow).Format(model.IssueLayout), []int{1, 2, 3, 4, 5, 6, 7}, []int{1, 2, 3, 4, 5, 6, 7}, tomorrow); err == nil {
		t.Fatal("missing source invented")
	}
}
