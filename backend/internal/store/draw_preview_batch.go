package store

import (
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"time"
)

const maxUniqueDrawAttempts = 64

// Keep adoption out while collecting a comparison so all candidates refer to
// the same effective draw version. No candidate changes the actual draw.
func (s *Store) RandomPreviews(g games.Session, issue string, ids []string, count int, now time.Time) ([]DrawPreview, error) {
	return s.randomPreviews(g, issue, ids, count, now, sampleNumbers)
}

func (s *Store) randomPreviews(g games.Session, issue string, ids []string, count int, now time.Time, sample drawSampler) ([]DrawPreview, error) {
	if count != 0 && count != 1 && count != 5 {
		return nil, errors.New("随机次数只能为1或5")
	}
	if len(ids) > 5 || count > 0 && len(ids) > 0 {
		return nil, errors.New("随机生成与已有预览不能混用，最多比较5组")
	}
	if count > 0 {
		ids = make([]string, count)
	} else if len(ids) == 0 {
		ids = []string{""}
	} else {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				return nil, errors.New("预览编号为空或重复")
			}
			seen[id] = true
		}
	}
	s.drawMu.RLock()
	defer s.drawMu.RUnlock()
	out := make([]DrawPreview, 0, len(ids))
	if count == 5 {
		seen := make(map[string]bool, count)
		draw := sample
		sample = func(g games.Session, base games.Round) (games.Round, error) {
			// Uniqueness applies to the ordered result, not to dice within a result.
			// Collisions stay in memory and never create discarded candidates.
			for attempt := 0; attempt < maxUniqueDrawAttempts; attempt++ {
				round, err := draw(g, base)
				if err != nil {
					return games.Round{}, err
				}
				key := fmt.Sprint(round.Numbers)
				if !seen[key] {
					seen[key] = true
					return round, nil
				}
			}
			return games.Round{}, errors.New("未能生成5组不同号码，请重试")
		}
	}
	operation := func(tx *bolt.Tx) error {
		for _, id := range ids {
			value, err := drawPreviewTx(tx, g, issue, id, count > 0, nil, nil, now, sample)
			if err != nil {
				return err
			}
			out = append(out, value)
		}
		return nil
	}
	var err error
	if count > 0 {
		err = s.db.Update(operation)
	} else {
		err = s.db.View(operation)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
