package games

import (
	"reflect"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestSessionBoundaries(t *testing.T) {
	cases := []struct{ id, now, due, next string }{
		{"ssc-10", "2026-10-12T00:00:00+08:00", "2026-10-11T23:50:05+08:00", "2026-10-12T00:00:05+08:00"},
		{"ssc-10", "2026-10-12T00:00:05+08:00", "2026-10-12T00:00:05+08:00", "2026-10-12T00:10:05+08:00"},
		{"ffc-3", "2026-10-11T09:07:05Z", "2026-10-11T17:06:05+08:00", "2026-10-11T17:09:05+08:00"},
		{"ffc-5", "2026-10-11T23:59:59+08:00", "2026-10-11T23:55:05+08:00", "2026-10-12T00:00:05+08:00"},
		{"lhc-daily", "2026-10-11T21:30:00+08:00", "2026-10-10T21:30:05+08:00", "2026-10-11T21:30:05+08:00"},
		{"lhc-daily", "2026-10-11T21:30:05+08:00", "2026-10-11T21:30:05+08:00", "2026-10-12T21:30:05+08:00"},
		{"lhc-daily", "2027-01-01T00:00:00+08:00", "2026-12-31T23:55:05+08:00", "2027-01-01T00:00:05+08:00"},
	}
	for _, tt := range cases {
		s, _ := Find(tt.id)
		now, _ := time.Parse(time.RFC3339, tt.now)
		if got := s.Due(now).Format(time.RFC3339); got != tt.due {
			t.Errorf("%s due %s want %s", tt.id, got, tt.due)
		}
		if got := s.Next(now).Format(time.RFC3339); got != tt.next {
			t.Errorf("%s next %s want %s", tt.id, got, tt.next)
		}
	}
}
func TestExactPositionsAndFormatting(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2027-01-01T00:00:01+08:00")
	d := model.Draw{Issue: at.Format(model.IssueLayout), ScheduledAt: at, Groups: map[string][]int{"g1": {0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, "g2": {9, 8, 7, 6, 5, 4, 3, 2, 1, 0}, "g3": {1, 2, 3, 4, 5, 6, 7, 8, 9, 1}, "g8": {1, 2, 3, 4, 5, 6, 1, 2, 3, 4}, "g6": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, "g7": {1, 2, 3, 4, 5, 6, 49}}}
	expected := map[string][]int{"ssc-10": {9, 8, 7, 6, 5}, "ffc-1": {0, 1, 2, 3, 4}, "ffc-3": {5, 6, 7, 8, 9}, "ffc-5": {2, 3, 4, 5, 6}, "k3-1": {1, 2, 3}, "k3-3": {2, 3, 4}, "k3-5": {4, 5, 6}, "pk10-10": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, "lhc-daily": {1, 2, 3, 4, 5, 6, 49}, "pc28-5": {1, 4, 7}}
	expected["k3-zj"] = []int{1, 4, 1}
	expected["k3-fj"] = []int{2, 5, 2}
	expected["k3-gd"] = []int{3, 6, 3}
	expected["k3-hn"] = []int{4, 1, 4}
	expected["k3-tw"] = []int{1, 5, 3}
	expected["k3-gx"] = []int{2, 6, 4}
	for offset, group := range []string{"g9", "g10", "g11"} {
		for n := 0; n < 49; n++ {
			d.Groups[group] = append(d.Groups[group], (n+offset*7)%49+1)
		}
	}
	expected["lhc-hk"] = []int{1, 2, 3, 4, 5, 6, 7}
	expected["lhc-tw"] = []int{8, 9, 10, 11, 12, 13, 14}
	expected["lhc-sg"] = []int{15, 16, 17, 18, 19, 20, 21}
	expected["lhc-bj"] = []int{8, 9, 10, 11, 12, 13, 14}
	expected["lhc-au"] = []int{15, 16, 17, 18, 19, 20, 21}
	for n := 7; n < 49; n++ {
		d.Groups["g7"] = append(d.Groups["g7"], n)
	}
	expected["lhc-id"] = []int{7, 8, 9, 10, 11, 12, 13}
	expected["lhc-vn"] = []int{22, 23, 24, 25, 26, 27, 28}
	for _, s := range All() {
		r, err := Project(s, d, "scheduled")
		if err != nil {
			t.Fatal(err)
		}
		if r.DrawAt.Second() != DrawSecond {
			t.Fatal("result not scheduled at second05")
		}
		if !reflect.DeepEqual(r.Numbers, expected[s.ID]) {
			t.Errorf("wrong mapping %s: %v", s.ID, r.Numbers)
		}
		if s.GameID == "pc28" && (r.Sum == nil || *r.Sum != 12) {
			t.Fatal("PC28 sum incorrect")
		}
		if s.GameID == "pk10" && (r.DisplayNumbers[0] != "01" || r.DisplayNumbers[9] != "10") {
			t.Fatal("PK10 not padded")
		}
	}
}

func TestClosedWindowAllSessions(t *testing.T) {
	for _, s := range All() {
		// Include cross-midnight and cross-year boundaries, plus the daily draw.
		for _, stamp := range []string{"2026-10-11T21:30:05+08:00", "2027-01-01T00:00:05+08:00"} {
			anchor, _ := time.Parse(time.RFC3339, stamp)
			due := s.Due(anchor)
			closeAt := s.CloseAt(due)
			window, second := 15*time.Second, 50
			if s.GameID == "lhc" {
				window, second = 65*time.Second, 0
				if due.Before(RegionalMarkSixStart()) && s.ID != "lhc-id" && s.ID != "lhc-vn" {
					window = 905 * time.Second
					if s.IsTwentyMinuteMarkSix() && !due.Before(TwentyMinuteMarkSixStart()) {
						window = 65 * time.Second
					}
				} else if s.ID == "lhc-hk" {
					window, second = 15*time.Second, 50
				}
			}
			if due.Sub(closeAt) != window || due.Second() != 5 || closeAt.Second() != second {
				t.Fatalf("%s incorrect timing %s %s", s.ID, closeAt, due)
			}
			if !s.BettingOpen(closeAt.Add(-time.Nanosecond)) {
				t.Fatalf("%s closed too early", s.ID)
			}
			for _, offset := range []time.Duration{0, 9 * time.Second, 10 * time.Second, 11 * time.Second, window - time.Nanosecond} {
				now := closeAt.Add(offset)
				if s.BettingOpen(now) || !s.Next(now).Equal(due) {
					t.Fatalf("%s reopened or changed issue in gap at %s", s.ID, now)
				}
			}
			interval := time.Duration(s.IntervalMinutes) * time.Minute
			if s.ID == "lhc-hk" && due.Before(HongKongScheduleStart()) {
				interval = 24 * time.Hour
			}
			if s.IsTwentyMinuteMarkSix() && due.Before(TwentyMinuteMarkSixStart()) {
				interval = 24 * time.Hour
			}
			if (s.ID == "lhc-sg" || s.ID == "lhc-bj") && due.Before(RegionalMarkSixStart()) {
				interval = 24 * time.Hour
			}
			if !s.BettingOpen(due) || !s.Next(due).Equal(due.Add(interval)) {
				t.Fatalf("%s failed to reopen next issue at05", s.ID)
			}
		}
	}
}
