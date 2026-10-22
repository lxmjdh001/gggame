package markets

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"starlight/backend/internal/games"
)

func TestPK10ChampionRunnerUpSumCatalog(t *testing.T) {
	odds := map[int]int64{3: 39600, 4: 39600, 5: 19600, 6: 19600, 7: 12900, 8: 12900, 9: 9450, 10: 9450, 11: 7740, 12: 9450, 13: 9450, 14: 12900, 15: 12900, 16: 19800, 17: 19800, 18: 39600, 19: 39600}
	for n, want := range odds {
		m, ok := Find("pk10", "sum.n"+strconv.Itoa(n))
		if !ok || m.Kind != "number" || m.Pick != strconv.Itoa(n) || m.Label != fmt.Sprintf("%02d", n) || m.DefaultOdds != want {
			t.Fatal("incorrect exact sum market", n, m)
		}
		if m.Category != "regular" || m.Group != "冠亚和" || !reflect.DeepEqual(m.Positions, []int{1, 2}) || len(m.PushValues) != 0 || m.RuleVersion != Version || !strings.Contains(m.Rule, "并非额外赛车号码") {
			t.Fatal("incorrect sum rule", m)
		}
	}
	for _, tc := range []struct {
		id, pick string
		odds     int64
	}{
		{"sum.set_3_4_18_19", "3,4,18,19", 11800},
		{"sum.set_5_6_16_17", "5,6,16,17", 5400},
		{"sum.set_7_8_14_15", "7,8,14,15", 3600},
		{"sum.set_9_10_12_13", "9,10,12,13", 2700},
	} {
		m, ok := Find("pk10", tc.id)
		if !ok || m.Kind != "number_set" || m.Pick != tc.pick || m.DefaultOdds != tc.odds || m.Category != "regular" || m.Group != "冠亚和" || !reflect.DeepEqual(m.Positions, []int{1, 2}) || len(m.PushValues) != 0 || m.RuleVersion != Version || !strings.Contains(m.Rule, "每注仅派彩一次") || !strings.Contains(m.Rule, "并非额外赛车号码") {
			t.Fatal("incorrect set market", m)
		}
	}
	catalog := Catalog("pk10")
	if len(catalog) != 167 {
		t.Fatal("expected 146 existing and 21 new selections", len(catalog))
	}
	for _, m := range catalog[:146] {
		if m.Kind == "number_set" || m.Kind == "number" && len(m.Positions) != 1 {
			t.Fatal("new selections displaced the old catalog", m)
		}
		if m.Kind == "number" && m.DefaultOdds != 9800 || m.Kind != "number" && m.DefaultOdds != 1960 {
			t.Fatal("existing default odds changed", m)
		}
		if m.Kind == "compare" && !reflect.DeepEqual(m.Positions, []int{1, 10}) {
			t.Fatal("PK10 comparison expanded beyond first versus last", m)
		}
	}
	for _, game := range []string{"ssc", "ffc", "k3", "pc28", "lhc", "lhc-au"} {
		for _, m := range Catalog(game) {
			if m.Kind == "number_set" || strings.HasPrefix(m.ID, "sum.set_") {
				t.Fatal("PK10 sets leaked into another game", game, m)
			}
		}
	}
}

func TestPK10NewSumMarketsExhaustAllNinetyOrderedPairs(t *testing.T) {
	g, _ := games.Find("pk10-10")
	sets := map[string][]int{
		"sum.set_3_4_18_19": {3, 4, 18, 19}, "sum.set_5_6_16_17": {5, 6, 16, 17},
		"sum.set_7_8_14_15": {7, 8, 14, 15}, "sum.set_9_10_12_13": {9, 10, 12, 13},
	}
	wins := map[string]int{}
	pairs := 0
	for first := 1; first <= 10; first++ {
		for second := 1; second <= 10; second++ {
			if first == second {
				continue
			}
			pairs++
			numbers := []int{first, second}
			for n := 1; n <= 10; n++ {
				if n != first && n != second {
					numbers = append(numbers, n)
				}
			}
			round := result(g.ID, numbers)
			sum := first + second
			for n := 3; n <= 19; n++ {
				id := "sum.n" + strconv.Itoa(n)
				m, _ := Find("pk10", id)
				want := "lose"
				if sum == n {
					want = "win"
				}
				got, err := Evaluate(m, g, round)
				if err != nil || got != want {
					t.Fatal(first, second, id, got, err, want)
				}
				if got == "win" {
					wins[id]++
				}
			}
			for id, values := range sets {
				m, _ := Find("pk10", id)
				want := "lose"
				if slices.Contains(values, sum) {
					want = "win"
				}
				got, err := Evaluate(m, g, round)
				if err != nil || got != want {
					t.Fatal(first, second, id, got, err, want)
				}
				if got == "win" {
					wins[id]++
				}
			}
		}
	}
	if pairs != 90 || wins["sum.n11"] != 10 || wins["sum.set_3_4_18_19"] != 8 || wins["sum.set_5_6_16_17"] != 16 || wins["sum.set_7_8_14_15"] != 24 || wins["sum.set_9_10_12_13"] != 32 {
		t.Fatal("incorrect sum coverage", pairs, wins)
	}
}

func TestPK10NumberSetsRejectAlteredRulesAndOtherGames(t *testing.T) {
	g, _ := games.Find("pk10-10")
	m, _ := Find("pk10", "sum.set_3_4_18_19")
	round := result(g.ID, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	for name, change := range map[string]func(*Market){
		"different kind": func(m *Market) { m.Kind = "number" },
		"different id":   func(m *Market) { m.ID = "p1.n3" },
		"different pair": func(m *Market) { m.Positions = []int{1, 3} },
		"duplicate rank": func(m *Market) { m.Positions = []int{1, 1} },
		"extra rank":     func(m *Market) { m.Positions = []int{1, 2, 3} },
		"push value":     func(m *Market) { m.PushValues = []int{3} },
		"version":        func(m *Market) { m.RuleVersion = 2 },
		"arbitrary set":  func(m *Market) { m.ID, m.Pick = "sum.set_3_11", "3,11" },
		"duplicate sum":  func(m *Market) { m.ID, m.Pick = "sum.set_3_3_18_19", "3,3,18,19" },
	} {
		t.Run(name, func(t *testing.T) {
			altered := m
			change(&altered)
			if _, err := Evaluate(altered, g, round); err == nil {
				t.Fatal("altered set rule accepted", altered)
			}
		})
	}
	for _, other := range games.All() {
		if other.GameID == "pk10" {
			continue
		}
		rule := other.NumberRule()
		numbers := make([]int, rule.Count)
		for i := range numbers {
			numbers[i] = rule.Min + i
		}
		if _, err := Evaluate(m, other, result(other.ID, numbers)); err == nil || err.Error() != "不支持的冠亚和组合规则" {
			t.Fatal("PK10 set accepted outside PK10", other.ID, err)
		}
	}
	for _, numbers := range [][]int{{1, 1, 3, 4, 5, 6, 7, 8, 9, 10}, {0, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 11}, {1, 2, 3, 4, 5, 6, 7, 8, 9}} {
		if _, err := Evaluate(m, g, result(g.ID, numbers)); err == nil {
			t.Fatal("invalid race result accepted", numbers)
		}
	}
}
