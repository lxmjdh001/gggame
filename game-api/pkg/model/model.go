package model

import (
	"fmt"
	"strconv"
	"time"
	_ "time/tzdata"
)

var Beijing = mustLocation()

func mustLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}

const IssueLayout = "200601021504"
const SchemaVersion = 4

type Rule struct {
	ID          string `json:"id"`
	Min         int    `json:"min"`
	Max         int    `json:"max"`
	Count       int    `json:"count"`
	AllowRepeat bool   `json:"allow_repeat"`
}

func Rules() []Rule {
	return []Rule{
		{ID: "g1", Min: 0, Max: 9, Count: 10, AllowRepeat: true},
		{ID: "g2", Min: 0, Max: 9, Count: 10, AllowRepeat: false},
		{ID: "g3", Min: 1, Max: 9, Count: 10, AllowRepeat: true},
		{ID: "g4", Min: 1, Max: 9, Count: 9, AllowRepeat: false},
		{ID: "g5", Min: 1, Max: 10, Count: 10, AllowRepeat: true},
		{ID: "g6", Min: 1, Max: 10, Count: 10, AllowRepeat: false},
		{ID: "g7", Min: 1, Max: 49, Count: 49, AllowRepeat: false},
		{ID: "g8", Min: 1, Max: 6, Count: 10, AllowRepeat: true},
	}
}

type Draw struct {
	SchemaVersion int                 `json:"schema_version"`
	Issue         string              `json:"issue"`
	Timezone      string              `json:"timezone"`
	ScheduledAt   time.Time           `json:"scheduled_at"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Groups        map[string][]int    `json:"groups"`
	DisplayGroups map[string][]string `json:"display_groups,omitempty"`
}
type Selection struct {
	Issue          string    `json:"issue"`
	Group          string    `json:"group"`
	Positions      []int     `json:"positions"`
	Numbers        []int     `json:"numbers"`
	DisplayNumbers []string  `json:"display_numbers"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	GeneratedAt    time.Time `json:"generated_at"`
}
type Page struct {
	Items      []Draw `json:"items"`
	NextBefore string `json:"next_before,omitempty"`
}
type Metadata struct {
	GenerationMode  string `json:"generation_mode"`
	BatchesPerDay   int    `json:"batches_per_day"`
	Timezone        string `json:"timezone"`
	IntervalSeconds int    `json:"interval_seconds"`
	TriggerSecond   int    `json:"trigger_second"`
	PositionBase    int    `json:"position_base"`
	Rules           []Rule `json:"groups"`
}
type Envelope[T any] struct {
	Data T `json:"data"`
}
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

func ParseIssue(issue string) (time.Time, error) {
	if len(issue) != 12 {
		return time.Time{}, fmt.Errorf("issue must be YYYYMMDDHHmm in Asia/Shanghai")
	}
	for _, c := range issue {
		if c < '0' || c > '9' {
			return time.Time{}, fmt.Errorf("invalid issue")
		}
	}
	t, err := time.ParseInLocation(IssueLayout, issue, Beijing)
	if err != nil || t.Format(IssueLayout) != issue {
		return time.Time{}, fmt.Errorf("invalid issue")
	}
	return t.Add(time.Second), nil
}
func Validate(d Draw) error {
	scheduled, err := ParseIssue(d.Issue)
	if err != nil || !scheduled.Equal(d.ScheduledAt) || (d.SchemaVersion < 1 || d.SchemaVersion > SchemaVersion) || d.Timezone != "Asia/Shanghai" || d.GeneratedAt.IsZero() || (d.SchemaVersion < 4 && d.GeneratedAt.Before(d.ScheduledAt)) {
		return fmt.Errorf("invalid draw metadata")
	}
	rules := Rules()
	if d.SchemaVersion == 1 {
		rules = rules[:6]
	} else if d.SchemaVersion == 2 {
		rules = rules[:7]
	}
	if len(d.Groups) != len(rules) {
		return fmt.Errorf("unexpected groups for schema version")
	}
	for _, rule := range rules {
		values, ok := d.Groups[rule.ID]
		if !ok || len(values) != rule.Count {
			return fmt.Errorf("invalid count for %s", rule.ID)
		}
		seen := map[int]bool{}
		for _, value := range values {
			if value < rule.Min || value > rule.Max || (!rule.AllowRepeat && seen[value]) {
				return fmt.Errorf("invalid numbers for %s", rule.ID)
			}
			seen[value] = true
		}
	}
	return nil
}
func Select(d Draw, group string, positions []int) (Selection, error) {
	numbers, ok := d.Groups[group]
	if !ok {
		return Selection{}, fmt.Errorf("unknown group: %s", group)
	}
	if len(positions) == 0 {
		positions = make([]int, len(numbers))
		for i := range positions {
			positions[i] = i + 1
		}
	}
	result := Selection{Issue: d.Issue, Group: group, Positions: append([]int(nil), positions...), Numbers: make([]int, 0, len(positions)), ScheduledAt: d.ScheduledAt, GeneratedAt: d.GeneratedAt}
	seen := map[int]bool{}
	for _, position := range positions {
		if position < 1 || position > len(numbers) || seen[position] {
			return Selection{}, fmt.Errorf("positions must be distinct integers from 1 to %s", strconv.Itoa(len(numbers)))
		}
		seen[position] = true
		result.Numbers = append(result.Numbers, numbers[position-1])
	}
	result.DisplayNumbers = Format(group, result.Numbers)
	return result, nil
}

func Format(group string, numbers []int) []string {
	out := make([]string, len(numbers))
	for i, n := range numbers {
		if group == "g6" || group == "g7" {
			out[i] = fmt.Sprintf("%02d", n)
		} else {
			out[i] = strconv.Itoa(n)
		}
	}
	return out
}
func Display(d *Draw) {
	d.DisplayGroups = make(map[string][]string)
	for group, numbers := range d.Groups {
		d.DisplayGroups[group] = Format(group, numbers)
	}
}
