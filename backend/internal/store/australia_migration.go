package store

import (
	"encoding/json"
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

// Only simulated history may be reprojected. Published real draws remain intact.
// An already adopted future result requires an explicit operator decision.
func migrateAustraliaHistory(tx *bolt.Tx) error {
	meta := tx.Bucket(metadata)
	key := []byte("au45-history-v2")
	if meta.Get(key) != nil {
		return nil
	}
	if b := tx.Bucket(drawOverrides); b != nil {
		if err := b.ForEach(func(_, raw []byte) error {
			var v drawOverride
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			if v.Round.SessionID == "lhc-au" && len(v.Round.Numbers) != 8 && v.Round.DrawAt.Before(games.Sydney49Start()) && v.Round.DrawAt.After(time.Now()) {
				return errors.New("澳大利亚本期已采用旧规则号码，需处理后再升级45选8")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	b := tx.Bucket(rounds).Bucket([]byte("lhc-au"))
	if b != nil {
		archive, err := tx.CreateBucketIfNotExists([]byte("au49_simulated_history_archive"))
		if err != nil {
			return err
		}
		g, _ := games.Find("lhc-au")
		replacements := map[string][]byte{}
		if err := b.ForEach(func(k, raw []byte) error {
			var r games.Round
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			if r.Origin != "simulated_history" || len(r.Numbers) == 8 || !g.IsLegacySydney(r.DrawAt) {
				return nil
			}
			var d model.Draw
			if err := json.Unmarshal(tx.Bucket(seedSources).Get([]byte(r.SourceIssue)), &d); err != nil {
				return err
			}
			next, err := games.Project(g, d, "simulated_history")
			if err != nil {
				return err
			}
			next.PublishedAt = r.PublishedAt
			encoded, err := json.Marshal(next)
			if err != nil {
				return err
			}
			if err = archive.Put(k, raw); err != nil {
				return err
			}
			replacements[string(k)] = encoded
			return nil
		}); err != nil {
			return err
		}
		for k, raw := range replacements {
			if err := b.Put([]byte(k), raw); err != nil {
				return err
			}
		}
	}
	return meta.Put(key, []byte("done"))
}
