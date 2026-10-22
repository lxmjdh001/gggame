package markets

import (
	"fmt"
	"testing"

	"starlight/backend/internal/games"
)

func TestPK10SumSizeV3RejectsAlteredContracts(t *testing.T) {
	g, _ := games.Find("pk10-10")
	m, _ := Find("pk10", "size3.sum.small")
	r := result(g.ID, []int{1, 10, 2, 3, 4, 5, 6, 7, 8, 9})
	for name, change := range map[string]func(*Market){
		"version":            func(m *Market) { m.RuleVersion = 2 },
		"kind":               func(m *Market) { m.Kind = "parity" },
		"id":                 func(m *Market) { m.ID = "size2.sum.small" },
		"pick":               func(m *Market) { m.Pick = "odd" },
		"position":           func(m *Market) { m.Positions = []int{1, 3} },
		"duplicate position": func(m *Market) { m.Positions = []int{1, 1} },
		"small boundary":     func(m *Market) { m.SmallMax = 10 },
		"big boundary":       func(m *Market) { m.BigMin = 11 },
		"push":               func(m *Market) { m.PushValues = []int{11} },
	} {
		t.Run(name, func(t *testing.T) {
			altered := m
			change(&altered)
			if _, err := Evaluate(altered, g, r); err == nil {
				t.Fatal("altered v3 contract accepted", altered)
			}
		})
	}
	for _, other := range games.All() {
		if other.GameID != "pk10" {
			if _, err := Evaluate(m, other, r); err == nil {
				t.Fatal("PK10 v3 rule accepted for another game", other.ID)
			}
		}
	}
}

func TestPK10ExactCarNumbersMatchOnlySelectedRank(t *testing.T) {
	g, _ := games.Find("pk10-10")
	// Rotate all ten car numbers through all ten ranks, then test every pick.
	for offset := 0; offset < 10; offset++ {
		numbers := make([]int, 10)
		for rank := range numbers {
			numbers[rank] = (rank+offset)%10 + 1
		}
		r := result(g.ID, numbers)
		for rank := 1; rank <= 10; rank++ {
			for pick := 1; pick <= 10; pick++ {
				id := fmt.Sprintf("p%d.n%d", rank, pick)
				m, ok := Find("pk10", id)
				if !ok {
					t.Fatal("missing number", id)
				}
				want := "lose"
				if numbers[rank-1] == pick {
					want = "win"
				}
				got, err := Evaluate(m, g, r)
				if err != nil || got != want {
					t.Fatal(id, numbers, got, err, want)
				}
			}
		}
	}
}
