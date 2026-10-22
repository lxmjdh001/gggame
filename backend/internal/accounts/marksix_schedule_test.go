package accounts

import (
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestEarlierMarkSixCloseDoesNotDropLaterFastLotteryBets(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	at := time.Date(2026, 10, 13, 21, 30, 5, 0, model.Beijing)
	g, _ := games.Find("lhc-hk")
	in := MarketOrderInput{SessionID: g.ID, Issue: at.Format(model.IssueLayout), MarketID: "lhc6.p1.big", OddsMilli: 1000, UnitCents: 200, Multiplier: 1, Key: randomID()}
	first, err := s.PlaceMarketOrder("member", in, at.Add(-16*time.Minute))
	must(t, err)
	in.Key = randomID()
	if _, err = s.PlaceMarketOrder("member", in, at.Add(-15*time.Minute)); err == nil {
		t.Fatal("accepted closed Mark Six bet")
	}
	// Another overdue issue forces discovery to scan all orders while Mark Six
	// is closed but other games for 21:30 are still taking bets.
	prior := marketInput(at.Add(-16*time.Minute), "p1.big")
	prior.SessionID = "ffc-1"
	prior.Issue = at.Add(-15 * time.Minute).Format(model.IssueLayout)
	_, err = s.PlaceMarketOrder("member", prior, at.Add(-16*time.Minute))
	must(t, err)
	must(t, s.discoverSettlementPlans(at.Add(-10*time.Minute)))
	plans, err := s.settlementPlans()
	must(t, err)
	for _, p := range plans {
		if p.Issue == in.Issue {
			t.Fatal("froze shared issue before other games closed")
		}
	}
	fast := in
	fast.SessionID = "ffc-1"
	fast.MarketID, fast.OddsMilli = "p1.big", 1960
	fast.Key = randomID()
	second, err := s.PlaceMarketOrder("member", fast, at.Add(-30*time.Second))
	must(t, err)
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, err = s.settlementPlans()
	must(t, err)
	found := map[string]bool{}
	for _, p := range plans {
		if p.Issue == in.Issue {
			for _, u := range planUsers(p) {
				for _, id := range u.Orders {
					found[id] = true
				}
			}
		}
	}
	if !found[first.ID] || !found[second.ID] {
		t.Fatal("missing order in shared settlement plan", found)
	}
}
