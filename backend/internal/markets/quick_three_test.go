package markets

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"starlight/backend/internal/games"
)

func TestQuickThreeCatalogAndEveryOutcome(t *testing.T) {
	catalog := Catalog("k3")
	if len(catalog) != 31 {
		t.Fatalf("expected 31 selections, got %d", len(catalog))
	}
	counts := map[string]int{}
	for _, m := range catalog {
		counts[m.Group]++
		wantVersion := 2
		if m.Kind == "dice_face" || m.Kind == "dice_double" {
			wantVersion = 3
		}
		if m.DefaultOdds != 1000 || m.RuleVersion != wantVersion || !strings.HasPrefix(m.ID, fmt.Sprintf("k3v%d.", wantVersion)) || !reflect.DeepEqual(m.Positions, []int{1, 2, 3}) || len(m.PushValues) != 0 {
			t.Fatal("invalid new catalog", m)
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{"双面": 4, "三军": 6, "围骰": 6, "全骰": 1, "点数": 14}) {
		t.Fatal("wrong game groups", counts)
	}
	for _, id := range []string{"p1.big", "p2.n2", "p3.odd", "sum.big", "sum.n3", "sum.n18", "k3v2.sum.n3", "k3v2.sum.n18", "k3v2.single.n1", "k3v2.double.n6"} {
		if _, ok := Find("k3", id); ok {
			t.Fatal("retired/invalid selection still offered", id)
		}
	}
	g, _ := games.Find("k3-1")
	r := result(g.ID, []int{1, 2, 3})
	wins := map[string]int{}
	for a := 1; a <= 6; a++ {
		for b := 1; b <= 6; b++ {
			for c := 1; c <= 6; c++ {
				r.Numbers = []int{a, b, c}
				frequencies := [7]int{}
				frequencies[a]++
				frequencies[b]++
				frequencies[c]++
				sum, triple := a+b+c, a == b && b == c
				expected := map[string]bool{
					"sum.big": sum >= 11 && sum <= 17 && !triple, "sum.small": sum >= 4 && sum <= 10 && !triple,
					"sum.odd": sum%2 == 1 && !triple, "sum.even": sum%2 == 0 && !triple, "triple.any": triple,
				}
				for n := 1; n <= 6; n++ {
					expected[fmt.Sprintf("single.n%d", n)] = frequencies[n] >= 1 && !triple
					expected[fmt.Sprintf("triple.n%d", n)] = frequencies[n] == 3
					expected[fmt.Sprintf("double.n%d", n)] = frequencies[n] >= 2 && !triple
				}
				for n := 4; n <= 17; n++ {
					expected[fmt.Sprintf("sum.n%d", n)] = sum == n
				}
				for x := 1; x <= 5; x++ {
					for y := x + 1; y <= 6; y++ {
						expected[fmt.Sprintf("pair.%d_%d", x, y)] = frequencies[x] > 0 && frequencies[y] > 0
					}
				}
				for _, m := range catalog {
					win, found := expected[strings.TrimPrefix(strings.TrimPrefix(m.ID, "k3v2."), "k3v3.")]
					if !found {
						t.Fatal("uncovered selection", m.ID)
					}
					want := "lose"
					if win {
						want = "win"
						wins[m.ID]++
					}
					got, err := Evaluate(m, g, r)
					if err != nil || got != want {
						t.Fatalf("%s %v: %s %v, want %s", m.ID, r.Numbers, got, err, want)
					}
				}
			}
		}
	}
	for _, m := range catalog {
		want := 0
		switch m.Kind {
		case "size", "parity":
			want = 105
		case "dice_face":
			want = 90
		case "triple":
			want = 1
			if m.Pick == "any" {
				want = 6
			}
		case "dice_pair":
			want = 30
		case "dice_double":
			want = 15
		case "number":
			n, _ := strconv.Atoi(m.Pick)
			want = []int{3, 6, 10, 15, 21, 25, 27, 27, 25, 21, 15, 10, 6, 3}[n-4]
		}
		if wins[m.ID] != want {
			t.Fatalf("%s won %d outcomes, expected %d", m.ID, wins[m.ID], want)
		}
	}
}

func TestQuickThreeRejectsAlteredRulesAndInvalidDice(t *testing.T) {
	g, _ := games.Find("k3-1")
	r := result(g.ID, []int{1, 1, 1})
	for _, id := range []string{"k3v2.sum.small", "k3v3.single.n1", "k3v2.triple.n1", "k3v2.triple.any", "k3v2.pair.1_2", "k3v3.double.n1", "k3v2.sum.n6"} {
		var m Market
		for _, historical := range quickThreeV3Catalog() {
			if historical.ID == id {
				m = historical
				break
			}
		}
		if m.ID == "" {
			t.Fatal(id)
		}
		for _, mutate := range []func(*Market){
			func(m *Market) { m.RuleVersion = 1 },
			func(m *Market) { m.ID = "sum.small" },
			func(m *Market) { m.Pick = "0" },
			func(m *Market) { m.Kind = "size"; m.BigMin = 0 },
			func(m *Market) { m.Positions = []int{1, 2} },
			func(m *Market) { m.Positions = []int{1, 1, 1} },
			func(m *Market) { m.PushValues = []int{3} },
			func(m *Market) { m.SmallMax = 18 },
		} {
			bad := m
			mutate(&bad)
			if _, err := Evaluate(bad, g, r); err == nil {
				t.Fatal("altered dice rule accepted", bad)
			}
		}
		for _, numbers := range [][]int{{0, 1, 1}, {1, 1, 7}, {1, 1}, {1, 1, 1, 1}} {
			invalid := r
			invalid.Numbers = numbers
			if _, err := Evaluate(m, g, invalid); err == nil {
				t.Fatal("invalid dice accepted", numbers)
			}
		}
		other, _ := games.Find("ffc-1")
		if _, err := Evaluate(m, other, result(other.ID, []int{1, 1, 1, 1, 1})); err == nil {
			t.Fatal("dice market accepted for another game", id)
		}
	}
}

func TestQuickThreeExistingSnapshotsKeepTheirRules(t *testing.T) {
	g, _ := games.Find("k3-1")
	for _, tc := range []struct {
		market  Market
		numbers []int
		want    string
	}{
		{Market{ID: "sum.small", Kind: "size", Pick: "small", Positions: []int{1, 2, 3}, SmallMax: 10, BigMin: 11, RuleVersion: 1}, []int{1, 1, 1}, "win"},
		{Market{ID: "sum.even", Kind: "parity", Pick: "even", Positions: []int{1, 2, 3}, RuleVersion: 1}, []int{6, 6, 6}, "win"},
		{Market{ID: "p2.n2", Kind: "number", Pick: "2", Positions: []int{2}, RuleVersion: 1}, []int{1, 2, 3}, "win"},
		{Market{ID: "sum.n18", Kind: "number", Pick: "18", Positions: []int{1, 2, 3}, RuleVersion: 1}, []int{6, 6, 6}, "win"},
	} {
		got, err := Evaluate(tc.market, g, result(g.ID, tc.numbers))
		if err != nil || got != tc.want {
			t.Fatal("old order rule changed", tc.market.ID, got, err)
		}
	}
}

func TestQuickThreeV2SingleDoubleSnapshotsKeepTripleWins(t *testing.T) {
	g, _ := games.Find("k3-1")
	count := 0
	for _, old := range quickThreeV2Catalog() {
		if old.Kind != "dice_face" && old.Kind != "dice_double" {
			continue
		}
		count++
		n, _ := strconv.Atoi(old.Pick)
		round := result(g.ID, []int{n, n, n})
		got, err := Evaluate(old, g, round)
		if err != nil || got != "win" {
			t.Fatalf("old %s: %s %v", old.ID, got, err)
		}
		var current Market
		for _, historical := range quickThreeV3Catalog() {
			if historical.ID == strings.Replace(old.ID, "k3v2.", "k3v3.", 1) {
				current = historical
				break
			}
		}
		if current.ID == "" {
			t.Fatal("missing v3 historical contract", old.ID)
		}
		got, err = Evaluate(current, g, round)
		if err != nil || got != "lose" {
			t.Fatalf("new %s: %s %v", current.ID, got, err)
		}
		for _, version := range []int{1, 2, 4} {
			bad := current
			bad.RuleVersion = version
			if _, err := Evaluate(bad, g, round); err == nil {
				t.Fatal("altered version accepted", bad)
			}
		}
	}
	if count != 12 {
		t.Fatal("must replace exactly twelve markets", count)
	}
}

func TestQuickThreeRetiredPairsPreserveEveryOutcome(t *testing.T) {
	g, _ := games.Find("k3-1")
	retired := []Market{}
	for _, m := range quickThreeV2Catalog() {
		if m.Kind == "dice_pair" || m.Kind == "dice_double" {
			retired = append(retired, m)
		}
	}
	for _, m := range quickThreeV3Catalog() {
		if m.Kind == "dice_double" {
			retired = append(retired, m)
		}
	}
	if len(retired) != 27 {
		t.Fatal("expected 21 retired offerings plus 6 older double contracts", len(retired))
	}
	for _, m := range retired {
		if _, ok := Find("k3", m.ID); ok {
			t.Fatal("retired market still offered", m.ID)
		}
		wins := 0
		for a := 1; a <= 6; a++ {
			for b := 1; b <= 6; b++ {
				for c := 1; c <= 6; c++ {
					count := [7]int{}
					count[a]++
					count[b]++
					count[c]++
					expected := false
					if m.Kind == "dice_pair" {
						var left, right int
						if _, err := fmt.Sscanf(m.Pick, "%d,%d", &left, &right); err != nil {
							t.Fatal(err)
						}
						expected = count[left] > 0 && count[right] > 0
					} else {
						n, err := strconv.Atoi(m.Pick)
						if err != nil {
							t.Fatal(err)
						}
						expected = count[n] >= 2
						if m.RuleVersion == 3 {
							expected = count[n] == 2
						}
					}
					want := "lose"
					if expected {
						want = "win"
						wins++
					}
					got, err := Evaluate(m, g, result(g.ID, []int{a, b, c}))
					if err != nil || got != want {
						t.Fatalf("%s %v got %s %v want %s", m.ID, []int{a, b, c}, got, err, want)
					}
				}
			}
		}
		expectedWins := 30
		if m.Kind == "dice_double" {
			expectedWins = 16
			if m.RuleVersion == 3 {
				expectedWins = 15
			}
		}
		if wins != expectedWins {
			t.Fatal("historical winning outcomes changed", m.ID, wins)
		}
	}
}
