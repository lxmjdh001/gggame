package storage

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/gameapi/internal/generator"
	"starlight/gameapi/pkg/model"
)

func TestSupplementalGroupsAtomicPreserveAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, model.Beijing)
	g := generator.New()
	old := map[string]model.Draw{}
	legacy := func(at time.Time) (model.Draw, error) {
		d, err := g.Generate(at)
		if err != nil {
			return d, err
		}
		d.SchemaVersion = 4
		for _, id := range []string{"g9", "g10", "g11"} {
			delete(d.Groups, id)
		}
		model.Display(&d)
		old[d.Issue] = d
		return d, nil
	}
	if err = s.PrepareDay(start, legacy); err != nil {
		t.Fatal(err)
	}
	previous, err := legacy(start.Add(-time.Minute + time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(previous); err != nil {
		t.Fatal(err)
	}
	for issue := range old {
		old[issue], err = s.Get(issue)
		if err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	if err = s.PrepareDay(start, func(at time.Time) (model.Draw, error) {
		calls++
		if calls == 100 {
			return model.Draw{}, errors.New("entropy failure")
		}
		return g.Generate(at)
	}); err == nil {
		t.Fatal("supplemental generation failure lost")
	}
	for issue, want := range old {
		got, err := s.Get(issue)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("partial upgrade changed existing batch", issue, err)
		}
	}
	if err = s.PrepareDay(start, g.Generate); err != nil {
		t.Fatal(err)
	}
	for issue, want := range old {
		got, err := s.Get(issue)
		if err != nil {
			t.Fatal(err)
		}
		if issue == previous.Issue {
			if !reflect.DeepEqual(got, want) {
				t.Fatal("historical day changed")
			}
			continue
		}
		if got.SchemaVersion != 5 || len(got.Groups) != 11 || !got.GeneratedAt.Equal(want.GeneratedAt) || !got.ScheduledAt.Equal(want.ScheduledAt) {
			t.Fatal("bad upgraded metadata")
		}
		for id, numbers := range want.Groups {
			if !reflect.DeepEqual(got.Groups[id], numbers) {
				t.Fatal("existing numbers changed", id)
			}
		}
		for _, id := range []string{"g9", "g10", "g11"} {
			selection, err := model.Select(got, id, []int{1, 2, 3, 4, 5, 6, 7})
			if err != nil || !selection.GeneratedAt.Equal(got.GroupGeneratedAt[id]) || selection.GeneratedAt.Before(want.GeneratedAt) {
				t.Fatal("supplement timestamp incorrect", id, err)
			}
		}
	}
	first, _ := s.Get(start.Format(model.IssueLayout))
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PrepareDay(start, func(time.Time) (model.Draw, error) { t.Fatal("regenerated after restart"); return model.Draw{}, nil }); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get(first.Issue)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("restart changed new groups")
	}
}
