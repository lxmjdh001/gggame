package store

import (
	"encoding/json"
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

var hongKongHistoryVersion = []byte("hongkong_simulated_48h_v1")

func (s *Store) HongKongHistoryUpdated() (bool, error) {
	updated := false
	err := s.db.View(func(tx *bolt.Tx) error { updated = tx.Bucket(metadata).Get(hongKongHistoryVersion) != nil; return nil })
	return updated, err
}

// Archive and replace only explicitly labelled simulation records, atomically.
// Existing published records win every collision and are never changed.
func (s *Store) ReplaceHongKongSimulatedHistory(items []games.Round, cutoff time.Time) error {
	if len(items) != 100 {
		return errors.New("expected 100 simulated Hong Kong history slots")
	}
	g, _ := games.Find("lhc-hk")
	seen := map[string]bool{}
	for _, r := range items {
		if r.SessionID != g.ID || r.Origin != "simulated_history" || !r.DrawAt.Before(cutoff) || !g.SimulatedDue(r.DrawAt).Equal(r.DrawAt) || r.Issue != r.DrawAt.In(model.Beijing).Format(model.IssueLayout) || r.SourceIssue != r.Issue || seen[r.Issue] {
			return errors.New("invalid simulated Hong Kong history slot")
		}
		seen[r.Issue] = true
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(metadata)
		if meta.Get(hongKongHistoryVersion) != nil {
			return nil
		}
		b, err := tx.Bucket(rounds).CreateBucketIfNotExists([]byte(g.ID))
		if err != nil {
			return err
		}
		archive, err := tx.CreateBucketIfNotExists([]byte("hongkong_daily_simulated_archive_v1"))
		if err != nil {
			return err
		}
		remove := []string{}
		if err = b.ForEach(func(k, raw []byte) error {
			var r games.Round
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			if r.Origin != "simulated_history" {
				return nil
			}
			if err := archive.Put(k, raw); err != nil {
				return err
			}
			remove = append(remove, string(k))
			return nil
		}); err != nil {
			return err
		}
		for _, k := range remove {
			if err = b.Delete([]byte(k)); err != nil {
				return err
			}
		}
		for _, r := range items {
			if err = put(tx, r); err != nil {
				return err
			}
		}
		if err = meta.Put([]byte("seed:"+g.ID), []byte("100")); err != nil {
			return err
		}
		return meta.Put(hongKongHistoryVersion, []byte(cutoff.Format(time.RFC3339Nano)))
	})
}
