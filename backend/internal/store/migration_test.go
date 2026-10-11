package store

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestDiceHistoryNamespaceAndSeedExtension(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "history.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now().Add(-time.Hour).In(model.Beijing).Truncate(time.Minute).Add(time.Second)
	old := games.Round{SessionID: "k3-1", Issue: at.Format(model.IssueLayout), SourceGroup: "g3", Numbers: []int{7, 8, 9}}
	e = s.db.Update(func(tx *bolt.Tx) error {
		b, e := tx.Bucket(rounds).CreateBucketIfNotExists([]byte("k3-1"))
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(old)
		if e = b.Put([]byte(old.Issue), raw); e != nil {
			return e
		}
		return tx.Bucket(metadata).Put([]byte("seed:k3-1"), []byte("100"))
	})
	if e != nil {
		t.Fatal(e)
	}
	done, e := s.Seeded("k3-1")
	if e != nil || done {
		t.Fatal("old seed marker reused")
	}
	next := old
	next.SourceGroup = "g8"
	next.Numbers = []int{1, 2, 3}
	if e = s.CommitSeed("k3-1", []games.Round{next}); e != nil {
		t.Fatal(e)
	}
	actual, e := s.Get("k3-1", old.Issue)
	if e != nil || !reflect.DeepEqual(actual.Numbers, next.Numbers) {
		t.Fatal("new dice history wrong")
	}
	e = s.db.View(func(tx *bolt.Tx) error {
		var original games.Round
		raw := tx.Bucket(rounds).Bucket([]byte("k3-1")).Get([]byte(old.Issue))
		if e := json.Unmarshal(raw, &original); e != nil {
			return e
		}
		if !reflect.DeepEqual(original.Numbers, old.Numbers) {
			t.Fatal("legacy history changed")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	d := model.Draw{SchemaVersion: 2, Issue: old.Issue, Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: time.Now(), Groups: map[string][]int{}}
	for _, r := range model.Rules()[:7] {
		v := make([]int, r.Count)
		for i := range v {
			v[i] = r.Min + i%(r.Max-r.Min+1)
		}
		d.Groups[r.ID] = v
	}
	if e = s.SaveSeedSource(d); e != nil {
		t.Fatal(e)
	}
	extra := d
	extra.SchemaVersion = 3
	extra.Groups = map[string][]int{}
	for k, v := range d.Groups {
		extra.Groups[k] = append([]int{}, v...)
	}
	extra.Groups["g1"][0] = 9
	extra.Groups["g8"] = []int{1, 2, 3, 4, 5, 6, 1, 2, 3, 4}
	upgraded, e := s.ExtendSeedSource(extra)
	if e != nil {
		t.Fatal(e)
	}
	if upgraded.Groups["g1"][0] != d.Groups["g1"][0] || len(upgraded.Groups["g8"]) != 10 {
		t.Fatal("source extension rewrote existing group")
	}
}
