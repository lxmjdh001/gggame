package markets

import (
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func result(id string, n []int) games.Round {
	g, _ := games.Find(id)
	at := g.Due(time.Now().Add(-24 * time.Hour))
	return games.Round{SessionID: id, Issue: at.Format(model.IssueLayout), SourceIssue: at.Format(model.IssueLayout), SourceGroup: g.Group, DrawAt: at, Origin: "scheduled", Numbers: n}
}
func TestRulesAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		id, market string
		n          []int
		want       string
	}{
		{"ffc-1", "p1.small", []int{4, 1, 2, 3, 5}, "win"}, {"ffc-1", "p1.big", []int{5, 1, 2, 3, 4}, "win"}, {"ffc-1", "p1.even", []int{0, 1, 2, 3, 4}, "win"},
		{"ffc-1", "sum.small", []int{9, 9, 4, 0, 0}, "win"}, {"ffc-1", "sum.big", []int{9, 9, 5, 0, 0}, "win"},
		{"ffc-1", "dt.dragon", []int{4, 1, 2, 3, 4}, "push"}, {"ffc-1", "dt.tiger", []int{4, 1, 2, 3, 5}, "win"},
		{"pk10-10", "p1.big", []int{6, 1, 2, 3, 4, 5, 7, 8, 9, 10}, "win"}, {"pk10-10", "size3.sum.small", []int{6, 5, 2, 3, 4, 1, 7, 8, 9, 10}, "win"},
		{"pk10-10", "size3.sum.big", []int{6, 5, 2, 3, 4, 1, 7, 8, 9, 10}, "lose"},
		{"pc28-5", "sum.small", []int{4, 4, 5}, "win"}, {"pc28-5", "sum.big", []int{4, 4, 6}, "win"}, {"pc28-5", "sum.n13", []int{4, 4, 5}, "win"},
		{"k3-1", "k3v2.sum.small", []int{1, 1, 1}, "lose"}, {"k3-1", "k3v2.sum.big", []int{6, 6, 6}, "lose"},
		{"k3-1", "k3v2.sum.n10", []int{2, 3, 5}, "win"}, {"k3-1", "k3v3.single.n2", []int{2, 3, 5}, "win"},
		{"lhc-daily", "size2.p7.big", []int{1, 2, 3, 4, 5, 6, 25}, "push"}, {"lhc-daily", "parity2.p7.odd", []int{1, 2, 3, 4, 5, 6, 49}, "win"}, {"lhc-daily", "p7.n49", []int{1, 2, 3, 4, 5, 6, 49}, "win"},
	} {
		g, _ := games.Find(tc.id)
		m, ok := Find(g.GameID, tc.market)
		if !ok && g.GameID == "lhc" {
			m, ok = findLegacyMarkSix(tc.market)
		}
		if !ok {
			t.Fatal("missing market", tc.market)
		}
		got, e := Evaluate(m, g, result(tc.id, tc.n))
		if e != nil || got != tc.want {
			t.Fatalf("%s %s got %s %v want %s", tc.id, tc.market, got, e, tc.want)
		}
	}
}
func TestEveryDiceOutcomeAndInvalidResults(t *testing.T) {
	g, _ := games.Find("k3-1")
	for _, id := range []string{"k3v2.sum.big", "k3v2.sum.small", "k3v2.sum.odd", "k3v2.sum.even"} {
		m, _ := Find("k3", id)
		wins := 0
		for a := 1; a <= 6; a++ {
			for b := 1; b <= 6; b++ {
				for c := 1; c <= 6; c++ {
					got, e := Evaluate(m, g, result(g.ID, []int{a, b, c}))
					if e != nil {
						t.Fatal(e)
					}
					if got == "win" {
						wins++
					}
				}
			}
		}
		if wins != 105 {
			t.Fatalf("%s winning outcomes %d/216", id, wins)
		}
	}
	m, _ := Find("k3", "k3v3.single.n1")
	for _, n := range [][]int{{1, 2, 7}, {0, 2, 3}, {1, 2}} {
		if _, e := Evaluate(m, g, result(g.ID, n)); e == nil {
			t.Fatal("invalid dice accepted", n)
		}
	}
	for _, game := range []string{"ssc", "ffc", "pk10", "k3", "pc28", "lhc"} {
		seen := map[string]bool{}
		for _, m := range Catalog(game) {
			if seen[m.ID] || m.DefaultOdds < 1000 || m.DefaultOdds > 1000000 {
				t.Fatal("invalid catalog", m)
			}
			seen[m.ID] = true
		}
	}
}

func TestDragonTigerTie(t *testing.T) {
	for _, id := range []string{"ssc", "pk10", "k3", "pc28", "lhc"} {
		for _, m := range Catalog(id) {
			if m.Pick == "tie" && m.Kind != "lhc_seven_wave" {
				t.Fatalf("unexpected tie in %s", id)
			}
		}
	}
	for _, session := range []string{"ffc-1", "ffc-3", "ffc-5"} {
		g, _ := games.Find(session)
		tie, ok := Find(g.GameID, "dt.tie")
		if !ok || tie.DefaultOdds != 8000 {
			t.Fatal("missing tie or wrong default odds")
		}
		for a := 0; a <= 9; a++ {
			for b := 0; b <= 9; b++ {
				r := result(session, []int{a, 1, 2, 3, b})
				for _, pick := range []string{"tie", "dragon", "tiger"} {
					m, _ := Find(g.GameID, "dt."+pick)
					want := "lose"
					if pick == "tie" && a == b || pick == "dragon" && a > b || pick == "tiger" && a < b {
						want = "win"
					}
					if pick != "tie" && a == b {
						want = "push"
					}
					got, e := Evaluate(m, g, r)
					if e != nil || got != want {
						t.Fatalf("%s %s %d/%d: %s %v want %s", session, pick, a, b, got, e, want)
					}
				}
			}
		}
	}
}
