package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: lhc-migration-check before.db after.db")
	}
	before, e := bolt.Open(os.Args[1], 0400, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	must(e)
	defer before.Close()
	after, e := bolt.Open(os.Args[2], 0400, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	must(e)
	defer after.Close()
	sessions, newQuotes := 0, 0
	must(before.View(func(a *bolt.Tx) error {
		return after.View(func(b *bolt.Tx) error {
			return a.ForEach(func(name []byte, old *bolt.Bucket) error {
				next := b.Bucket(name)
				if next == nil {
					return fmt.Errorf("missing bucket %s", name)
				}
				if string(name) != "audit" && old.Sequence() != next.Sequence() {
					return fmt.Errorf("changed sequence %s", name)
				}
				return old.ForEach(func(k, v []byte) error {
					n := next.Get(k)
					if v == nil {
						return fmt.Errorf("unexpected nested bucket %s", name)
					}
					if bytes.Equal(v, n) {
						return nil
					}
					if string(name) != "market_quotes" || !strings.HasPrefix(string(k), "lhc-") {
						return fmt.Errorf("unexpected changed record in %s", name)
					}
					var x, y []map[string]any
					if e := json.Unmarshal(v, &x); e != nil {
						return e
					}
					if e := json.Unmarshal(n, &y); e != nil {
						return e
					}
					lookup := map[string]map[string]any{}
					for _, q := range y {
						id, _ := q["market_id"].(string)
						if _, found := lookup[id]; found {
							return fmt.Errorf("duplicate quote %s", id)
						}
						lookup[id] = q
					}
					for _, q := range x {
						id, _ := q["market_id"].(string)
						newQ := lookup[id]
						j1, _ := json.Marshal(q)
						j2, _ := json.Marshal(newQ)
						if !bytes.Equal(j1, j2) {
							return fmt.Errorf("existing quote changed %s/%s", k, id)
						}
						delete(lookup, id)
					}
					for id := range lookup {
						if !strings.HasPrefix(id, "lhc6.") {
							return fmt.Errorf("unexpected new quote %s", id)
						}
					}
					sessions++
					newQuotes += len(lookup)
					return nil
				})
			})
		})
	}))
	// Only the quote migration may append audit entries; all other record counts must match.
	must(after.View(func(b *bolt.Tx) error {
		return before.View(func(a *bolt.Tx) error {
			return b.ForEach(func(name []byte, next *bolt.Bucket) error {
				old := a.Bucket(name)
				if old == nil {
					return fmt.Errorf("new bucket %s", name)
				}
				return next.ForEach(func(k, v []byte) error {
					if old.Get(k) != nil {
						return nil
					}
					if string(name) != "audit" {
						return fmt.Errorf("unexpected added record in %s", name)
					}
					var event map[string]any
					if e := json.Unmarshal(v, &event); e != nil {
						return e
					}
					action, _ := event["action"].(string)
					if !strings.Contains(action, "odds") {
						return fmt.Errorf("unexpected audit action %s", action)
					}
					return nil
				})
			})
		})
	}))
	fmt.Printf("verified existing records and quotes unchanged; sessions=%d added_quotes=%d\n", sessions, newQuotes)
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
