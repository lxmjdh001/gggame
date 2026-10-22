package games

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"starlight/gameapi/pkg/model"
)

func TestSydney49CurrentAndHistoricalRuleSelection(t *testing.T) {
	s, ok := Find("lhc-au")
	if !ok {
		t.Fatal("Sydney session missing")
	}
	if s.GameName != "悉尼六合彩" || s.GameID != "lhc" || s.Group != "g10" || s.Format != "marksix" || !reflect.DeepEqual(s.Positions, []int{8, 9, 10, 11, 12, 13, 14}) {
		t.Fatalf("incorrect current Sydney catalog: %+v", s)
	}
	current := s.NumberRule()
	if current.Min != 1 || current.Max != 49 || current.Count != 7 || current.AllowRepeat || s.MarketCatalogID() != "lhc" {
		t.Fatalf("current Sydney must use seven distinct 1–49 numbers: %+v", current)
	}
	start := Sydney49Start()
	if !start.After(RegionalMarkSixStart()) || !s.Matches(start) {
		t.Fatal("Sydney rule transition must retain an existing 15-minute boundary", start)
	}
	for _, tc := range []struct {
		at     time.Time
		legacy bool
	}{
		{start.Add(-15 * time.Minute), true},
		{start.Truncate(time.Minute).Add(-time.Nanosecond), true},
		{start.Add(-SourcePublishDelay), false},
		{start.Add(-time.Nanosecond), false},
		{start, false},
		{start.Add(15 * time.Minute), false},
	} {
		max, count, catalog := 49, 7, "lhc"
		if tc.legacy {
			max, count, catalog = 45, 8, "lhc-au"
		}
		rule := s.NumberRuleAt(tc.at)
		if s.IsLegacySydney(tc.at) != tc.legacy || rule.Min != 1 || rule.Max != max || rule.Count != count || rule.AllowRepeat || s.MarketCatalogIDAt(tc.at) != catalog {
			t.Fatalf("Sydney rule at %s: %+v, catalog %s", tc.at, rule, s.MarketCatalogIDAt(tc.at))
		}
	}
	for _, other := range All() {
		if other.ID == s.ID {
			continue
		}
		for _, at := range []time.Time{start.Add(-time.Hour), start, start.Add(time.Hour)} {
			if other.IsLegacySydney(at) || !reflect.DeepEqual(other.NumberRuleAt(at), other.NumberRule()) || other.MarketCatalogIDAt(at) != other.MarketCatalogID() {
				t.Fatal("Sydney rule transition changed another session", other.ID, at)
			}
		}
	}
}

func sydney49Permutation(rotate int) []int {
	raw := make([]int, 49)
	for i := range raw {
		raw[i] = (i+rotate)%49 + 1
	}
	return raw
}

func TestSydney49ProjectionPreservesHistoryAndRawSourcePositions(t *testing.T) {
	s, _ := Find("lhc-au")
	start := Sydney49Start()
	for _, at := range []time.Time{start.Add(-15 * time.Minute), start, start.Add(15 * time.Minute)} {
		for rotate := 0; rotate < 49; rotate++ {
			raw := sydney49Permutation(rotate)
			before := append([]int(nil), raw...)
			sourceAt := at.Add(-SourcePublishDelay)
			d := model.Draw{Issue: sourceAt.Format(model.IssueLayout), ScheduledAt: sourceAt, Groups: map[string][]int{"g10": raw}}
			r, err := Project(s, d, "scheduled")
			if err != nil {
				t.Fatal(at, rotate, err)
			}
			want, positions := raw[7:14], []int{8, 9, 10, 11, 12, 13, 14}
			if at.Before(start) {
				var eligible, sourcePositions []int
				for i, n := range raw {
					if n <= 45 {
						eligible = append(eligible, n)
						sourcePositions = append(sourcePositions, i+1)
					}
				}
				want, positions = eligible[7:15], sourcePositions[7:15]
			}
			if !reflect.DeepEqual(r.Numbers, want) || !reflect.DeepEqual(r.Positions, positions) || !reflect.DeepEqual(raw, before) {
				t.Fatalf("projection at %s rotation %d: numbers %v positions %v; want %v %v", at, rotate, r.Numbers, r.Positions, want, positions)
			}
			if r.SessionID != s.ID || r.SourceGroup != "g10" || r.Issue != d.Issue || r.SourceIssue != d.Issue || !r.DrawAt.Equal(at) || r.Origin != "scheduled" || len(r.DisplayNumbers) != len(want) {
				t.Fatalf("projection lost source metadata: %+v", r)
			}
			for i, n := range want {
				if raw[r.Positions[i]-1] != n || r.DisplayNumbers[i] != fmt.Sprintf("%02d", n) {
					t.Fatal("incorrect source provenance or display", r)
				}
			}
		}
	}
}

func TestSydney49RejectsInvalidFullSourceOnBothSidesOfTransition(t *testing.T) {
	s, _ := Find("lhc-au")
	for _, at := range []time.Time{Sydney49Start().Add(-15 * time.Minute), Sydney49Start()} {
		for _, tc := range []struct {
			name   string
			mutate func([]int) []int
		}{
			{"missing", func([]int) []int { return nil }},
			{"short", func(raw []int) []int { return raw[:48] }},
			{"long", func(raw []int) []int { return append(raw, 1) }},
			{"duplicate_outside_projection", func(raw []int) []int { raw[48] = raw[0]; return raw }},
			{"zero_outside_projection", func(raw []int) []int { raw[48] = 0; return raw }},
			{"high_outside_projection", func(raw []int) []int { raw[48] = 50; return raw }},
		} {
			t.Run(at.Format(model.IssueLayout)+"/"+tc.name, func(t *testing.T) {
				sourceAt := at.Add(-SourcePublishDelay)
				d := model.Draw{Issue: sourceAt.Format(model.IssueLayout), ScheduledAt: sourceAt, Groups: map[string][]int{"g10": tc.mutate(sydney49Permutation(0))}}
				if _, err := Project(s, d, "scheduled"); err == nil {
					t.Fatal("invalid complete Sydney source accepted")
				}
			})
		}
	}
}

func TestSydney49LeavesTaiwanProjectionUnchanged(t *testing.T) {
	tw, _ := Find("lhc-tw")
	// Hour boundaries are shared by the Sydney 15-minute and Taiwan 20-minute schedules.
	hour := Sydney49Start().Truncate(time.Hour).Add(DrawSecond * time.Second)
	for _, at := range []time.Time{hour.Add(-time.Hour), hour.Add(time.Hour)} {
		for rotate := 0; rotate < 49; rotate++ {
			raw := sydney49Permutation(rotate)
			sourceAt := at.Add(-SourcePublishDelay)
			d := model.Draw{Issue: sourceAt.Format(model.IssueLayout), ScheduledAt: sourceAt, Groups: map[string][]int{"g10": raw}}
			r, err := Project(tw, d, "scheduled")
			if err != nil || !reflect.DeepEqual(r.Numbers, raw[:7]) || !reflect.DeepEqual(r.Positions, []int{1, 2, 3, 4, 5, 6, 7}) {
				t.Fatal("Sydney transition changed Taiwan", at, rotate, r, err)
			}
		}
	}
}

func TestSydney49KeepsFifteenMinuteScheduleAndSixtyFiveSecondClose(t *testing.T) {
	s, _ := Find("lhc-au")
	if s.IntervalMinutes != 15 || s.OffsetMinutes != 0 || s.ClosedWindowSeconds() != 65 {
		t.Fatal("Sydney cadence or cutoff configuration changed", s)
	}
	for i := -1; i <= 400; i++ {
		at := Sydney49Start().Add(time.Duration(i) * 15 * time.Minute)
		if !s.Due(at).Equal(at) || !s.Next(at).Equal(at.Add(15*time.Minute)) || !s.Due(at.Add(-time.Nanosecond)).Equal(at.Add(-15*time.Minute)) || !s.Next(at.Add(-time.Nanosecond)).Equal(at) || !s.MatchesSource(at.Add(-SourcePublishDelay)) {
			t.Fatal("Sydney rule transition changed schedule", at)
		}
		closeAt := at.Add(-65 * time.Second)
		if !s.CloseAt(at).Equal(closeAt) || !s.BettingOpen(closeAt.Add(-time.Nanosecond)) || s.BettingOpen(closeAt) || s.BettingOpen(at.Add(-time.Nanosecond)) || !s.BettingOpen(at) {
			t.Fatal("Sydney rule transition changed betting window", at)
		}
	}
}
