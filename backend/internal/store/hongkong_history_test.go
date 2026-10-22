package store

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestHongKongMigrationArchivesAndRejectsInvalidSlots(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cutoff := games.HongKongScheduleStart().Add(-time.Hour)
	items := make([]games.Round, 100)
	for i := range items {
		at := games.HongKongScheduleStart().Add(-time.Duration(i+1) * 48 * time.Hour)
		issue := at.Format(model.IssueLayout)
		items[i] = games.Round{SessionID: "lhc-hk", Issue: issue, SourceIssue: issue, DrawAt: at, Origin: "simulated_history", Numbers: []int{1, 2, 3, 4, 5, 6, 7}}
	}
	old := items[0]
	old.DrawAt = old.DrawAt.Add(24 * time.Hour)
	old.Issue = old.DrawAt.Format(model.IssueLayout)
	old.SourceIssue = old.Issue
	if err = s.CommitSeed("lhc-hk", []games.Round{old}); err != nil {
		t.Fatal(err)
	}
	bad := append([]games.Round(nil), items...)
	bad[50].Origin = "scheduled"
	if s.ReplaceHongKongSimulatedHistory(bad, cutoff) == nil {
		t.Fatal("accepted actual draw as simulation")
	}
	if _, err = s.Get("lhc-hk", old.Issue); err != nil {
		t.Fatal("invalid migration removed old history")
	}
	if err = s.ReplaceHongKongSimulatedHistory(items, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("lhc-hk", old.Issue); err != ErrNotFound {
		t.Fatal("old daily simulation remains")
	}
	err = s.db.View(func(tx *bolt.Tx) error {
		want, _ := json.Marshal(old)
		got := tx.Bucket([]byte("hongkong_daily_simulated_archive_v1")).Get([]byte(old.Issue))
		if !bytes.Equal(got, want) {
			t.Fatal("archive changed original record")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceHongKongSimulatedHistory(items, cutoff); err != nil {
		t.Fatal(err)
	}
	h, err := s.History("lhc-hk", 100, "")
	if err != nil || h.Total != 100 {
		t.Fatal("non-idempotent migration", err)
	}
}
