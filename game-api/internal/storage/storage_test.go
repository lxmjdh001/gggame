package storage

import (
	"errors"
	"path/filepath"
	"reflect"
	"starlight/gameapi/internal/generator"
	"starlight/gameapi/pkg/model"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPersistenceIdempotencyAndPagination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draws.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-10 * time.Minute).Truncate(time.Minute).Add(time.Second)
	var draws []model.Draw
	for i := 0; i < 3; i++ {
		d, e := generator.New().Generate(at.Add(time.Duration(i*2) * time.Minute))
		if e != nil {
			t.Fatal(e)
		}
		draws = append(draws, d)
		if e = s.Save(d); e != nil {
			t.Fatal(e)
		}
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	d, err := generator.New().Generate(at.Add(6 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Save(d)
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, ErrExists) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("duplicate write succeeded: %d", success.Load())
	}
	page, err := s.List(2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Issue != d.Issue || page.NextBefore != draws[2].Issue {
		t.Fatalf("wrong first page: %+v", page)
	}
	next, err := s.List(2, page.NextBefore)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 2 || next.Items[0].Issue != draws[1].Issue || next.NextBefore != "" {
		t.Fatalf("wrong second page: %+v", next)
	}
	gap, err := s.List(10, at.Add(3*time.Minute).In(model.Beijing).Format(model.IssueLayout))
	if err != nil || len(gap.Items) != 2 {
		t.Fatalf("gap cursor failed: %+v %v", gap, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	actual, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if actual.Issue != d.Issue || !reflect.DeepEqual(actual.Groups, d.Groups) {
		t.Fatal("restart changed draw")
	}
	// Immutable issue key: a different valid draw cannot replace the first result.
	changed := d
	changed.Groups = map[string][]int{}
	for k, v := range d.Groups {
		changed.Groups[k] = append([]int(nil), v...)
	}
	changed.Groups["g1"][0] = (changed.Groups["g1"][0] + 1) % 10
	if !errors.Is(s.Save(changed), ErrExists) {
		t.Fatal("overwrite was accepted")
	}
	saved, err := s.Get(d.Issue)
	if err != nil || !reflect.DeepEqual(saved.Groups, d.Groups) {
		t.Fatal("immutable result changed")
	}
}
