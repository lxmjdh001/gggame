package storage

import (
	"errors"
	"path/filepath"
	"reflect"
	"starlight/gameapi/internal/generator"
	"starlight/gameapi/pkg/model"
	"sync"
	"testing"
	"time"
)

func TestDailyAtomicImmutableRestartAndConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daily.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2027, 1, 1, 0, 0, 0, 0, model.Beijing)
	g := generator.New()
	calls := 0
	failure := func(at time.Time) (model.Draw, error) {
		calls++
		if calls == 100 {
			return model.Draw{}, errors.New("entropy failure")
		}
		return g.Generate(at)
	}
	if err = s.PrepareDay(at, failure); err == nil {
		t.Fatal("failure lost")
	}
	if _, err = s.Latest(); !errors.Is(err, ErrNotFound) {
		t.Fatal("partial batch escaped transaction", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.PrepareDay(at, g.Generate); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	first, err := s.Get("202701010000")
	if err != nil {
		t.Fatal(err)
	}
	last, err := s.Get("202701012359")
	if err != nil {
		t.Fatal(err)
	}
	if first.GeneratedAt.After(first.ScheduledAt) || last.SchemaVersion != model.SchemaVersion {
		t.Fatal("not pre-generated")
	}
	count := 0
	before := ""
	for {
		page, err := s.List(100, before)
		if err != nil {
			t.Fatal(err)
		}
		count += len(page.Items)
		if page.NextBefore == "" {
			break
		}
		before = page.NextBefore
	}
	if count != 1440 {
		t.Fatal(count)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fail := func(time.Time) (model.Draw, error) {
		t.Error("regenerated existing day")
		return model.Draw{}, errors.New("unexpected")
	}
	if err = s.PrepareDay(at, fail); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get(first.Issue)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("restart changed numbers")
	}
	if err = s.PrepareDay(at.AddDate(0, 0, 1), g.Generate); err != nil {
		t.Fatal(err)
	}
	again, _ = s.Get(first.Issue)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("next day changed old numbers")
	}
}

func TestDailyPreservesLegacyBatch(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, model.Beijing)
	g := generator.New()
	d, err := g.Generate(at.Add(10*time.Hour + time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d.SchemaVersion = 3
	for _, group := range []string{"g9", "g10", "g11"} {
		delete(d.Groups, group)
	}
	if err = s.Save(d); err != nil {
		t.Fatal(err)
	}
	before, err := s.Get(d.Issue)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PrepareDay(at, g.Generate); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Get(d.Issue)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("legacy source replaced")
	}
}
