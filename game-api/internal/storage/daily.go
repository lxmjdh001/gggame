package storage

import (
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
	"time"
)

var days = []byte("completed_days_v1")

// One write transaction prevents incomplete days or racing generators from replacing numbers.
// Existing legacy minute batches are preserved when first enabling daily generation.
func (s *Store) PrepareDay(start time.Time, generate func(time.Time) (model.Draw, error)) error {
	start = start.In(model.Beijing)
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 {
		return fmt.Errorf("expected Beijing midnight")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		dayBucket, err := tx.CreateBucketIfNotExists(days)
		if err != nil {
			return err
		}
		date := []byte(start.Format("2006-01-02"))
		complete := dayBucket.Get(date) != nil
		b := tx.Bucket(bucket)
		for i := 0; i < 1440; i++ {
			at := start.Add(time.Duration(i)*time.Minute + time.Second)
			issue := at.Format(model.IssueLayout)
			if raw := b.Get([]byte(issue)); raw != nil {
				if _, err := decode(raw); err != nil {
					return err
				}
				continue
			}
			if complete {
				return fmt.Errorf("completed day has missing issue %s; restore original data", issue)
			}
			d, err := generate(at)
			if err != nil {
				return err
			}
			if err = model.Validate(d); err != nil {
				return err
			}
			if d.Issue != issue {
				return fmt.Errorf("generator returned wrong issue")
			}
			raw, err := json.Marshal(d)
			if err != nil {
				return err
			}
			if err = b.Put([]byte(issue), raw); err != nil {
				return err
			}
		}
		return dayBucket.Put(date, []byte("1440"))
	})
}
