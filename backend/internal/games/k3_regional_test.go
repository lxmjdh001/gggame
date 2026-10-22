package games

import (
	"reflect"
	"starlight/gameapi/pkg/model"
	"strings"
	"testing"
	"time"
)

func TestRegionalK3CatalogAndNumberRules(t *testing.T) {
	want := []struct {
		id, name  string
		minutes   int
		positions []int
	}{
		{"k3-1", "极速快三", 1, []int{1, 2, 3}},
		{"k3-3", "福彩快三", 3, []int{8, 9, 10}},
		{"k3-5", "幸运快三", 5, []int{4, 5, 6}},
		{"k3-zj", "浙江快三", 20, []int{1, 4, 7}},
		{"k3-fj", "福建快三", 20, []int{2, 5, 8}},
		{"k3-gd", "广东快三", 20, []int{3, 6, 9}},
		{"k3-hn", "湖南快三", 20, []int{4, 7, 10}},
		{"k3-tw", "台湾快三", 20, []int{1, 5, 9}},
		{"k3-gx", "广西快三", 20, []int{2, 6, 10}},
	}
	var got []Session
	for _, s := range All() {
		if s.GameID == "k3" {
			got = append(got, s)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d K3 sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		s := got[i]
		if s.ID != w.id || s.GameName != w.name || s.IntervalMinutes != w.minutes || s.OffsetMinutes != 0 || s.Group != "g8" || s.Format != "triple" || !reflect.DeepEqual(s.Positions, w.positions) {
			t.Fatalf("session %d: got %+v, want %+v", i, s, w)
		}
		rule := s.NumberRule()
		if rule.Min != 1 || rule.Max != 6 || rule.Count != 3 || !rule.AllowRepeat || s.MarketCatalogID() != "k3" {
			t.Fatalf("%s number or market rules changed: %+v", s.ID, rule)
		}
		if s.ClosedWindowSeconds() != 15 || !strings.Contains(s.Rule, "提前10秒截止") || !strings.Contains(s.Rule, "可重复") {
			t.Fatal(s.ID, "cutoff or dice repetition rule missing")
		}
	}
}

func TestRegionalK3ProjectionRetainsSourceAndRepeatedDice(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 1, 0, model.Beijing)
	for _, s := range All() {
		if s.GameID != "k3" {
			continue
		}
		for _, raw := range [][]int{
			{1, 2, 3, 4, 5, 6, 1, 2, 3, 4},
			{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			{6, 6, 6, 6, 6, 6, 6, 6, 6, 6},
		} {
			before := append([]int(nil), raw...)
			d := model.Draw{Issue: at.Format(model.IssueLayout), ScheduledAt: at, Groups: map[string][]int{"g8": raw}}
			r, err := Project(s, d, "scheduled")
			if err != nil {
				t.Fatal(s.ID, err)
			}
			expected, sum := make([]int, 3), 0
			for i, p := range s.Positions {
				expected[i] = raw[p-1]
				sum += expected[i]
			}
			if !reflect.DeepEqual(r.Numbers, expected) || r.Sum == nil || *r.Sum != sum {
				t.Fatal(s.ID, "dice or sum changed", r)
			}
			if r.SourceIssue != d.Issue || r.Issue != d.Issue || r.SourceGroup != "g8" || !reflect.DeepEqual(r.Positions, s.Positions) || !r.DrawAt.Equal(at.Add(SourcePublishDelay)) {
				t.Fatal(s.ID, "source provenance or publication time changed", r)
			}
			if !reflect.DeepEqual(d.Groups["g8"], before) {
				t.Fatal(s.ID, "projection mutated source")
			}
		}
	}
}

func TestRegionalK3ScheduleAndCutoffAcrossHistoricalAndFutureYears(t *testing.T) {
	for _, s := range All() {
		if s.GameID != "k3" {
			continue
		}
		interval := time.Duration(s.IntervalMinutes) * time.Minute
		for _, start := range []time.Time{
			time.Date(2025, 12, 30, 0, 0, DrawSecond, 0, model.Beijing),
			time.Date(2026, 10, 14, 0, 0, DrawSecond, 0, model.Beijing),
			time.Date(2026, 12, 30, 0, 0, DrawSecond, 0, model.Beijing),
		} {
			for at := start; at.Before(start.Add(72 * time.Hour)); at = at.Add(interval) {
				if !s.Due(at).Equal(at) || !s.Next(at).Equal(at.Add(interval)) || !s.Due(at.Add(-time.Nanosecond)).Equal(at.Add(-interval)) || !s.Next(at.Add(-time.Nanosecond)).Equal(at) {
					t.Fatal(s.ID, "incorrect schedule boundary", at)
				}
				if !s.Matches(at) || s.Matches(at.Add(-time.Second)) || s.Matches(at.Add(time.Second)) || !s.MatchesSource(at.Add(-SourcePublishDelay)) || s.MatchesSource(at.Add(-SourcePublishDelay+time.Second)) || !s.MatchesStored(at.Add(-SourcePublishDelay)) {
					t.Fatal(s.ID, "incorrect source boundary", at)
				}
				closeAt := at.Truncate(time.Minute).Add(-10 * time.Second)
				if !s.CloseAt(at).Equal(closeAt) || !s.BettingOpen(closeAt.Add(-time.Nanosecond)) || s.BettingOpen(closeAt) || s.BettingOpen(at.Add(-time.Nanosecond)) || !s.BettingOpen(at) {
					t.Fatal(s.ID, "incorrect ten-second cutoff", at)
				}
			}
		}
	}
}
