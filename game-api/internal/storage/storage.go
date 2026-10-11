package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"starlight/gameapi/pkg/model"
	"time"
)

var ErrNotFound = errors.New("draw not found")
var ErrExists = errors.New("draw already exists")
var bucket = []byte("draws_v1")

type Store struct{ db *bolt.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open database (only one service may own this file): %w", err)
	}
	if err = db.Update(func(tx *bolt.Tx) error { _, err := tx.CreateBucketIfNotExists(bucket); return err }); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func decode(raw []byte) (model.Draw, error) {
	var d model.Draw
	if raw == nil {
		return d, ErrNotFound
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	model.Display(&d)
	return d, model.Validate(d)
}
func (s *Store) Save(d model.Draw) error {
	if err := model.Validate(d); err != nil {
		return err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b.Get([]byte(d.Issue)) != nil {
			return ErrExists
		}
		return b.Put([]byte(d.Issue), raw)
	})
}
func (s *Store) Get(issue string) (model.Draw, error) {
	var d model.Draw
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		d, err = decode(tx.Bucket(bucket).Get([]byte(issue)))
		return err
	})
	return d, err
}
func (s *Store) Latest() (model.Draw, error) {
	var d model.Draw
	err := s.db.View(func(tx *bolt.Tx) error {
		_, raw := tx.Bucket(bucket).Cursor().Last()
		var err error
		d, err = decode(raw)
		return err
	})
	return d, err
}
func (s *Store) List(limit int, before string) (model.Page, error) {
	page := model.Page{Items: []model.Draw{}}
	if limit < 1 || limit > 100 {
		return page, fmt.Errorf("limit must be between 1 and 100")
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		k, v := c.Last()
		if before != "" {
			k, v = c.Seek([]byte(before))
			if k == nil {
				k, v = c.Last()
			} else {
				k, v = c.Prev()
			}
		}
		for k != nil && len(page.Items) < limit {
			d, err := decode(v)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, d)
			k, v = c.Prev()
		}
		if k != nil && len(page.Items) > 0 {
			page.NextBefore = page.Items[len(page.Items)-1].Issue
		}
		return nil
	})
	return page, err
}
