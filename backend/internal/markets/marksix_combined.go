package markets

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/6tail/lunar-go/calendar"
	"starlight/gameapi/pkg/model"
)

// SelectionSchema describes one exact selected set. A combined-zodiac
// selection is one order, regardless of the number of selected zodiac signs.
type SelectionSchema struct {
	Type      string `json:"type"`
	Mode      string `json:"mode"`
	PickCount int    `json:"pick_count"`
	MinValue  int    `json:"min_value"`
	MaxValue  int    `json:"max_value"`
	MaxPicks  int    `json:"max_picks"`
}
type MarkSixValueGroup struct {
	Value   int    `json:"value"`
	Label   string `json:"label"`
	Numbers []int  `json:"numbers"`
}
type MarkSixNumber struct {
	Number int    `json:"number"`
	Color  string `json:"color"`
	Zodiac int    `json:"zodiac"`
	Tail   int    `json:"tail"`
}
type MarkSixContext struct {
	Issue        string              `json:"issue"`
	DrawAt       time.Time           `json:"draw_at"`
	LunarYear    int                 `json:"lunar_year"`
	Zodiacs      []MarkSixValueGroup `json:"zodiacs"`
	Tails        []MarkSixValueGroup `json:"tails"`
	Numbers      []MarkSixNumber     `json:"numbers"`
	MaxBatchBets int                 `json:"max_batch_bets"`
}

var markSixAnimals = [...]string{"鼠", "牛", "虎", "兔", "龙", "蛇", "马", "羊", "猴", "鸡", "狗", "猪"}
var markSixLunarYears sync.Map

func MarkSixLunarYear(at time.Time) int {
	at = at.In(model.Beijing)
	day := at.Format("2006-01-02")
	if year, ok := markSixLunarYears.Load(day); ok {
		return year.(int)
	}
	year := calendar.NewSolarFromDate(at).GetLunar().GetYear()
	markSixLunarYears.Store(day, year)
	return year
}
func MarkSixColor(number int) string {
	switch number {
	case 1, 2, 7, 8, 12, 13, 18, 19, 23, 24, 29, 30, 34, 35, 40, 45, 46:
		return "red"
	case 3, 4, 9, 10, 14, 15, 20, 25, 26, 31, 36, 37, 41, 42, 47, 48:
		return "blue"
	default:
		return "green"
	}
}
func MarkSixZodiac(number, year int) int { return ((year-4-(number-1))%12 + 12) % 12 }
func MarkSixContextAt(at time.Time) *MarkSixContext {
	at = at.In(model.Beijing)
	c := &MarkSixContext{Issue: at.Format(model.IssueLayout), DrawAt: at, LunarYear: MarkSixLunarYear(at), MaxBatchBets: 500}
	for value, label := range markSixAnimals {
		c.Zodiacs = append(c.Zodiacs, MarkSixValueGroup{Value: value, Label: label, Numbers: []int{}})
	}
	for tail := 0; tail < 10; tail++ {
		c.Tails = append(c.Tails, MarkSixValueGroup{Value: tail, Label: fmt.Sprintf("%d尾", tail), Numbers: []int{}})
	}
	for number := 1; number <= 49; number++ {
		zodiac, tail := MarkSixZodiac(number, c.LunarYear), number%10
		c.Numbers = append(c.Numbers, MarkSixNumber{Number: number, Color: MarkSixColor(number), Zodiac: zodiac, Tail: tail})
		c.Zodiacs[zodiac].Numbers = append(c.Zodiacs[zodiac].Numbers, number)
		c.Tails[tail].Numbers = append(c.Tails[tail].Numbers, number)
	}
	return c
}

func markSixCombinedCatalog() []Market {
	all := make([]Market, 0, 10)
	for count := 2; count <= 11; count++ {
		all = append(all, Market{
			ID:    fmt.Sprintf("lhc5.combined_zodiac.in.%d", count),
			Group: "合肖", Label: fmt.Sprintf("%d肖中", count), UIGroup: "combined_zodiac",
			Category: "regular", Kind: "lhc_combined_zodiac", Pick: "in", Positions: []int{7},
			Rule:        "每期开出6个平码（正码）和1个特码。选择2–11个不同生肖合为一注，仅第7个特码的生肖在所选组合内即中奖，前6个平码不参与判定；每注只派一次，49正常判奖。",
			RuleVersion: 5, DefaultOdds: 1000,
			Selection: &SelectionSchema{Type: "zodiac", Mode: "set", PickCount: count, MinValue: 0, MaxValue: 11, MaxPicks: count},
		})
	}
	return all
}

var markSixCombinedContracts = func() map[string]Market {
	result := make(map[string]Market)
	for _, m := range markSixCombinedCatalog() {
		result[m.ID] = m
	}
	return result
}()

func sameMarkSixContract(m, expected Market) bool {
	return m.RuleVersion == 5 && m.ID == expected.ID && m.Kind == expected.Kind && m.Pick == expected.Pick && m.SmallMax == expected.SmallMax && m.BigMin == expected.BigMin && reflect.DeepEqual(m.Positions, expected.Positions) && reflect.DeepEqual(m.PushValues, expected.PushValues) && reflect.DeepEqual(m.Selection, expected.Selection)
}
func zodiacMarket(m Market) bool {
	return m.Kind == "lhc_combined_zodiac"
}
func selectionValues(m Market, values []int) ([]int, error) {
	if m.Selection == nil {
		if len(values) != 0 {
			return nil, errors.New("此玩法不接受组合选值")
		}
		return nil, nil
	}
	s := m.Selection
	if len(values) != s.PickCount {
		return nil, fmt.Errorf("%s每注须选择%d个不同选项", m.Label, s.PickCount)
	}
	result := append([]int(nil), values...)
	sort.Ints(result)
	for i, value := range result {
		if value < s.MinValue || value > s.MaxValue || i > 0 && value == result[i-1] {
			return nil, errors.New("组合选值重复或超出该玩法范围")
		}
	}
	return result, nil
}

// BindSelection is called only after finding a server-side template. Clients
// send exact values, never their own settlement thresholds, positions or year.
func BindSelection(m Market, values []int, drawAt time.Time) (Market, error) {
	if m.RuleVersion == 6 || strings.HasPrefix(m.ID, "lhc6.") {
		return bindMarkSixV6(m, values, drawAt)
	}
	if m.RuleVersion != 5 {
		if len(values) != 0 {
			return Market{}, errors.New("此玩法不接受组合选值")
		}
		return m, nil
	}
	expected, ok := markSixCombinedContracts[m.ID]
	if !ok || !sameMarkSixContract(m, expected) {
		return Market{}, errors.New("不支持的六合彩合肖规则")
	}
	picks, err := selectionValues(m, values)
	if err != nil {
		return Market{}, err
	}
	m.SelectionValues = picks
	if zodiacMarket(m) {
		m.ZodiacYear = MarkSixLunarYear(drawAt)
	}
	return m, nil
}
func SelectionLabel(m Market) string {
	if len(m.SelectionValues) == 0 || m.Selection == nil {
		return ""
	}
	parts := make([]string, 0, len(m.SelectionValues))
	for _, value := range m.SelectionValues {
		switch m.Selection.Type {
		case "zodiac":
			if value < 0 || value >= len(markSixAnimals) {
				return ""
			}
			parts = append(parts, markSixAnimals[value])
		case "tail":
			parts = append(parts, fmt.Sprintf("%d尾", value))
		case "number":
			parts = append(parts, fmt.Sprintf("%02d", value))
		default:
			return ""
		}
	}
	return strings.Join(parts, " / ")
}
func evaluateMarkSixCombined(m Market, numbers []int, drawAt time.Time) (string, error) {
	expected, ok := markSixCombinedContracts[m.ID]
	if !ok || !sameMarkSixContract(m, expected) {
		return "", errors.New("不支持的六合彩合肖规则")
	}
	values, err := selectionValues(m, m.SelectionValues)
	if err != nil || !reflect.DeepEqual(values, m.SelectionValues) {
		return "", errors.New("六合彩投注快照选值无效")
	}
	if m.ZodiacYear != MarkSixLunarYear(drawAt) {
		return "", errors.New("生肖年份快照与开奖期不匹配")
	}
	if len(numbers) != 7 {
		return "", errors.New("六合彩应开出7个号码")
	}
	seen := map[int]bool{}
	for _, number := range numbers {
		if number < 1 || number > 49 || seen[number] {
			return "", errors.New("六合彩号码应为01–49且不可重复")
		}
		seen[number] = true
	}
	// Only the final, special ball decides this single set wager.
	zodiac := MarkSixZodiac(numbers[6], m.ZodiacYear)
	for _, value := range values {
		if value == zodiac {
			return "win", nil
		}
	}
	return "lose", nil
}
