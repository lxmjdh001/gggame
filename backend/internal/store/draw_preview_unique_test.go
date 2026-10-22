package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func uniquePreviewFixture(t *testing.T) (*Store, games.Session, string, time.Time) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	g, _ := games.Find("k3-1")
	at := time.Date(2026, 10, 16, 12, 0, games.DrawSecond, 0, model.Beijing)
	d := draftSource(at)
	if err := db.CacheSources([]model.Draw{d}); err != nil {
		t.Fatal(err)
	}
	return db, g, d.Issue, at.Add(-30 * time.Second)
}

func candidateCount(t *testing.T, db *Store) int {
	t.Helper()
	count := 0
	if err := db.db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(drawCandidates); b != nil {
			return b.ForEach(func(_, _ []byte) error { count++; return nil })
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func candidateJSON(t *testing.T, candidate *DrawCandidate) string {
	t.Helper()
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRandomFiveRedrawsOnlyDuplicateOrderedResults(t *testing.T) {
	db, g, issue, now := uniquePreviewFixture(t)
	draws := [][]int{
		{1, 1, 1}, {1, 1, 1}, // The collision must not persist another candidate.
		{1, 1, 2}, {1, 2, 1}, {2, 1, 1}, // Order remains significant.
		{6, 6, 6}, // Repeated dice within one result remain legal.
	}
	calls := 0
	sample := func(g games.Session, base games.Round) (games.Round, error) {
		if calls == len(draws) {
			t.Fatal("unexpected additional random draw")
		}
		numbers := draws[calls]
		calls++
		return setNumbers(g, base, numbers)
	}
	previews, err := db.randomPreviews(g, issue, nil, 5, now, sample)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 6 || len(previews) != 5 || candidateCount(t, db) != 5 {
		t.Fatal("collision created extra candidates or lost a distinct result", calls, len(previews))
	}
	want := [][]int{draws[0], draws[2], draws[3], draws[4], draws[5]}
	ids := make([]string, len(previews))
	for i, preview := range previews {
		if !reflect.DeepEqual(preview.Candidate.Round.Numbers, want[i]) {
			t.Fatal("ordered tuple or repeated dice changed", preview.Candidate.Round.Numbers, want[i])
		}
		ids[i] = preview.Candidate.ID
	}
	refresh, err := db.randomPreviews(g, issue, ids, 0, now, func(games.Session, games.Round) (games.Round, error) {
		t.Fatal("refresh must not reroll candidates")
		return games.Round{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range previews {
		if candidateJSON(t, refresh[i].Candidate) != candidateJSON(t, previews[i].Candidate) {
			t.Fatal("refresh changed a saved candidate")
		}
	}
}

func TestRandomFiveFailureIsBoundedAndRollsBackDrafts(t *testing.T) {
	for _, failure := range []string{"collisions", "random source error"} {
		t.Run(failure, func(t *testing.T) {
			db, g, issue, now := uniquePreviewFixture(t)
			existing, err := db.RandomPreview(g, issue, "", true, now)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			sourceError := errors.New("random source unavailable")
			_, err = db.randomPreviews(g, issue, nil, 5, now, func(g games.Session, base games.Round) (games.Round, error) {
				calls++
				if failure == "random source error" {
					if calls == 3 {
						return games.Round{}, sourceError
					}
					return setNumbers(g, base, []int{calls, calls, calls})
				}
				return setNumbers(g, base, []int{1, 1, 1})
			})
			if err == nil {
				t.Fatal("failed five-way generation unexpectedly succeeded")
			}
			if failure == "collisions" && calls != 1+maxUniqueDrawAttempts {
				t.Fatal("collision retries were not bounded", calls)
			}
			if failure == "random source error" && (!errors.Is(err, sourceError) || calls != 3) {
				t.Fatal("random error was not propagated immediately", err, calls)
			}
			if candidateCount(t, db) != 1 {
				t.Fatal("failed batch left partial candidates")
			}
			refresh, err := db.RandomPreviews(g, issue, []string{existing.Candidate.ID}, 0, now)
			if err != nil || candidateJSON(t, refresh[0].Candidate) != candidateJSON(t, existing.Candidate) {
				t.Fatal("failed batch changed an existing candidate", err)
			}
			if _, err := db.Get(g.ID, issue); err != ErrNotFound {
				t.Fatal("failed batch published a result")
			}
		})
	}
}

func TestSingleRandomAndLegacyDuplicateRefreshRemainUnchanged(t *testing.T) {
	db, g, issue, now := uniquePreviewFixture(t)
	calls := 0
	sameAsCurrent := func(_ games.Session, base games.Round) (games.Round, error) {
		calls++
		return base, nil
	}
	ids := make([]string, 2)
	for i := range ids {
		preview, err := db.randomPreviews(g, issue, nil, 1, now, sameAsCurrent)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(preview[0].Current.Numbers, preview[0].Candidate.Round.Numbers) {
			t.Fatal("single random unexpectedly excluded the current numbers")
		}
		ids[i] = preview[0].Candidate.ID
	}
	if calls != 2 || ids[0] == ids[1] {
		t.Fatal("single random retried or reused a candidate ID")
	}
	refresh, err := db.randomPreviews(g, issue, ids, 0, now, func(games.Session, games.Round) (games.Round, error) {
		t.Fatal("existing duplicate candidates must not be rerolled")
		return games.Round{}, nil
	})
	if err != nil || len(refresh) != 2 {
		t.Fatal("legacy candidates with matching numbers were rejected", err)
	}
	if !reflect.DeepEqual(refresh[0].Candidate.Round.Numbers, refresh[1].Candidate.Round.Numbers) {
		t.Fatal("legacy duplicate numbers changed")
	}
}

func TestAllMarkSixAndK3RandomFiveResultsAreLegalAndDistinct(t *testing.T) {
	db, _, issue, now := uniquePreviewFixture(t)
	markSixCount, k3Count := 0, 0
	for _, g := range games.All() {
		if g.GameID != "lhc" && g.GameID != "k3" {
			continue
		}
		if g.GameID == "lhc" {
			markSixCount++
		} else {
			k3Count++
		}
		t.Run(g.ID, func(t *testing.T) {
			previews, err := db.RandomPreviews(g, issue, nil, 5, now)
			if err != nil || len(previews) != 5 {
				t.Fatal("five-way random generation failed", err)
			}
			seenResults, seenIDs := map[string]bool{}, map[string]bool{}
			rule := g.NumberRuleAt(g.Next(now))
			for _, preview := range previews {
				candidate := preview.Candidate
				result := candidate.Round
				key := fmt.Sprint(result.Numbers)
				if seenResults[key] || seenIDs[candidate.ID] {
					t.Fatal("duplicate result or candidate ID", key)
				}
				seenResults[key], seenIDs[candidate.ID] = true, true
				if result.SessionID != g.ID || result.Issue != issue || !result.DrawAt.Equal(g.Next(now)) || len(result.Numbers) != rule.Count {
					t.Fatal("candidate session, issue, time or length mismatch", result)
				}
				seenNumbers, sum := map[int]bool{}, 0
				for _, number := range result.Numbers {
					if number < rule.Min || number > rule.Max || !rule.AllowRepeat && seenNumbers[number] {
						t.Fatal("illegal random numbers", result.Numbers)
					}
					seenNumbers[number] = true
					sum += number
				}
				if g.GameID == "k3" && (result.Sum == nil || *result.Sum != sum) {
					t.Fatal("incorrect dice sum", result)
				}
			}
			current, err := db.PreviewRound(g, g.Next(now))
			if err != nil || !reflect.DeepEqual(current.Numbers, previews[0].Current.Numbers) {
				t.Fatal("random drafts changed the effective draw", err)
			}
		})
	}
	if markSixCount != 8 || k3Count != 9 || candidateCount(t, db) != 17*5 {
		t.Fatal("regional coverage or saved candidate count mismatch", markSixCount, k3Count)
	}
}
