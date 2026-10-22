package games

import (
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestTwentyMinuteMarkSixTransition(t *testing.T) {
	lastDaily := time.Date(2026, 10, 14, 21, 30, 5, 0, model.Beijing)
	start := TwentyMinuteMarkSixStart()
	for _, id := range []string{"lhc-tw", "lhc-daily", "lhc-au"} {
		g, _ := Find(id)
		for _, tc := range []struct{ now, next time.Time }{
			{lastDaily.Add(-time.Hour), lastDaily},
			{lastDaily.Add(-time.Nanosecond), lastDaily},
			{lastDaily, start},
			{start.Add(-time.Nanosecond), start},
			{start, start.Add(20 * time.Minute)},
		} {
			if got := g.Next(tc.now); !got.Equal(tc.next) {
				t.Fatalf("%s next(%s)=%s, want %s", id, tc.now, got, tc.next)
			}
		}
		if !g.CloseAt(lastDaily).Equal(lastDaily.Truncate(time.Minute).Add(-15 * time.Minute)) {
			t.Fatal(id, "legacy cutoff changed")
		}
		if !g.MatchesStored(lastDaily) || !g.MatchesStored(lastDaily.Add(-SourcePublishDelay)) {
			t.Fatal(id, "legacy results rejected")
		}
		for at := start; at.Before(RegionalMarkSixStart()); at = at.Add(20 * time.Minute) {
			if !g.Matches(at) || !g.MatchesSource(at.Add(-SourcePublishDelay)) {
				t.Fatal(id, "missing boundary", at)
			}
			close := g.CloseAt(at)
			if at.Sub(close) != 65*time.Second {
				t.Fatal(id, "cutoff changed")
			}
			if !g.BettingOpen(close.Add(-time.Nanosecond)) || g.BettingOpen(close) {
				t.Fatal(id, "cutoff incorrect", at)
			}
		}
		if g.Matches(start.Add(10 * time.Minute)) {
			t.Fatal(id, "accepted non-20-minute round")
		}
		if got := g.Due(start.Add(-time.Nanosecond)); !got.Equal(lastDaily) {
			t.Fatal(id, "lost last daily result", got)
		}
	}
	s, _ := Find("lhc-au")
	legacy := s.NumberRuleAt(lastDaily)
	if s.GameName != "悉尼六合彩" || legacy.Max != 45 || legacy.Count != 8 || s.MarketCatalogIDAt(lastDaily) != "lhc-au" || !s.IsLegacySydney(lastDaily) {
		t.Fatal("historical Sydney rule changed")
	}
	for _, id := range []string{"lhc-sg", "lhc-bj"} {
		g, _ := Find(id)
		if !g.Due(lastDaily).Equal(lastDaily) || !g.Next(lastDaily.Add(-time.Nanosecond)).Equal(lastDaily) {
			t.Fatal("historical daily schedule changed", id)
		}
	}
}
