package markets

import (
	"starlight/backend/internal/games"
	"testing"
)

func TestSizeAndParityMarketsFollowConfiguredRules(t *testing.T) {
	for _, g := range games.All() {
		rule := g.NumberRule()
		for _, m := range Catalog(g.MarketCatalogID()) {

			if m.Kind == "parity" && len(m.PushValues) != 0 {
				t.Fatal(g.ID, m.ID, "parity must not push")
			}
			if m.Kind != "size" {
				continue
			}
			if g.GameID == "pk10" && len(m.Positions) == 2 {
				if m.SmallMax != 11 || m.BigMin != 12 || len(m.PushValues) != 0 || m.RuleVersion != 3 || m.ID != "size3.sum."+m.Pick {
					t.Fatal(g.ID, m.ID, "PK10 sum must use 3–11 small and 12–19 big without push", m)
				}
				continue
			}
			count := len(m.Positions)
			low, high := rule.Min*count, rule.Max*count
			if !rule.AllowRepeat {
				low += count * (count - 1) / 2
				high -= count * (count - 1) / 2
			}
			mid := (low + high) / 2
			if (low+high)%2 == 0 {
				if len(m.PushValues) != 1 || m.PushValues[0] != mid || m.SmallMax != mid-1 || m.BigMin != mid+1 {
					t.Fatal(g.ID, m.ID, "size tie is not the middle", low, high, m)
				}
			} else if len(m.PushValues) != 0 || m.SmallMax != mid || m.BigMin != mid+1 {
				t.Fatal(g.ID, m.ID, "unexpected tie or wrong boundary", m)
			}
		}
	}
}

func TestPK10EveryChampionRunnerUpPair(t *testing.T) {
	g, _ := games.Find("pk10-10")
	for a := 1; a <= 10; a++ {
		for b := 1; b <= 10; b++ {
			if a == b {
				continue
			}
			numbers := []int{a, b}
			for n := 1; n <= 10; n++ {
				if n != a && n != b {
					numbers = append(numbers, n)
				}
			}
			for _, pick := range []string{"big", "small", "odd", "even"} {
				id := "sum." + pick
				if pick == "big" || pick == "small" {
					id = "size3." + id
				}
				m, ok := Find("pk10", id)
				if !ok {
					t.Fatal(id)
				}
				want := "lose"
				sum := a + b
				if pick == "big" && sum > 11 || pick == "small" && sum <= 11 || pick == "odd" && sum%2 == 1 || pick == "even" && sum%2 == 0 {
					want = "win"
				}
				got, err := Evaluate(m, g, result(g.ID, numbers))
				if err != nil || got != want {
					t.Fatal(a, b, id, got, err, want)
				}
			}
		}
	}
}
