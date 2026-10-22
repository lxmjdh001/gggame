package games

import (
	"reflect"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestRegionalMarkSixCatalog(t *testing.T) {
	want := []struct {
		id, name, group string
		minutes         int
		positions       []int
	}{
		{"lhc-daily", "大马六合彩", "g7", 5, []int{1, 2, 3, 4, 5, 6, 7}},
		{"lhc-au", "悉尼六合彩", "g10", 15, []int{8, 9, 10, 11, 12, 13, 14}},
		{"lhc-tw", "台湾六合彩", "g10", 20, []int{1, 2, 3, 4, 5, 6, 7}},
		{"lhc-hk", "极速六合彩", "g9", 1, []int{1, 2, 3, 4, 5, 6, 7}},
		{"lhc-sg", "五分六合彩", "g11", 5, []int{1, 2, 3, 4, 5, 6, 7}},
		{"lhc-bj", "十分六合彩", "g9", 10, []int{8, 9, 10, 11, 12, 13, 14}},
		{"lhc-id", "印尼六合彩", "g7", 20, []int{8, 9, 10, 11, 12, 13, 14}},
		{"lhc-vn", "越南六合彩", "g11", 20, []int{8, 9, 10, 11, 12, 13, 14}},
	}
	var got []Session
	for _, s := range All() {
		if s.GameID == "lhc" {
			got = append(got, s)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d Mark Six sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		s := got[i]
		if s.ID != w.id || s.GameName != w.name || s.IntervalMinutes != w.minutes || s.OffsetMinutes != 0 || s.Group != w.group || !reflect.DeepEqual(s.Positions, w.positions) {
			t.Fatalf("session %d: got %+v, want %+v", i, s, w)
		}
		rule := s.NumberRule()
		if rule.Min != 1 || rule.Max != 49 || rule.Count != 7 || rule.AllowRepeat || s.MarketCatalogID() != "lhc" {
			t.Fatalf("%s number rules or markets changed: %+v", s.ID, rule)
		}
	}
}

func TestRegionalMarkSixTransition(t *testing.T) {
	start := RegionalMarkSixStart()
	lastTwenty := start.Add(-20 * time.Minute)
	lastDaily := time.Date(2026, 10, 14, 21, 30, DrawSecond, 0, model.Beijing)
	for _, tc := range []struct {
		id   string
		last time.Time
	}{
		{"lhc-daily", lastTwenty}, {"lhc-au", lastTwenty}, {"lhc-tw", lastTwenty},
		{"lhc-hk", HongKongScheduleStart()}, {"lhc-sg", lastDaily}, {"lhc-bj", lastDaily},
		{"lhc-id", lastTwenty}, {"lhc-vn", lastTwenty},
	} {
		s, _ := Find(tc.id)
		if got := s.Due(start.Add(-time.Nanosecond)); !got.Equal(tc.last) {
			t.Fatalf("%s lost historical latest issue: got %s, want %s", s.ID, got, tc.last)
		}
		for _, now := range []time.Time{tc.last, tc.last.Add(time.Nanosecond), start.Add(-time.Minute), start.Add(-time.Nanosecond)} {
			if got := s.Next(now); !got.Equal(start) {
				t.Fatalf("%s next(%s) = %s, want first regional issue %s", s.ID, now, got, start)
			}
		}
		if !s.Due(start).Equal(start) || !s.MatchesSource(start.Add(-SourcePublishDelay)) {
			t.Fatal(s.ID, "first regional issue missing")
		}
		interval := time.Duration(s.IntervalMinutes) * time.Minute
		if got := s.Next(start); !got.Equal(start.Add(interval)) {
			t.Fatal(s.ID, "wrong interval after transition", got)
		}
		closeAt := start.Truncate(time.Minute).Add(-time.Minute)
		if s.ID == "lhc-hk" {
			closeAt = start.Truncate(time.Minute).Add(-10 * time.Second)
		}
		if !s.CloseAt(start).Equal(closeAt) || !s.BettingOpen(closeAt.Add(-time.Nanosecond)) || s.BettingOpen(closeAt) || s.BettingOpen(start.Add(-time.Nanosecond)) || !s.BettingOpen(start) {
			t.Fatal(s.ID, "first regional cutoff or reopening incorrect")
		}
	}
}

func TestRegionalMarkSixBoundariesAcrossDaysMonthsAndYears(t *testing.T) {
	for _, s := range All() {
		if s.GameID != "lhc" {
			continue
		}
		interval := time.Duration(s.IntervalMinutes) * time.Minute
		window := 65 * time.Second
		if s.ID == "lhc-hk" {
			window = 15 * time.Second
		}
		if s.ClosedWindowSeconds() != int(window/time.Second) {
			t.Fatal(s.ID, "wrong configured close window")
		}
		start := RegionalMarkSixStart().Add(12 * time.Hour)
		for day := 0; day < 400; day++ {
			midnight := start.AddDate(0, 0, day)
			for _, offset := range []time.Duration{0, interval, 12 * time.Hour, 24*time.Hour - interval} {
				at := midnight.Add(offset)
				if !s.Due(at).Equal(at) || !s.Next(at).Equal(at.Add(interval)) || !s.Due(at.Add(-time.Nanosecond)).Equal(at.Add(-interval)) || !s.Next(at.Add(-time.Nanosecond)).Equal(at) {
					t.Fatal(s.ID, "broken boundary", at)
				}
				if !s.Matches(at) || s.Matches(at.Add(-time.Nanosecond)) || s.Matches(at.Add(time.Nanosecond)) || !s.MatchesSource(at.Add(-SourcePublishDelay)) || s.MatchesSource(at.Add(-SourcePublishDelay+time.Second)) {
					t.Fatal(s.ID, "incorrect boundary or source match", at)
				}
				if !s.MatchesStored(at) || !s.MatchesStored(at.Add(-SourcePublishDelay)) {
					t.Fatal(s.ID, "stored result timestamp rejected", at)
				}
				closeAt := s.CloseAt(at)
				if at.Sub(closeAt) != window || !s.BettingOpen(closeAt.Add(-time.Nanosecond)) || s.BettingOpen(closeAt) || s.BettingOpen(at.Add(-time.Nanosecond)) || !s.BettingOpen(at) {
					t.Fatal(s.ID, "incorrect closed window", at)
				}
			}
		}
	}
}

func TestRegionalMarkSixKeepsPreviouslyBookedEveningIssues(t *testing.T) {
	for _, id := range []string{"lhc-hk", "lhc-sg", "lhc-bj"} {
		s, _ := Find(id)
		at := time.Date(2026, 10, 15, 21, 30, DrawSecond, 0, model.Beijing)
		if !s.Matches(at) || !s.MatchesSource(at.Add(-SourcePublishDelay)) || !s.MatchesStored(at.Add(-SourcePublishDelay)) {
			t.Fatal(id, "existing evening issue rejected")
		}
	}
}

func TestNewRegionalMarkSixHistoryUsesTwentyMinuteCadence(t *testing.T) {
	for _, id := range []string{"lhc-id", "lhc-vn"} {
		s, _ := Find(id)
		at := RegionalMarkSixStart()
		for i := 0; i < 400*72; i++ {
			at = at.Add(-20 * time.Minute)
			if !s.Matches(at) || !s.MatchesSource(at.Add(-SourcePublishDelay)) || !s.Due(at.Add(19*time.Minute)).Equal(at) || !s.Next(at).Equal(at.Add(20*time.Minute)) || at.Sub(s.CloseAt(at)) != 65*time.Second {
				t.Fatal(id, "historical seed boundary rejected", at)
			}
		}
	}
}
