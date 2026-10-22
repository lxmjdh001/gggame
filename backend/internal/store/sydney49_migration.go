package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
)

var sydney49StartKey = []byte("sydney_49_seven_start_v1")
var sydney45UnpublishedArchive = []byte("sydney_45_unpublished_archive_v1")

// Keep the cutover immutable across restarts. The source cache, published
// history, adopted past results and change audit are never rewritten. Only
// incompatible private drafts/overrides for new-format issues are archived.
func migrateSydney49(tx *bolt.Tx) error {
	meta := tx.Bucket(metadata)
	expected := []byte(games.Sydney49Start().Format(time.RFC3339))
	if existing := meta.Get(sydney49StartKey); existing != nil && !bytes.Equal(existing, expected) {
		return errors.New("悉尼49选7切换期号与已存档配置不符")
	}
	g, _ := games.Find("lhc-au")
	for _, input := range []struct {
		bucket    []byte
		prefix    string
		candidate bool
	}{
		{drawCandidates, "candidate:", true}, {drawOverrides, "override:", false},
	} {
		b := tx.Bucket(input.bucket)
		if b == nil {
			continue
		}
		c := b.Cursor()
		for key, raw := c.First(); key != nil; key, raw = c.Next() {
			var r games.Round
			if input.candidate {
				var value DrawCandidate
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				r = value.Round
			} else {
				var value drawOverride
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				r = value.Round
			}
			if r.SessionID != "lhc-au" || g.IsLegacySydney(r.DrawAt) {
				continue
			}
			if _, err := setNumbers(g, r, r.Numbers); err == nil {
				continue
			}
			// Published history remains authoritative, even if a past release was
			// installed late. Its old override is retained as supporting evidence.
			if published := tx.Bucket(rounds).Bucket([]byte(sessionKey(r.SessionID))); published != nil && published.Get([]byte(r.Issue)) != nil {
				continue
			}
			archive, err := tx.CreateBucketIfNotExists(sydney45UnpublishedArchive)
			if err != nil {
				return err
			}
			archiveKey := append([]byte(input.prefix), key...)
			if archive.Get(archiveKey) == nil {
				if err = archive.Put(archiveKey, raw); err != nil {
					return err
				}
			}
			if err = c.Delete(); err != nil {
				return err
			}
		}
	}
	return meta.Put(sydney49StartKey, expected)
}
