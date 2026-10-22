package markets

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

var v6At = time.Date(2026, 10, 18, 12, 20, 5, 0, model.Beijing)
var v6HorseChart = [12]int{6, 5, 4, 3, 2, 1, 0, 11, 10, 9, 8, 7}
var v6Colors = map[string][]int{
	"red":   {1, 2, 7, 8, 12, 13, 18, 19, 23, 24, 29, 30, 34, 35, 40, 45, 46},
	"blue":  {3, 4, 9, 10, 14, 15, 20, 25, 26, 31, 36, 37, 41, 42, 47, 48},
	"green": {5, 6, 11, 16, 17, 21, 22, 27, 28, 32, 33, 38, 39, 43, 44, 49},
}
var v6TestCatalog = func() map[string]Market {
	out := map[string]Market{}
	for _, m := range Catalog("lhc") {
		out[m.ID] = m
	}
	return out
}()

func v6Template(t *testing.T, id string) Market {
	t.Helper()
	m, ok := v6TestCatalog[id]
	if !ok {
		t.Fatal("missing", id)
	}
	return m
}
func v6Round(numbers []int) (games.Session, games.Round) {
	g, _ := games.Find("lhc-tw")
	return g, games.Round{SessionID: g.ID, Issue: v6At.Format(model.IssueLayout), SourceIssue: v6At.Format(model.IssueLayout), SourceGroup: g.Group, DrawAt: v6At, Origin: "scheduled", Numbers: numbers}
}
func v6Eval(t *testing.T, id string, values, numbers []int) Evaluation {
	t.Helper()
	m, err := BindSelection(v6Template(t, id), values, v6At)
	if err != nil {
		t.Fatal(id, values, err)
	}
	g, r := v6Round(numbers)
	got, err := EvaluateDetailed(m, g, r)
	if err != nil {
		t.Fatal(id, values, numbers, err)
	}
	return got
}
func v6Check(t *testing.T, id string, values, numbers []int, want Evaluation) {
	t.Helper()
	got := v6Eval(t, id, values, numbers)
	if got != want {
		t.Fatalf("%s values=%v numbers=%v got=%+v want=%+v", id, values, numbers, got, want)
	}
}
func v6Want(win bool) Evaluation {
	if win {
		return Evaluation{Outcome: "win", Multiplier: 1}
	}
	return Evaluation{Outcome: "lose"}
}
func v6WithNumber(p, n int) []int {
	out := make([]int, 7)
	out[p-1] = n
	next := 1
	for i := range out {
		if i == p-1 {
			continue
		}
		if next == n {
			next++
		}
		out[i] = next
		next++
	}
	return out
}
func v6Color(n int) string {
	for c, values := range v6Colors {
		for _, v := range values {
			if n == v {
				return c
			}
		}
	}
	panic("invalid reference number")
}
func v6Clone(m Market) Market {
	b, _ := json.Marshal(m)
	var c Market
	_ = json.Unmarshal(b, &c)
	return c
}

func TestMarkSixV6CatalogScopeAndLegacySalesIsolation(t *testing.T) {
	catalog := Catalog("lhc")
	if len(catalog) != 630 {
		t.Fatalf("expected full 630 item catalog; got %d", len(catalog))
	}
	seen := map[string]bool{}
	tiers, counted, oldNumbers := 0, 0, 0
	for _, m := range catalog {
		if seen[m.ID] {
			t.Fatal("duplicate", m.ID)
		}
		seen[m.ID] = true
		if m.RuleVersion == 1 {
			oldNumbers++
			old, ok := findLegacyMarkSix(m.ID)
			if !ok || old.Kind != "number" || m.Kind != old.Kind || m.Pick != old.Pick || !reflect.DeepEqual(m.Positions, old.Positions) || m.DefaultOdds != old.DefaultOdds || m.DefaultOdds != 48020 || m.Selection != nil || len(m.PushValues) != 0 {
				t.Fatal("exact contract changed", m)
			}
			continue
		}
		if m.RuleVersion != 6 || !strings.HasPrefix(m.ID, "lhc6.") || m.DefaultOdds != 1000 {
			t.Fatal("unexpected current rule", m)
		}
		if len(m.PayoutTiers) > 0 {
			tiers++
			if len(m.PayoutTiers) != 2 {
				t.Fatal(m)
			}
			for _, tier := range m.PayoutTiers {
				if tier.DefaultOdds != 1000 {
					t.Fatal(tier)
				}
			}
		}
		if m.MaxPayoutMultiplier != 0 {
			counted++
			if m.Kind != "lhc_zodiac_count" || m.MaxPayoutMultiplier != 5 {
				t.Fatal(m)
			}
		}
	}
	if oldNumbers != 343 || tiers != 2 || counted != 12 {
		t.Fatal("wrong exact/tier/count market coverage", oldNumbers, tiers, counted)
	}
	for _, id := range []string{"lhc6.link.four_one", "lhc6.miss.12", "lhc6.link_zodiac.6", "lhc6.link_tail.6", "lhc6.combined_zodiac.out.2", "lhc5.combined_zodiac.in.2", "size2.p7.big", "parity2.p1.odd"} {
		if _, ok := Find("lhc", id); ok {
			t.Fatal("retired or excluded market sold", id)
		}
	}
	if len(LegacyMarkSixCatalog()) != 381 || len(Catalog("lhc-au")) != 392 || len(Catalog("k3")) != 31 {
		t.Fatal("legacy/other game scope changed")
	}
}

func TestMarkSixV6EveryPositionEveryNumber(t *testing.T) {
	for p := 1; p <= 7; p++ {
		prefix := fmt.Sprintf("lhc6.p%d", p)
		if p == 7 {
			prefix = "lhc6.special"
		}
		for n := 1; n <= 49; n++ {
			numbers := v6WithNumber(p, n)
			digit := n/10 + n%10
			for _, tc := range []struct {
				suffix string
				win    bool
			}{
				{"big", n >= 25}, {"small", n <= 24}, {"odd", n%2 == 1}, {"even", n%2 == 0},
				{"digit.big", digit >= 7}, {"digit.small", digit <= 6}, {"digit.odd", digit%2 == 1}, {"digit.even", digit%2 == 0},
				{"tail.big", n%10 >= 5}, {"tail.small", n%10 <= 4},
			} {
				want := v6Want(tc.win)
				if n == 49 {
					want = Evaluation{Outcome: "push"}
				}
				v6Check(t, prefix+"."+tc.suffix, nil, numbers, want)
			}
			for _, color := range []string{"red", "blue", "green"} {
				v6Check(t, prefix+".color."+color, nil, numbers, v6Want(v6Color(n) == color))
			}
			v6Check(t, fmt.Sprintf("p%d.n%d", p, n), nil, numbers, v6Want(true))
		}
	}
}

func TestMarkSixV6SpecialCompositesHeadTailElementsAndZodiacGroups(t *testing.T) {
	elements := map[string][]int{
		"metal": {4, 5, 12, 13, 26, 27, 34, 35, 42, 43}, "wood": {8, 9, 16, 17, 24, 25, 38, 39, 46, 47}, "water": {1, 14, 15, 22, 23, 30, 31, 44, 45}, "fire": {2, 3, 10, 11, 18, 19, 32, 33, 40, 41, 48, 49}, "earth": {6, 7, 20, 21, 28, 29, 36, 37},
	}
	groups := map[string][]int{"sky": {1, 3, 4, 6, 8, 11}, "earth": {0, 2, 5, 7, 9, 10}, "front": {0, 1, 2, 3, 4, 5}, "back": {6, 7, 8, 9, 10, 11}, "domestic": {1, 6, 7, 9, 10, 11}, "wild": {0, 2, 3, 4, 5, 8}}
	contains := func(values []int, n int) bool {
		for _, v := range values {
			if v == n {
				return true
			}
		}
		return false
	}
	for n := 1; n <= 49; n++ {
		numbers := v6WithNumber(7, n)
		for _, size := range []string{"big", "small"} {
			for _, parity := range []string{"odd", "even"} {
				match := (size == "big" && n >= 25 || size == "small" && n <= 24) && (parity == "odd" && n%2 == 1 || parity == "even" && n%2 == 0)
				want := v6Want(match)
				if n == 49 {
					want = Evaluation{Outcome: "push"}
				}
				v6Check(t, "lhc6.special.combo."+size+"_"+parity, nil, numbers, want)
				for _, color := range []string{"red", "blue", "green"} {
					want := v6Want(match && v6Color(n) == color)
					if n == 49 {
						want = Evaluation{Outcome: "push"}
					}
					v6Check(t, "lhc6.special.halfhalf."+color+"_"+size+"_"+parity, nil, numbers, want)
				}
			}
		}
		for _, color := range []string{"red", "blue", "green"} {
			for _, face := range []struct {
				pick string
				win  bool
			}{{"big", n >= 25}, {"small", n <= 24}, {"odd", n%2 == 1}, {"even", n%2 == 0}} {
				want := v6Want(face.win && v6Color(n) == color)
				if n == 49 {
					want = Evaluation{Outcome: "push"}
				}
				v6Check(t, "lhc6.special.half."+color+"_"+face.pick, nil, numbers, want)
			}
		}
		for head := 0; head < 5; head++ {
			v6Check(t, fmt.Sprintf("lhc6.special.head.n%d", head), nil, numbers, v6Want(n/10 == head))
		}
		for tail := 0; tail < 10; tail++ {
			v6Check(t, fmt.Sprintf("lhc6.special.tail.n%d", tail), nil, numbers, v6Want(n%10 == tail))
		}
		matches := 0
		for element, values := range elements {
			match := contains(values, n)
			if match {
				matches++
			}
			v6Check(t, "lhc6.special.element."+element, nil, numbers, v6Want(match))
		}
		if matches != 1 {
			t.Fatal("reference element coverage", n, matches)
		}
		zodiac := v6HorseChart[(n-1)%12]
		for group, values := range groups {
			want := v6Want(contains(values, zodiac))
			if n == 49 {
				want = Evaluation{Outcome: "push"}
			}
			v6Check(t, "lhc6.special.zodiac_group."+group, nil, numbers, want)
		}
		for z := 0; z < 12; z++ {
			v6Check(t, fmt.Sprintf("lhc6.special.zodiac.z%d", z), nil, numbers, v6Want(zodiac == z))
		}
	}
}

func TestMarkSixV6Sum174175AndOrdinary49(t *testing.T) {
	for _, numbers := range [][]int{{1, 2, 3, 4, 5, 6, 7}, {2, 8, 17, 28, 39, 46, 34}, {2, 8, 17, 28, 39, 46, 35}, {43, 44, 45, 46, 47, 48, 49}} {
		sum := 0
		for _, n := range numbers {
			sum += n
		}
		for _, tc := range []struct {
			pick string
			win  bool
		}{{"big", sum >= 175}, {"small", sum <= 174}, {"odd", sum%2 == 1}, {"even", sum%2 == 0}} {
			v6Check(t, "lhc6.sum."+tc.pick, nil, numbers, v6Want(tc.win))
		}
	}
}

func TestMarkSixV6ZodiacRepeatedPayoutAndAnyRegularNumber(t *testing.T) {
	for zodiac := 0; zodiac < 12; zodiac++ {
		members, other := []int{}, []int{}
		for n := 1; n <= 49; n++ {
			if v6HorseChart[(n-1)%12] == zodiac {
				members = append(members, n)
			} else {
				other = append(other, n)
			}
		}
		for count := 0; count <= len(members); count++ {
			regular := append([]int(nil), members[:count]...)
			regular = append(regular, other[:6-count]...)
			special := other[6-count]
			if count < len(members) {
				special = members[count]
			}
			numbers := append(append([]int(nil), regular...), special)
			expected := v6Want(count > 0)
			if count > 0 {
				expected.Multiplier = int64(count)
			}
			v6Check(t, fmt.Sprintf("lhc6.regular.zodiac.z%d", zodiac), nil, numbers, expected)
			v6Check(t, fmt.Sprintf("lhc6.any.zodiac.z%d", zodiac), nil, numbers, v6Want(count > 0 || v6HorseChart[(special-1)%12] == zodiac))
		}
	}
	numbers := []int{10, 20, 30, 40, 1, 11, 49}
	for tail := 0; tail < 10; tail++ {
		v6Check(t, fmt.Sprintf("lhc6.any.tail.t%d", tail), nil, numbers, v6Want(tail == 0 || tail == 1 || tail == 9))
	}
	for n := 1; n <= 49; n++ {
		hit := false
		for _, r := range numbers[:6] {
			hit = hit || n == r
		}
		v6Check(t, fmt.Sprintf("lhc6.regular.n%d", n), nil, numbers, v6Want(hit))
	}
	// The only horse is special 49: any zodiac wins, regular zodiac loses.
	numbers = []int{2, 3, 4, 5, 6, 7, 49}
	v6Check(t, "lhc6.any.zodiac.z6", nil, numbers, v6Want(true))
	v6Check(t, "lhc6.regular.zodiac.z6", nil, numbers, v6Want(false))
}

func TestMarkSixV6SevenWaveAllColorPatterns(t *testing.T) {
	colors := []string{"red", "blue", "green"}
	for code := 0; code < 2187; code++ {
		cursor := code
		used := [3]int{}
		numbers := []int{}
		regular := [3]int{}
		special := 0
		for i := 0; i < 7; i++ {
			c := cursor % 3
			cursor /= 3
			numbers = append(numbers, v6Colors[colors[c]][used[c]])
			used[c]++
			if i < 6 {
				regular[c]++
			} else {
				special = c
			}
		}
		scores := [3]float64{float64(regular[0]), float64(regular[1]), float64(regular[2])}
		scores[special] += 1.5
		tied := scores[0] == scores[1] && scores[0] > scores[2] || scores[0] == scores[2] && scores[0] > scores[1] || scores[1] == scores[2] && scores[1] > scores[0]
		for i, color := range colors {
			win := scores[i] > scores[(i+1)%3] && scores[i] > scores[(i+2)%3]
			want := v6Want(win)
			if tied {
				want = Evaluation{Outcome: "push"}
			}
			v6Check(t, "lhc6.seven_wave."+color, nil, numbers, want)
		}
		v6Check(t, "lhc6.seven_wave.tie", nil, numbers, v6Want(tied))
	}
}

func TestMarkSixV6TotalZodiacTwoThroughSeven(t *testing.T) {
	cases := [][]int{{1, 13, 25, 37, 49, 2, 14}, {1, 13, 25, 37, 2, 14, 3}, {1, 13, 25, 2, 14, 3, 4}, {1, 13, 2, 14, 3, 4, 5}, {1, 13, 2, 3, 4, 5, 6}, {1, 2, 3, 4, 5, 6, 7}}
	for index, numbers := range cases {
		count := index + 2
		for n := 2; n <= 7; n++ {
			v6Check(t, fmt.Sprintf("lhc6.total_zodiac.n%d", n), nil, numbers, v6Want(n == count))
		}
		v6Check(t, "lhc6.total_zodiac.odd", nil, numbers, v6Want(count%2 == 1))
		v6Check(t, "lhc6.total_zodiac.even", nil, numbers, v6Want(count%2 == 0))
	}
}

func TestMarkSixV6ThreeTwoEverySelectionHasOnlyOneTier(t *testing.T) {
	numbers := []int{1, 13, 25, 37, 49, 6, 2}
	regular := map[int]bool{1: true, 13: true, 25: true, 37: true, 49: true, 6: true}
	for a := 1; a <= 47; a++ {
		for b := a + 1; b <= 48; b++ {
			for c := b + 1; c <= 49; c++ {
				count := 0
				for _, v := range []int{a, b, c} {
					if regular[v] {
						count++
					}
				}
				want := Evaluation{Outcome: "lose"}
				if count == 2 {
					want = Evaluation{Outcome: "win", Tier: "hit2", Multiplier: 1}
				}
				if count == 3 {
					want = Evaluation{Outcome: "win", Tier: "hit3", Multiplier: 1}
				}
				v6Check(t, "lhc6.link.three_two", []int{a, b, c}, numbers, want)
			}
		}
	}
	v6Check(t, "lhc6.link.three_two", []int{1, 13, 2}, numbers, Evaluation{Outcome: "win", Tier: "hit2", Multiplier: 1})
}
func TestMarkSixV6TwoSpecialEverySelectionAndOtherLinks(t *testing.T) {
	numbers := []int{1, 13, 25, 37, 49, 6, 2}
	regular := map[int]bool{1: true, 13: true, 25: true, 37: true, 49: true, 6: true}
	for a := 1; a <= 48; a++ {
		for b := a + 1; b <= 49; b++ {
			want := Evaluation{Outcome: "lose"}
			if regular[a] && regular[b] {
				want = Evaluation{Outcome: "win", Tier: "hit2", Multiplier: 1}
			}
			if a == 2 && regular[b] || b == 2 && regular[a] {
				want = Evaluation{Outcome: "win", Tier: "special", Multiplier: 1}
			}
			v6Check(t, "lhc6.link.two_special", []int{a, b}, numbers, want)
			v6Check(t, "lhc6.link.two_all", []int{a, b}, numbers, v6Want(regular[a] && regular[b]))
			v6Check(t, "lhc6.link.special_pair", []int{a, b}, numbers, v6Want(a == 2 && regular[b] || b == 2 && regular[a]))
		}
	}
	v6Check(t, "lhc6.link.three_all", []int{1, 13, 49}, numbers, v6Want(true))
	v6Check(t, "lhc6.link.three_all", []int{1, 13, 2}, numbers, v6Want(false))
	v6Check(t, "lhc6.link.four_all", []int{1, 13, 25, 49}, numbers, v6Want(true))
	v6Check(t, "lhc6.link.four_all", []int{1, 13, 25, 2}, numbers, v6Want(false))
}

func TestMarkSixV6CombinedAllSizesAllSpecialsAnd49Push(t *testing.T) {
	for count := 2; count <= 11; count++ {
		for offset := 0; offset < 12; offset++ {
			values, selected := []int{}, map[int]bool{}
			for i := 0; i < count; i++ {
				v := (offset + i) % 12
				values = append(values, v)
				selected[v] = true
			}
			for special := 1; special <= 49; special++ {
				numbers := v6WithNumber(7, special)
				want := v6Want(selected[v6HorseChart[(special-1)%12]])
				if special == 49 {
					want = Evaluation{Outcome: "push"}
				}
				v6Check(t, fmt.Sprintf("lhc6.combined_zodiac.in.%d", count), values, numbers, want)
			}
		}
	}
	// Regular balls alone never trigger combined-zodiac payout.
	v6Check(t, "lhc6.combined_zodiac.in.2", []int{5, 6}, []int{1, 13, 25, 37, 2, 14, 7}, v6Want(false))
	v6Check(t, "lhc6.combined_zodiac.in.2", []int{0, 1}, []int{1, 13, 25, 37, 2, 14, 49}, Evaluation{Outcome: "push"})
}
func TestMarkSixV6LinkedClassesAndMissFullScope(t *testing.T) {
	for count := 2; count <= 5; count++ {
		// Zodiac numbers 1..6 represent horse through ox; 49 supplies horse again.
		numbers := []int{1, 2, 3, 4, 5, 6, 49}
		values := []int{}
		for i := 0; i < count; i++ {
			values = append(values, 6-i)
		}
		v6Check(t, fmt.Sprintf("lhc6.link_zodiac.%d", count), values, numbers, v6Want(true))
		missing := append([]int(nil), values...)
		missing[len(missing)-1] = 0
		v6Check(t, fmt.Sprintf("lhc6.link_zodiac.%d", count), missing, numbers, v6Want(false))
		tails := []int{9}
		for i := 0; i < count-1; i++ {
			tails = append(tails, i)
		}
		numbers = []int{10, 21, 32, 43, 14, 25, 49}
		v6Check(t, fmt.Sprintf("lhc6.link_tail.%d", count), tails, numbers, v6Want(true))
		missing = append([]int(nil), tails...)
		missing[len(missing)-1] = 8
		v6Check(t, fmt.Sprintf("lhc6.link_tail.%d", count), missing, numbers, v6Want(false))
	}
	// Many matching horse balls do not stand in for the missing rat class.
	v6Check(t, "lhc6.link_zodiac.2", []int{0, 6}, []int{1, 13, 25, 37, 49, 6, 2}, v6Want(false))
	for count := 5; count <= 11; count++ {
		values := []int{}
		for i := 0; i < count; i++ {
			values = append(values, 10+i)
		}
		numbers := []int{1, 2, 3, 4, 5, 6, 49}
		v6Check(t, fmt.Sprintf("lhc6.miss.%d", count), values, numbers, v6Want(true))
		values[count-1] = 49
		v6Check(t, fmt.Sprintf("lhc6.miss.%d", count), values, numbers, v6Want(false))
	}
	// A 7-number five-not-hit pool expands to 21 wagers; here just one wins.
	pool := []int{1, 12, 15, 19, 32, 38, 40}
	draw := []int{19, 24, 17, 34, 40, 39, 49}
	wins, total := 0, 0
	for a := 0; a < 3; a++ {
		for b := a + 1; b < 4; b++ {
			for c := b + 1; c < 5; c++ {
				for d := c + 1; d < 6; d++ {
					for e := d + 1; e < 7; e++ {
						total++
						got := v6Eval(t, "lhc6.miss.5", []int{pool[a], pool[b], pool[c], pool[d], pool[e]}, draw)
						if got.Outcome == "win" {
							wins++
						}
					}
				}
			}
		}
	}
	if total != 21 || wins != 1 {
		t.Fatal("independent combo count or settlement wrong", total, wins)
	}
}

func TestMarkSixV6BindingCanonicalSetsAndAllSchemas(t *testing.T) {
	for _, m := range Catalog("lhc") {
		if m.RuleVersion != 6 {
			continue
		}
		if m.Selection == nil {
			if _, err := BindSelection(m, []int{1}, v6At); err == nil {
				t.Fatal("fixed pick accepted selection", m.ID)
			}
			bound, err := BindSelection(m, nil, v6At)
			if err != nil {
				t.Fatal(m.ID, err)
			}
			if markSixV6ZodiacMarket(m) && bound.ZodiacYear != 2026 {
				t.Fatal("fixed zodiac not bound", bound)
			}
			continue
		}
		values := []int{}
		for i := 0; i < m.Selection.PickCount; i++ {
			values = append(values, m.Selection.MinValue+i)
		}
		reverse := append([]int(nil), values...)
		sort.Sort(sort.Reverse(sort.IntSlice(reverse)))
		bound, err := BindSelection(m, reverse, v6At)
		if err != nil || !reflect.DeepEqual(bound.SelectionValues, values) {
			t.Fatal(m.ID, bound, err)
		}
		if len(values) > 1 && reverse[0] != values[len(values)-1] {
			t.Fatal("bind mutated input")
		}
		invalid := [][]int{nil, values[:len(values)-1], append(append([]int(nil), values...), m.Selection.MaxValue)}
		duplicate := append([]int(nil), values...)
		duplicate[1] = duplicate[0]
		invalid = append(invalid, duplicate)
		low := append([]int(nil), values...)
		low[0] = m.Selection.MinValue - 1
		invalid = append(invalid, low)
		high := append([]int(nil), values...)
		high[0] = m.Selection.MaxValue + 1
		invalid = append(invalid, high)
		for _, bad := range invalid {
			if _, err := BindSelection(m, bad, v6At); err == nil {
				t.Fatal("invalid exact selection accepted", m.ID, bad)
			}
		}
	}
	for _, tc := range []struct {
		id     string
		values []int
		label  string
	}{
		{"lhc6.link.two_all", []int{49, 1}, "01 / 49"}, {"lhc6.link_tail.2", []int{9, 0}, "0尾 / 9尾"}, {"lhc6.combined_zodiac.in.2", []int{6, 0}, "鼠 / 马"},
	} {
		m, err := BindSelection(v6Template(t, tc.id), tc.values, v6At)
		if err != nil || SelectionLabel(m) != tc.label {
			t.Fatal(tc, m, err, SelectionLabel(m))
		}
	}
}

func TestMarkSixV6RejectsTamperedContractsSnapshotsAndSources(t *testing.T) {
	original := v6Template(t, "lhc6.link.three_two")
	mutations := []struct {
		name string
		fn   func(*Market)
	}{
		{"id", func(m *Market) { m.ID = "lhc6.link.three_all" }},
		{"kind", func(m *Market) { m.Kind = "lhc_miss" }},
		{"pick", func(m *Market) { m.Pick = "three_all" }},
		{"version", func(m *Market) { m.RuleVersion = 1 }},
		{"positions", func(m *Market) { m.Positions = []int{7} }},
		{"position order", func(m *Market) { m.Positions[0], m.Positions[6] = m.Positions[6], m.Positions[0] }},
		{"push", func(m *Market) { m.PushValues = []int{49} }},
		{"small", func(m *Market) { m.SmallMax = 6 }},
		{"big", func(m *Market) { m.BigMin = 7 }},
		{"max multiplier", func(m *Market) { m.MaxPayoutMultiplier = 10 }},
		{"missing tiers", func(m *Market) { m.PayoutTiers = nil }},
		{"duplicate tiers", func(m *Market) { m.PayoutTiers[1].Key = m.PayoutTiers[0].Key }},
		{"tier key", func(m *Market) { m.PayoutTiers[0].Key = "other" }},
		{"tier order", func(m *Market) { m.PayoutTiers[0], m.PayoutTiers[1] = m.PayoutTiers[1], m.PayoutTiers[0] }},
		{"tier default", func(m *Market) { m.PayoutTiers[0].DefaultOdds = 1000000 }},
		{"tier label", func(m *Market) { m.PayoutTiers[0].Label = "中三" }},
		{"schema nil", func(m *Market) { m.Selection = nil }},
		{"schema type", func(m *Market) { m.Selection.Type = "zodiac" }},
		{"schema mode", func(m *Market) { m.Selection.Mode = "set" }},
		{"schema count", func(m *Market) { m.Selection.PickCount = 2 }},
		{"schema min", func(m *Market) { m.Selection.MinValue = 0 }},
		{"schema max", func(m *Market) { m.Selection.MaxValue = 50 }},
		{"schema max picks", func(m *Market) { m.Selection.MaxPicks = 50 }},
	}
	g, r := v6Round([]int{1, 2, 3, 4, 5, 6, 49})
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			bad := v6Clone(original)
			tc.fn(&bad)
			if _, err := BindSelection(bad, []int{1, 2, 3}, v6At); err == nil {
				t.Fatal("bad contract bound")
			}
			bad.SelectionValues = []int{1, 2, 3}
			if _, err := EvaluateDetailed(bad, g, r); err == nil {
				t.Fatal("bad contract evaluated")
			}
		})
	}
	m, err := BindSelection(original, []int{1, 2, 3}, v6At)
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]int{nil, {1, 2}, {1, 2, 2}, {3, 2, 1}, {0, 1, 2}, {1, 2, 50}} {
		bad := v6Clone(m)
		bad.SelectionValues = values
		if _, err := EvaluateDetailed(bad, g, r); err == nil {
			t.Fatal("invalid snapshot values accepted", values)
		}
	}
	bad := v6Clone(m)
	bad.ZodiacYear = 2026
	if _, err := EvaluateDetailed(bad, g, r); err == nil {
		t.Fatal("non-zodiac year accepted")
	}
	counted, err := BindSelection(v6Template(t, "lhc6.regular.zodiac.z6"), nil, v6At)
	if err != nil {
		t.Fatal(err)
	}
	counted.MaxPayoutMultiplier = 6
	if _, err := EvaluateDetailed(counted, g, r); err == nil {
		t.Fatal("inflated repeated payout accepted")
	}
	for _, mutate := range []func(*games.Round){
		func(r *games.Round) { r.SessionID = "lhc-au" }, func(r *games.Round) { r.SourceGroup = "g1" }, func(r *games.Round) { r.SourceIssue = "wrong" }, func(r *games.Round) { r.Issue = "wrong" }, func(r *games.Round) { r.Origin = "manual" }, func(r *games.Round) { r.DrawAt = r.DrawAt.Add(time.Second) },
	} {
		bad := r
		mutate(&bad)
		if _, err := EvaluateDetailed(m, g, bad); err == nil {
			t.Fatal("invalid result source accepted", bad)
		}
	}
	for _, numbers := range [][]int{nil, {1, 2, 3, 4, 5, 6}, {1, 2, 3, 4, 5, 6, 7, 8}, {0, 2, 3, 4, 5, 6, 49}, {1, 2, 3, 4, 5, 6, 50}, {1, 2, 3, 4, 5, 6, 6}} {
		bad := r
		bad.Numbers = numbers
		if _, err := EvaluateDetailed(m, g, bad); err == nil {
			t.Fatal("invalid full result accepted", numbers)
		}
	}
	utc := r
	utc.DrawAt = r.DrawAt.UTC()
	if _, err := EvaluateDetailed(m, g, utc); err != nil {
		t.Fatal("same instant in UTC rejected", err)
	}
}

func TestMarkSixV6LunarYearBindingAndOldSnapshotsRemainIndependent(t *testing.T) {
	for _, tc := range []struct {
		at           time.Time
		year, zodiac int
	}{
		{time.Date(2026, 2, 16, 23, 40, 5, 0, model.Beijing), 2025, 5}, {time.Date(2026, 2, 17, 0, 0, 5, 0, model.Beijing), 2026, 6},
	} {
		m, err := BindSelection(v6Template(t, fmt.Sprintf("lhc6.special.zodiac.z%d", tc.zodiac)), nil, tc.at.UTC())
		if err != nil || m.ZodiacYear != tc.year {
			t.Fatal(m, err)
		}
		got, err := evaluateMarkSixV6(m, []int{2, 3, 4, 5, 6, 7, 1}, tc.at)
		if err != nil || got != v6Want(true) {
			t.Fatal(got, err)
		}
		m.ZodiacYear++
		if _, err := evaluateMarkSixV6(m, []int{2, 3, 4, 5, 6, 7, 1}, tc.at); err == nil {
			t.Fatal("forged lunar year accepted")
		}
		c := MarkSixContextAt(tc.at.UTC())
		if c.LunarYear != tc.year || c.Issue != tc.at.Format(model.IssueLayout) || c.Numbers[0].Zodiac != tc.zodiac {
			t.Fatal("context mismatch", c)
		}
	}
	g, r := v6Round([]int{1, 2, 3, 4, 5, 6, 49})
	oldCombined, _ := findLegacyMarkSix("lhc5.combined_zodiac.in.2")
	oldCombined, err := BindSelection(oldCombined, []int{5, 6}, v6At)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EvaluateDetailed(oldCombined, g, r)
	if err != nil || got != v6Want(true) {
		t.Fatal("v5 changed", got, err)
	}
	v6Check(t, "lhc6.combined_zodiac.in.2", []int{5, 6}, r.Numbers, Evaluation{Outcome: "push"})
	legacySize, _ := findLegacyMarkSix("size2.p7.big")
	legacyParity, _ := findLegacyMarkSix("parity2.p7.odd")
	old := []Market{legacySize, legacyParity, {ID: "p7.big", Kind: "size", Pick: "big", Positions: []int{7}, RuleVersion: 1, SmallMax: 24, BigMin: 25, PushValues: []int{49}}}
	wants := []Evaluation{v6Want(true), v6Want(true), {Outcome: "push"}}
	for i, m := range old {
		got, err := EvaluateDetailed(m, g, r)
		if err != nil || got != wants[i] {
			t.Fatal("legacy boundary changed", m, got, err)
		}
	}
	r.Numbers = []int{1, 2, 3, 4, 5, 6, 25}
	got, err = EvaluateDetailed(legacySize, g, r)
	if err != nil || got.Outcome != "push" {
		t.Fatal("legacy25 changed", got, err)
	}
	v6Check(t, "lhc6.special.big", nil, r.Numbers, v6Want(true))
	for _, s := range games.All() {
		if s.GameID != "lhc" {
			continue
		}
		at := s.Next(v6At)
		m, err := BindSelection(v6Template(t, "lhc6.special.digit.small"), nil, at)
		if err != nil {
			t.Fatal(err)
		}
		round := games.Round{SessionID: s.ID, Issue: at.Format(model.IssueLayout), SourceIssue: at.Format(model.IssueLayout), SourceGroup: s.Group, DrawAt: at, Origin: "scheduled", Numbers: []int{1, 2, 3, 4, 5, 6, 33}}
		got, err := EvaluateDetailed(m, s, round)
		if err != nil || got != v6Want(true) {
			t.Fatal("region rejected v6", s.ID, got, err)
		}
	}
}
