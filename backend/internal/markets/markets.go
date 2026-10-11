// Package markets defines versioned, position-specific selections and settlement rules.
package markets

import (
	"errors"
	"fmt"
	"starlight/backend/internal/games"
	"strconv"
)

const Version = 1

type Market struct {
	ID          string `json:"id"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	Kind        string `json:"kind"`
	Positions   []int  `json:"positions"`
	Pick        string `json:"pick"`
	SmallMax    int    `json:"small_max"`
	BigMin      int    `json:"big_min"`
	PushValues  []int  `json:"push_values,omitempty"`
	Rule        string `json:"rule"`
	RuleVersion int    `json:"rule_version"`
	DefaultOdds int64  `json:"default_odds_milli"`
}

func Catalog(gameID string) []Market {
	result := []Market{}
	count, low, high := 5, 0, 9
	switch gameID {
	case "ssc", "ffc":
	case "k3":
		count = 3
		low = 1
		high = 6
	case "pk10":
		count = 10
		low = 1
		high = 10
	case "lhc":
		count = 7
		low = 1
		high = 49
	case "pc28":
		count = 3
	default:
		return result
	}
	add := func(id, group, category, kind, pick, label, rule string, pos []int, small, big int, push []int, odds int64) {
		result = append(result, Market{id, group, label, category, kind, pos, pick, small, big, push, rule, Version, odds})
	}
	faces := func(prefix, group string, pos []int, small, big int, sizePush, parityPush []int, rule string) {
		for _, x := range []struct{ pick, label string }{{"big", "大"}, {"small", "小"}, {"odd", "单"}, {"even", "双"}} {
			kind, push := "size", sizePush
			if x.pick == "odd" || x.pick == "even" {
				kind = "parity"
				push = parityPush
			}
			add(prefix+"."+x.pick, group, "two_sides", kind, x.pick, x.label, rule, pos, small, big, push, 1960)
		}
	}
	for i := 1; i <= count; i++ {
		group := fmt.Sprintf("第%d球", i)
		if gameID == "pk10" {
			group = fmt.Sprintf("第%d名", i)
		}
		if gameID == "lhc" {
			group = fmt.Sprintf("正码%d", i)
			if i == 7 {
				group = "特码"
			}
		}
		prefix := fmt.Sprintf("p%d", i)
		small, big := 4, 5
		var sizePush, parityPush []int
		rule := "0–4为小，5–9为大；0为双，按号码奇偶判单双。"
		if gameID == "pk10" {
			small = 5
			big = 6
			rule = "01–05为小，06–10为大；按号码奇偶判单双。"
		}
		if gameID == "k3" {
			small = 3
			big = 4
			rule = "骰子单球1–3为小，4–6为大；按点数奇偶判单双。"
		}
		if gameID == "lhc" {
			small = 24
			big = 25
			sizePush = []int{49}
			parityPush = []int{49}
			rule = "01–24小、25–48大；单双按奇偶；开49时该位置大小单双退本金，指定数字49正常判奖。"
		}
		faces(prefix, group, []int{i}, small, big, sizePush, parityPush, rule)
		odds := int64(high-low+1) * 980
		for n := low; n <= high; n++ {
			label := strconv.Itoa(n)
			if gameID == "pk10" || gameID == "lhc" {
				label = fmt.Sprintf("%02d", n)
			}
			add(prefix+".n"+strconv.Itoa(n), group, "regular", "number", strconv.Itoa(n), label, "仅比较所选位置的开奖号码，其他位置不影响本注。", []int{i}, 0, 0, nil, odds)
		}
	}
	if gameID == "ssc" || gameID == "ffc" {
		faces("sum", "总和", []int{1, 2, 3, 4, 5}, 22, 23, nil, nil, "五球之和0–22小，23–45大；按和值奇偶判单双。")
		group, rule := "龙虎", "第一球大于第五球为龙，小于为虎；相等退回龙虎本金。"
		if gameID == "ffc" {
			group = "龙虎和"
			rule = "第一球大于第五球为龙，小于为虎，相等为和；投注龙或虎遇和退回本金，投注和仅在两球相等时中奖。"
		}
		for _, x := range []struct{ pick, label string }{{"dragon", "龙"}, {"tiger", "虎"}} {
			add("dt."+x.pick, group, "two_sides", "compare", x.pick, x.label, rule, []int{1, 5}, 0, 0, nil, 1960)
		}
		if gameID == "ffc" {
			add("dt.tie", group, "two_sides", "compare", "tie", "和", rule, []int{1, 5}, 0, 0, nil, 8000)
		}
	}
	if gameID == "pk10" {
		faces("sum", "冠亚和", []int{1, 2}, 11, 12, nil, nil, "前两名号码之和3–11小、12–19大；按和值奇偶判单双。")
		for i := 1; i <= 5; i++ {
			for _, x := range []struct{ pick, label string }{{"dragon", "龙"}, {"tiger", "虎"}} {
				add(fmt.Sprintf("dt%d.%s", i, x.pick), fmt.Sprintf("第%d名 vs 第%d名", i, 11-i), "two_sides", "compare", x.pick, x.label, fmt.Sprintf("比较第%d名和第%d名，前者大为龙，前者小为虎。", i, 11-i), []int{i, 11 - i}, 0, 0, nil, 1960)
			}
		}
	}
	if gameID == "k3" {
		faces("sum", "总和", []int{1, 2, 3}, 10, 11, nil, nil, "三颗骰子和值3–10小、11–18大；单双按和值奇偶，本项目不设豹子通吃。")
		ways := [19]int64{}
		for a := 1; a <= 6; a++ {
			for b := 1; b <= 6; b++ {
				for c := 1; c <= 6; c++ {
					ways[a+b+c]++
				}
			}
		}
		for n := 3; n <= 18; n++ {
			add("sum.n"+strconv.Itoa(n), "和值数字", "regular", "number", strconv.Itoa(n), strconv.Itoa(n), "三个骰子的点数之和与所选和值相同即中奖。", []int{1, 2, 3}, 0, 0, nil, 211680/ways[n])
		}
	}
	if gameID == "pc28" {
		faces("sum", "总和", []int{1, 2, 3}, 13, 14, nil, nil, "三球和值0–13小、14–27大；按和值奇偶判单双，13/14正常赔付，不降赔、不退本。")
		ways := [28]int64{}
		for a := 0; a < 10; a++ {
			for b := 0; b < 10; b++ {
				for c := 0; c < 10; c++ {
					ways[a+b+c]++
				}
			}
		}
		for n := 0; n <= 27; n++ {
			add("sum.n"+strconv.Itoa(n), "和值数字", "regular", "number", strconv.Itoa(n), strconv.Itoa(n), "三球相加的和值与所选数字一致即中奖；不同和值的概率与赔率不同。", []int{1, 2, 3}, 0, 0, nil, 980000/ways[n])
		}
	}
	return result
}
func Find(gameID, id string) (Market, bool) {
	for _, m := range Catalog(gameID) {
		if m.ID == id {
			return m, true
		}
	}
	return Market{}, false
}
func Evaluate(m Market, g games.Session, r games.Round) (string, error) {
	if m.RuleVersion != Version {
		return "", errors.New("不支持的规则版本")
	}
	if r.SessionID != g.ID || r.Origin != "scheduled" || !g.MatchesStored(r.DrawAt) {
		return "", errors.New("开奖来源或时刻不匹配")
	}
	// Validate the complete result before selecting positions, not merely the requested position.
	count, low, high, unique := 5, 0, 9, false
	switch g.GameID {
	case "ssc":
		unique = true
	case "ffc":
	case "k3":
		count = 3
		low = 1
		high = 6
	case "pk10":
		count = 10
		low = 1
		high = 10
		unique = true
	case "lhc":
		count = 7
		low = 1
		high = 49
		unique = true
	case "pc28":
		count = 3
	default:
		return "", errors.New("未知游戏")
	}
	if len(r.Numbers) != count {
		return "", errors.New("开奖号码长度不正确")
	}
	seen := map[int]bool{}
	for _, n := range r.Numbers {
		if n < low || n > high || (unique && seen[n]) {
			return "", errors.New("开奖号码不符合规则")
		}
		seen[n] = true
	}
	value := 0
	for _, p := range m.Positions {
		if p < 1 || p > len(r.Numbers) {
			return "", errors.New("位置不正确")
		}
		value += r.Numbers[p-1]
	}
	for _, p := range m.PushValues {
		if value == p {
			return "push", nil
		}
	}
	won := false
	switch m.Kind {
	case "size":
		won = m.Pick == "big" && value >= m.BigMin || m.Pick == "small" && value <= m.SmallMax
	case "parity":
		won = m.Pick == "odd" && value%2 == 1 || m.Pick == "even" && value%2 == 0
	case "number":
		n, e := strconv.Atoi(m.Pick)
		if e != nil {
			return "", e
		}
		won = value == n
	case "compare":
		if len(m.Positions) != 2 {
			return "", errors.New("龙虎位置不正确")
		}
		a, b := r.Numbers[m.Positions[0]-1], r.Numbers[m.Positions[1]-1]
		if m.Pick == "tie" {
			won = a == b
		} else {
			if a == b {
				return "push", nil
			}
			won = m.Pick == "dragon" && a > b || m.Pick == "tiger" && a < b
		}
	default:
		return "", errors.New("不支持的玩法")
	}
	if won {
		return "win", nil
	}
	return "lose", nil
}
