package markets

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestArchivedCombinedCatalogKeepsLegacyMarkSixRules(t *testing.T) {
	catalog := LegacyMarkSixCatalog()
	if len(catalog) != 381 || len(Catalog("lhc-au")) != 392 || len(Catalog("k3")) != 31 {
		t.Fatal("release catalog scope changed")
	}
	legacy, combined := 0, 0
	for _, m := range catalog {
		if m.RuleVersion == 5 {
			combined++
			if m.Kind != "lhc_combined_zodiac" || !strings.HasPrefix(m.ID, "lhc5.combined_zodiac.in.") || m.Selection == nil || m.Selection.Mode != "set" || m.Selection.Type != "zodiac" || m.Selection.PickCount < 2 || m.Selection.PickCount > 11 || m.Selection.MaxPicks != m.Selection.PickCount || m.DefaultOdds != 1000 || !reflect.DeepEqual(m.Positions, []int{7}) || len(m.PushValues) != 0 {
				t.Fatal("unconfirmed market or invalid combined contract", m)
			}
			continue
		}
		legacy++
		if m.RuleVersion != 1 && m.RuleVersion != 2 || m.Selection != nil || m.ZodiacYear != 0 || len(m.SelectionValues) != 0 {
			t.Fatal("legacy market changed", m)
		}
		if m.Kind == "size" && (m.SmallMax != 24 || m.BigMin != 26 || !reflect.DeepEqual(m.PushValues, []int{25}) || !strings.HasPrefix(m.ID, "size2.")) {
			t.Fatal("legacy size boundaries changed", m)
		}
		if m.Kind == "parity" && (len(m.PushValues) != 0 || !strings.HasPrefix(m.ID, "parity2.")) {
			t.Fatal("legacy parity rules changed", m)
		}
	}
	if legacy != 371 || combined != 10 {
		t.Fatal("expected legacy371 + combined10", legacy, combined)
	}
	for _, id := range []string{"lhc4.p1.big", "lhc5.any.zodiac.z0", "lhc5.link.two_all", "lhc5.miss.5"} {
		if _, ok := findLegacyMarkSix(id); ok {
			t.Fatal("unconfirmed market offered", id)
		}
	}
}

func TestCombinedOnlyEverySetAndSpecialBall(t *testing.T) {
	g, _ := games.Find("lhc-tw")
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	for count := 2; count <= 11; count++ {
		template, ok := findLegacyMarkSix(fmt.Sprintf("lhc5.combined_zodiac.in.%d", count))
		if !ok {
			t.Fatal("missing template", count)
		}
		values := make([]int, count)
		for i := range values {
			values[i] = i
		}
		m, err := BindSelection(template, values, at)
		if err != nil {
			t.Fatal(err)
		}
		for special := 1; special <= 49; special++ {
			numbers := make([]int, 0, 7)
			for n := 1; len(numbers) < 6; n++ {
				if n != special {
					numbers = append(numbers, n)
				}
			}
			numbers = append(numbers, special)
			r := games.Round{SessionID: g.ID, Issue: at.Format(model.IssueLayout), SourceIssue: at.Format(model.IssueLayout), SourceGroup: g.Group, DrawAt: at, Origin: "scheduled", Numbers: numbers}
			want := "lose"
			if MarkSixZodiac(special, 2026) < count {
				want = "win"
			}
			got, err := Evaluate(m, g, r)
			if err != nil || got != want {
				t.Fatalf("%d zodiac set special %d: got %s/%v want %s", count, special, got, err, want)
			}
		}
	}
}

func TestCombinedOnlyRejectsAlteredSnapshotsAndOtherGames(t *testing.T) {
	g, _ := games.Find("lhc-tw")
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	template, _ := findLegacyMarkSix("lhc5.combined_zodiac.in.2")
	for _, values := range [][]int{nil, {1}, {1, 1}, {-1, 1}, {1, 12}, {1, 2, 3}} {
		if _, err := BindSelection(template, values, at); err == nil {
			t.Fatal("invalid set accepted", values)
		}
	}
	m, err := BindSelection(template, []int{6, 5}, at)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.SelectionValues, []int{5, 6}) || m.ZodiacYear != 2026 {
		t.Fatal("snapshot not canonical", m)
	}
	r := games.Round{SessionID: g.ID, Issue: at.Format(model.IssueLayout), SourceIssue: at.Format(model.IssueLayout), SourceGroup: g.Group, DrawAt: at, Origin: "scheduled", Numbers: []int{1, 13, 25, 37, 2, 14, 49}}
	for _, mutate := range []func(*Market){
		func(v *Market) { v.ID = "lhc5.any.zodiac.z0" },
		func(v *Market) { v.Kind = "lhc_zodiac" },
		func(v *Market) { v.Positions = []int{1} },
		func(v *Market) { v.PushValues = []int{49} },
		func(v *Market) { v.Pick = "out" },
		func(v *Market) { v.RuleVersion = 4 },
		func(v *Market) { v.ZodiacYear = 2025 },
		func(v *Market) { v.SelectionValues = []int{6, 5} },
		func(v *Market) { schema := *v.Selection; schema.Mode = "combination"; v.Selection = &schema },
	} {
		invalid := m
		mutate(&invalid)
		if _, err := Evaluate(invalid, g, r); err == nil {
			t.Fatal("altered snapshot accepted", invalid)
		}
	}
	for _, id := range []string{"lhc-au", "k3-1", "pk10-10", "ffc-1", "pc28-5"} {
		other, _ := games.Find(id)
		r2 := r
		r2.SessionID = id
		r2.DrawAt = other.Due(at)
		if _, err := Evaluate(m, other, r2); err == nil {
			t.Fatal("combined set accepted for other catalog", id)
		}
	}
	static, _ := findLegacyMarkSix("p7.n49")
	if _, err := BindSelection(static, []int{5, 6}, at); err == nil {
		t.Fatal("static market accepted set")
	}
}

func TestCombinedOnlyLunarBoundaryUsesDrawDate(t *testing.T) {
	before := time.Date(2026, 2, 16, 23, 40, 5, 0, model.Beijing)
	after := time.Date(2026, 2, 17, 0, 0, 5, 0, model.Beijing)
	template, _ := findLegacyMarkSix("lhc5.combined_zodiac.in.2")
	for _, tc := range []struct {
		at        time.Time
		year, one int
	}{{before, 2025, 5}, {after, 2026, 6}} {
		m, err := BindSelection(template, []int{0, 1}, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		context := MarkSixContextAt(tc.at.UTC())
		if m.ZodiacYear != tc.year || context.LunarYear != tc.year || MarkSixZodiac(1, tc.year) != tc.one || context.Issue != tc.at.Format(model.IssueLayout) {
			t.Fatal("lunar draw-year boundary is wrong", tc, m, context)
		}
		if len(context.Zodiacs) != 12 || len(context.Numbers) != 49 {
			t.Fatal("incomplete zodiac context")
		}
	}
}

// This table is transcribed from the confirmed horse-year chart rather than
// deriving expected outcomes with the production zodiac helper.
func TestCombinedIndependentAllSetSizesAndSpecialNumbers(t *testing.T) {
	g, _ := games.Find("lhc-tw")
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	horseYear := [...]int{6, 5, 4, 3, 2, 1, 0, 11, 10, 9, 8, 7}
	for count := 2; count <= 11; count++ {
		template, _ := findLegacyMarkSix(fmt.Sprintf("lhc5.combined_zodiac.in.%d", count))
		for offset := 0; offset < 12; offset++ {
			values, selected := make([]int, count), map[int]bool{}
			for i := range values {
				values[i] = (offset + i) % 12
				selected[values[i]] = true
			}
			original := append([]int(nil), values...)
			m, err := BindSelection(template, values, at)
			if err != nil || !reflect.DeepEqual(original, values) {
				t.Fatal("binding rejected or mutated a valid selected set", values, err)
			}
			for special := 1; special <= 49; special++ {
				want := "lose"
				if selected[horseYear[(special-1)%12]] {
					want = "win"
				}
				// Completely different regular-ball sets must produce the same
				// result, including when 49 is the special ball.
				for _, descending := range []bool{false, true} {
					numbers := make([]int, 0, 7)
					for i := 1; len(numbers) < 6; i++ {
						n := i
						if descending {
							n = 50 - i
						}
						if n != special {
							numbers = append(numbers, n)
						}
					}
					numbers = append(numbers, special)
					r := games.Round{SessionID: g.ID, Origin: "scheduled", DrawAt: at, Numbers: numbers}
					got, err := Evaluate(m, g, r)
					if err != nil || got != want {
						t.Fatalf("set %v, numbers %v: got %s/%v, want %s", values, numbers, got, err, want)
					}
				}
			}
		}
	}
}

func TestCombinedIndependentEveryExactSetSizeIsEnforced(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	for count := 2; count <= 11; count++ {
		m, _ := findLegacyMarkSix(fmt.Sprintf("lhc5.combined_zodiac.in.%d", count))
		valid := make([]int, count)
		for i := range valid {
			valid[i] = i
		}
		duplicate, low, high := append([]int(nil), valid...), append([]int(nil), valid...), append([]int(nil), valid...)
		duplicate[count-1], low[0], high[count-1] = 0, -1, 12
		for _, values := range [][]int{nil, valid[:count-1], append(append([]int(nil), valid...), count), duplicate, low, high} {
			if _, err := BindSelection(m, values, at); err == nil {
				t.Fatalf("%d-zodiac market accepted invalid exact set %v", count, values)
			}
		}
	}
	for _, count := range []int{0, 1, 12, 13} {
		if _, ok := findLegacyMarkSix(fmt.Sprintf("lhc5.combined_zodiac.in.%d", count)); ok {
			t.Fatal("unsupported group size offered", count)
		}
	}
}

func TestCombinedIndependentRejectsEverySettlementContractMutation(t *testing.T) {
	g, _ := games.Find("lhc-tw")
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	template, _ := findLegacyMarkSix("lhc5.combined_zodiac.in.2")
	m, err := BindSelection(template, []int{5, 6}, at)
	if err != nil {
		t.Fatal(err)
	}
	r := games.Round{SessionID: g.ID, Origin: "scheduled", DrawAt: at, Numbers: []int{1, 13, 25, 37, 2, 14, 49}}
	cases := map[string]func(*Market){
		"id":               func(v *Market) { v.ID = "lhc5.combined_zodiac.in.3" },
		"kind":             func(v *Market) { v.Kind = "number" },
		"pick":             func(v *Market) { v.Pick = "out" },
		"position_regular": func(v *Market) { v.Positions = []int{1} },
		"position_extra":   func(v *Market) { v.Positions = []int{1, 7} },
		"position_nil":     func(v *Market) { v.Positions = nil },
		"push49":           func(v *Market) { v.PushValues = []int{49} },
		"small":            func(v *Market) { v.SmallMax = 24 },
		"big":              func(v *Market) { v.BigMin = 25 },
		"version":          func(v *Market) { v.RuleVersion = 1 },
		"year_previous":    func(v *Market) { v.ZodiacYear-- },
		"year_next":        func(v *Market) { v.ZodiacYear++ },
		"values_unbound":   func(v *Market) { v.SelectionValues = nil },
		"values_unsorted":  func(v *Market) { v.SelectionValues = []int{6, 5} },
		"values_duplicate": func(v *Market) { v.SelectionValues = []int{6, 6} },
		"values_outside":   func(v *Market) { v.SelectionValues = []int{6, 12} },
		"values_extra":     func(v *Market) { v.SelectionValues = []int{4, 5, 6} },
		"schema_nil":       func(v *Market) { v.Selection = nil },
		"schema_type":      func(v *Market) { v.Selection.Type = "number" },
		"schema_mode":      func(v *Market) { v.Selection.Mode = "combination" },
		"schema_count":     func(v *Market) { v.Selection.PickCount = 3 },
		"schema_min":       func(v *Market) { v.Selection.MinValue = -1 },
		"schema_max":       func(v *Market) { v.Selection.MaxValue = 12 },
		"schema_max_picks": func(v *Market) { v.Selection.MaxPicks = 3 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			invalid := m
			schema := *m.Selection
			invalid.Selection = &schema
			mutate(&invalid)
			if got, err := Evaluate(invalid, g, r); err == nil {
				t.Fatal("altered settlement snapshot accepted", got)
			}
		})
	}
}

func TestCombinedIndependentRejectsIllegalDraws(t *testing.T) {
	g, _ := games.Find("lhc-tw")
	at := time.Date(2026, 10, 16, 12, 20, 5, 0, model.Beijing)
	template, _ := findLegacyMarkSix("lhc5.combined_zodiac.in.2")
	m, err := BindSelection(template, []int{5, 6}, at)
	if err != nil {
		t.Fatal(err)
	}
	valid := games.Round{SessionID: g.ID, Origin: "scheduled", DrawAt: at, Numbers: []int{1, 13, 25, 37, 2, 14, 49}}
	cases := map[string]func(*games.Round){
		"other_session":   func(r *games.Round) { r.SessionID = "lhc-hk" },
		"missing_session": func(r *games.Round) { r.SessionID = "" },
		"missing_origin":  func(r *games.Round) { r.Origin = "" },
		"manual_origin":   func(r *games.Round) { r.Origin = "manual" },
		"replay_origin":   func(r *games.Round) { r.Origin = "replay" },
		"missing_time":    func(r *games.Round) { r.DrawAt = time.Time{} },
		"wrong_time":      func(r *games.Round) { r.DrawAt = at.Add(time.Second) },
		"no_numbers":      func(r *games.Round) { r.Numbers = nil },
		"six_numbers":     func(r *games.Round) { r.Numbers = []int{1, 2, 3, 4, 5, 6} },
		"eight_numbers":   func(r *games.Round) { r.Numbers = []int{1, 2, 3, 4, 5, 6, 7, 49} },
		"zero_regular":    func(r *games.Round) { r.Numbers = []int{0, 2, 3, 4, 5, 6, 49} },
		"over49_regular":  func(r *games.Round) { r.Numbers = []int{50, 2, 3, 4, 5, 6, 49} },
		"zero_special":    func(r *games.Round) { r.Numbers = []int{1, 2, 3, 4, 5, 6, 0} },
		"over49_special":  func(r *games.Round) { r.Numbers = []int{1, 2, 3, 4, 5, 6, 50} },
		"repeat_regular":  func(r *games.Round) { r.Numbers = []int{1, 1, 3, 4, 5, 6, 49} },
		"repeat_special":  func(r *games.Round) { r.Numbers = []int{1, 2, 3, 4, 5, 49, 49} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			mutate(&invalid)
			if got, err := Evaluate(m, g, invalid); err == nil {
				t.Fatal("illegal draw settled", got)
			}
		})
	}
}

// Historical v5 contracts are deliberately absent from the new sales catalog.
func findLegacyMarkSix(id string) (Market, bool) {
	for _, m := range LegacyMarkSixCatalog() {
		if m.ID == id {
			return m, true
		}
	}
	return Market{}, false
}
