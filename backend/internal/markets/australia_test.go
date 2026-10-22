package markets

import (
	"fmt"
	"starlight/backend/internal/games"
	"testing"
)

func TestArchivedMarkSixAndAustraliaKeepSeparateSizeAndParityRules(t *testing.T) {
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			continue
		}
		count, high, tie, small := 7, 49, 25, 24
		if g.ID == "lhc-au" {
			count, high, tie, small = 8, 45, 23, 22
		}
		for pos := 1; pos <= count; pos++ {
			for n := 1; n <= high; n++ {
				numbers := make([]int, count)
				numbers[pos-1] = n
				next := 1
				for i := range numbers {
					if i == pos-1 {
						continue
					}
					if next == n {
						next++
					}
					numbers[i] = next
					next++
				}
				for _, pick := range []string{"big", "small", "odd", "even"} {
					id := fmt.Sprintf("p%d.%s", pos, pick)
					if g.ID == "lhc-au" {
						id = "au45." + id
					} else if pick == "odd" || pick == "even" {
						id = "parity2." + id
					} else {
						id = "size2." + id
					}
					catalogID := g.MarketCatalogID()
					if g.ID == "lhc-au" {
						catalogID = "lhc-au" // Historical 45/8 snapshots remain readable.
					}
					m, ok := Find(catalogID, id)
					if catalogID == "lhc" {
						m, ok = findLegacyMarkSix(id)
					}
					if !ok {
						t.Fatal("missing market", id)
					}
					want := "lose"
					if pick == "odd" && n%2 == 1 || pick == "even" && n%2 == 0 || pick == "small" && n <= small || pick == "big" && n > small && n != tie {
						want = "win"
					}
					if n == tie && (pick == "big" || pick == "small") {
						want = "push"
					}

					got, err := Evaluate(m, g, result(g.ID, numbers))
					if err != nil || got != want {
						t.Fatalf("%s %s number %d: %s %v, want %s", g.ID, id, n, got, err, want)
					}
				}
			}
		}
	}
}

func TestAustraliaRejectsInvalidDrawAndKeepsLegacySnapshot(t *testing.T) {
	g, _ := games.Find("lhc-au")
	m, _ := Find("lhc-au", "au45.p8.n23")
	if got, err := Evaluate(m, g, result(g.ID, []int{1, 2, 3, 4, 5, 6, 7, 23})); err != nil || got != "win" {
		t.Fatal(got, err)
	}
	for _, numbers := range [][]int{{1, 2, 3, 4, 5, 6, 7}, {1, 2, 3, 4, 5, 6, 7, 46}, {1, 2, 3, 4, 5, 6, 7, 7}} {
		if _, err := Evaluate(m, g, result(g.ID, numbers)); err == nil {
			t.Fatal("accepted invalid draw", numbers)
		}
	}
	legacy := Market{ID: "p7.big", Kind: "size", Pick: "big", Positions: []int{7}, RuleVersion: 1, SmallMax: 24, BigMin: 25, PushValues: []int{49}}
	if got, err := Evaluate(legacy, g, result(g.ID, []int{1, 2, 3, 4, 5, 6, 49})); err != nil || got != "push" {
		t.Fatal("legacy result no longer readable", got, err)
	}
	for _, g := range games.All() {
		for _, m := range Catalog(g.MarketCatalogID()) {

			if m.Kind == "parity" && len(m.PushValues) != 0 {
				t.Fatal("parity still pushes", g.ID, m.ID)
			}
		}
	}
}
