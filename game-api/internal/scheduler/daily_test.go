package scheduler

import (
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestNextDayBeijingBoundaries(t *testing.T) {
	for _, tc := range []struct{ at, want string }{{"2026-12-31T15:59:59Z", "2027-01-01T00:00:00+08:00"}, {"2026-12-31T16:00:00Z", "2027-01-02T00:00:00+08:00"}, {"2028-02-28T16:00:00Z", "2028-03-01T00:00:00+08:00"}} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		if got := NextDay(at).In(model.Beijing).Format(time.RFC3339); got != tc.want {
			t.Fatalf("%s: %s", tc.at, got)
		}
	}
}
