package games

import (
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestHongKongHistoricalSchedules(t *testing.T) {
	g, _ := Find("lhc-hk")
	for _, tc := range []struct{ now, due, next string }{
		{"2026-01-01T00:00:00+08:00", "2025-12-31T21:30:05+08:00", "2026-01-01T21:30:05+08:00"},
		{"2026-10-01T00:00:00+08:00", "2026-09-30T21:30:05+08:00", "2026-10-01T21:30:05+08:00"},
		{"2026-10-13T00:00:00+08:00", "2026-10-12T21:30:05+08:00", "2026-10-13T21:30:05+08:00"},
		{"2026-10-13T21:30:04+08:00", "2026-10-12T21:30:05+08:00", "2026-10-13T21:30:05+08:00"},
		{"2026-10-13T21:30:05+08:00", "2026-10-13T21:30:05+08:00", "2026-10-15T12:00:05+08:00"},
		{"2026-10-14T23:59:59+08:00", "2026-10-13T21:30:05+08:00", "2026-10-15T12:00:05+08:00"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		if got := g.Due(now).Format(time.RFC3339); got != tc.due {
			t.Fatal(tc.now, "due", got, tc.due)
		}
		if got := g.Next(now).Format(time.RFC3339); got != tc.next {
			t.Fatal(tc.now, "next", got, tc.next)
		}
	}
	start := HongKongScheduleStart()
	for day := 0; start.AddDate(0, 0, day).Before(RegionalMarkSixStart()); day++ {
		at := start.AddDate(0, 0, day)
		if g.Matches(at) != (day%2 == 0) || g.MatchesSource(at.Add(-SourcePublishDelay)) != (day%2 == 0) {
			t.Fatal("wrong draw day", at)
		}
	}
	// Already published daily records remain valid after the schedule change.
	if !g.MatchesStored(time.Date(2026, 10, 12, 21, 30, 5, 0, model.Beijing)) {
		t.Fatal("old daily result rejected")
	}
}

func TestHistoricalMarkSixRetainsFifteenMinuteClose(t *testing.T) {
	for _, id := range []string{"lhc-hk", "lhc-tw", "lhc-daily", "lhc-sg", "lhc-bj", "lhc-au"} {
		g, _ := Find(id)
		at := time.Date(2026, 10, 13, 21, 30, 5, 0, model.Beijing)
		closeAt := at.Truncate(time.Minute).Add(-15 * time.Minute)
		if !g.CloseAt(at).Equal(closeAt) {
			t.Fatal(g.ID, "wrong close time")
		}
		if !g.BettingOpen(closeAt.Add(-time.Nanosecond)) {
			t.Fatal(g.ID, "closed early")
		}
		for _, now := range []time.Time{closeAt, closeAt.Add(time.Minute), at.Add(-5 * time.Second), at.Add(-time.Nanosecond)} {
			if g.BettingOpen(now) || !g.Next(now).Equal(at) {
				t.Fatal(g.ID, "late bet or early rollover", now)
			}
		}
		next := at.Add(24 * time.Hour)
		if g.ID == "lhc-hk" {
			next = RegionalMarkSixStart()
		}
		if !g.BettingOpen(at) || !g.Next(at).Equal(next) {
			t.Fatal(g.ID, "next issue incorrect")
		}
	}
}

func TestHongKongSimulatedHistoryUsesEpochBackwards(t *testing.T) {
	g, _ := Find("lhc-hk")
	for _, tc := range []struct{ now, want string }{
		{"2026-10-13T12:00:00+08:00", "2026-10-11T21:30:05+08:00"},
		{"2026-10-11T21:30:04+08:00", "2026-10-09T21:30:05+08:00"},
		{"2026-10-11T21:30:05+08:00", "2026-10-11T21:30:05+08:00"},
		{"2026-10-01T12:00:00+08:00", "2026-09-29T21:30:05+08:00"},
		{"2026-10-15T21:30:05+08:00", "2026-10-15T21:30:05+08:00"},
		{"2026-10-31T21:30:05+08:00", "2026-10-31T21:30:05+08:00"},
		{"2026-12-31T21:30:05+08:00", "2026-12-30T21:30:05+08:00"},
		{"2027-01-01T21:30:05+08:00", "2027-01-01T21:30:05+08:00"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		if got := g.SimulatedDue(now).Format(time.RFC3339); got != tc.want {
			t.Fatal(tc.now, got, tc.want)
		}
	}
	// The legacy migration must remain anchored even across calendar boundaries.
	for day := 0; day < 400; day++ {
		at := HongKongScheduleStart().AddDate(0, 0, day)
		if g.SimulatedDue(at).Equal(at) != (day%2 == 0) {
			t.Fatal("legacy seed cadence changed", at)
		}
	}
}
