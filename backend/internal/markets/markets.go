// Package markets defines versioned, position-specific selections and settlement rules.
package markets

import (
	"errors"
	"fmt"
	"starlight/backend/internal/games"
	"strconv"
	"strings"
)

const Version = 1

var pk10SumSets = [...]struct {
	pick, label string
	odds        int64
}{
	{"3,4,18,19", "03 / 04 / 18 / 19", 11800},
	{"5,6,16,17", "05 / 06 / 16 / 17", 5400},
	{"7,8,14,15", "07 / 08 / 14 / 15", 3600},
	{"9,10,12,13", "09 / 10 / 12 / 13", 2700},
}

type Market struct {
	ID                  string           `json:"id"`
	Group               string           `json:"group"`
	Label               string           `json:"label"`
	Category            string           `json:"category"`
	Kind                string           `json:"kind"`
	Positions           []int            `json:"positions"`
	Pick                string           `json:"pick"`
	SmallMax            int              `json:"small_max"`
	BigMin              int              `json:"big_min"`
	PushValues          []int            `json:"push_values,omitempty"`
	Rule                string           `json:"rule"`
	RuleVersion         int              `json:"rule_version"`
	DefaultOdds         int64            `json:"default_odds_milli"`
	UIGroup             string           `json:"ui_group,omitempty"`
	Selection           *SelectionSchema `json:"selection_schema,omitempty"`
	SelectionValues     []int            `json:"selection_values,omitempty"`
	ZodiacYear          int              `json:"zodiac_year,omitempty"`
	PayoutTiers         []PayoutTier     `json:"payout_tiers,omitempty"`
	MaxPayoutMultiplier int64            `json:"max_payout_multiplier,omitempty"`
}

func Catalog(gameID string) []Market {
	if gameID == "lhc" {
		return markSixV6Catalog()
	}
	return legacyCatalog(gameID)
}

// LegacyMarkSixCatalog is retained for historical snapshot/quote migrations.
// It is never returned by the current sales catalog or Find.
func LegacyMarkSixCatalog() []Market { return legacyCatalog("lhc") }

func legacyCatalog(gameID string) []Market {

	if gameID == "k3" {
		return quickThreeCatalog()
	}
	result := []Market{}
	count, low, high := 5, 0, 9
	switch gameID {
	case "ssc", "ffc":
	case "pk10":
		count = 10
		low = 1
		high = 10
	case "lhc-au":
		count, low, high = 8, 1, 45
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
		version := Version
		if kind == "size" && gameID == "pk10" && strings.HasPrefix(id, "sum.") {
			id = "size3." + id
			version = 3
		} else if kind == "size" && gameID == "lhc" {
			id = "size2." + id
			version = 2
		}
		if gameID == "lhc" && kind == "parity" {
			id = "parity2." + id
			version = 2
		}
		if gameID == "lhc-au" {
			id = "au45." + id
			version = 2
		}
		result = append(result, Market{ID: id, Group: group, Label: label, Category: category, Kind: kind, Positions: pos, Pick: pick, SmallMax: small, BigMin: big, PushValues: push, Rule: rule, RuleVersion: version, DefaultOdds: odds})
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
		if gameID == "lhc" || gameID == "lhc-au" {
			group = fmt.Sprintf("正码%d", i)
			if i == 7 {
				group = "特码"
			}
			if gameID == "lhc-au" && i >= 7 {
				group = fmt.Sprintf("附加号%d", i-6)
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
		if gameID == "lhc" || gameID == "lhc-au" {
			small = 24
			big = 26
			sizePush = []int{25}
			parityPush = nil
			rule = "01–24小、25为和、26–49大；开25时仅大小退本金；单双独立按奇偶判奖，25、49均为单；指定数字正常判奖。"
		}
		if gameID == "lhc-au" {
			small, big = 22, 24
			sizePush, parityPush = []int{23}, nil
			rule = "01–22小、24–45大，23为和；单双按奇偶；开23时仅大小退本金，单中奖、双不中奖；指定数字23正常判奖。附加号1、附加号2分别按第7、第8位判奖。"
		}
		faces(prefix, group, []int{i}, small, big, sizePush, parityPush, rule)
		odds := int64(high-low+1) * 980
		for n := low; n <= high; n++ {
			label := strconv.Itoa(n)
			if gameID == "pk10" || gameID == "lhc" || gameID == "lhc-au" {
				label = fmt.Sprintf("%02d", n)
			}
			add(prefix+".n"+strconv.Itoa(n), group, "regular", "number", strconv.Itoa(n), label, "仅比较所选位置的开奖号码，其他位置不影响本注。", []int{i}, 0, 0, nil, odds)
		}
	}
	if gameID == "ssc" || gameID == "ffc" {
		sumRule := "五球之和0–22小，23–45大；按和值奇偶判单双，无和数。"
		if gameID == "ssc" {
			sumRule = "五个不重复号码之和10–22小，23–35大；按和值奇偶判单双，无和数。"
		}
		faces("sum", "总和", []int{1, 2, 3, 4, 5}, 22, 23, nil, nil, sumRule)
		group, rule := "龙虎", "第一球大于第五球为龙，小于为虎；本场号码不重复，不会出现相等。"
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
		faces("sum", "冠亚和", []int{1, 2}, 11, 12, nil, nil, "冠军与亚军车号之和3–11为小、12–19为大；单双按和值奇偶判奖，11为小、单；无和局退本。")
		for _, x := range []struct{ pick, label string }{{"dragon", "龙"}, {"tiger", "虎"}} {
			add("dt1."+x.pick, "龙虎", "two_sides", "compare", x.pick, x.label, "比较第1名和第10名，前者大为龙，前者小为虎。", []int{1, 10}, 0, 0, nil, 1960)
		}
		sumOdds := [...]int64{39600, 39600, 19600, 19600, 12900, 12900, 9450, 9450, 7740, 9450, 9450, 12900, 12900, 19800, 19800, 39600, 39600}
		for i, odds := range sumOdds {
			n := i + 3
			add("sum.n"+strconv.Itoa(n), "冠亚和", "regular", "number", strconv.Itoa(n), fmt.Sprintf("%02d", n), "冠亚和为第1名与第2名号码之和（03–19），并非额外赛车号码；与所选和值相同即中奖，11正常判奖、不退本。", []int{1, 2}, 0, 0, nil, odds)
		}
		for _, set := range pk10SumSets {
			add("sum.set_"+strings.ReplaceAll(set.pick, ",", "_"), "冠亚和", "regular", "number_set", set.pick, set.label, "冠亚和为第1名与第2名号码之和，并非额外赛车号码；和值属于本组任一数字即中奖，每注仅派彩一次；和值11不中奖、不退本。", []int{1, 2}, 0, 0, nil, set.odds)
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
	if gameID == "lhc" {
		for i := range result {
			// These are presentation groups only. Keep all 371 legacy IDs,
			// thresholds, push rules and default prices exactly as before.
			result[i].UIGroup = "position_faces"
			if result[i].Kind == "number" {
				result[i].UIGroup = "position_numbers"
			}
			if len(result[i].Positions) == 1 && result[i].Positions[0] == 7 {
				result[i].UIGroup = "special"
			}
		}
		result = append(result, markSixCombinedCatalog()...)
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
func evaluateLegacy(m Market, g games.Session, r games.Round) (string, error) {
	// This additive release accepts only the ten confirmed combined-zodiac
	// contracts. Other version-5 rules are not enabled by this entry point.
	if strings.HasPrefix(m.ID, "lhc5.") || m.RuleVersion == 5 || m.Kind == "lhc_combined_zodiac" {
		if g.MarketCatalogIDAt(r.DrawAt) != "lhc" || m.RuleVersion != 5 {
			return "", errors.New("不支持的六合彩合肖规则")
		}
		if r.SessionID != g.ID || r.Origin != "scheduled" || !g.MatchesStored(r.DrawAt) {
			return "", errors.New("开奖来源或时刻不匹配")
		}
		return evaluateMarkSixCombined(m, r.Numbers, r.DrawAt)
	}
	if m.RuleVersion != Version && !((g.GameID == "lhc" || g.GameID == "pk10" || g.GameID == "k3") && m.RuleVersion == 2) && !((g.GameID == "pk10" || g.GameID == "k3") && m.RuleVersion == 3) {
		return "", errors.New("不支持的规则版本")
	}

	// Version 3 changes only new PK10 sum-size orders. Old snapshots retain
	// their original boundaries and push values when settled after the change.
	if strings.HasPrefix(m.ID, "size3.") || m.RuleVersion == 3 && g.GameID != "k3" {
		valid := g.GameID == "pk10" && m.RuleVersion == 3 && m.Kind == "size" && (m.Pick == "big" || m.Pick == "small") && m.ID == "size3.sum."+m.Pick && len(m.Positions) == 2 && m.Positions[0] == 1 && m.Positions[1] == 2 && m.SmallMax == 11 && m.BigMin == 12 && len(m.PushValues) == 0
		if !valid {
			return "", errors.New("不支持的冠亚和大小规则")
		}
	}
	if m.Kind == "triple" || strings.HasPrefix(m.Kind, "dice_") || strings.HasPrefix(m.ID, "k3v2.") || strings.HasPrefix(m.ID, "k3v3.") || g.GameID == "k3" && (m.RuleVersion == 2 || m.RuleVersion == 3) {
		if g.GameID != "k3" || !validQuickThreeMarket(m) {
			return "", errors.New("不支持的快三规则")
		}
	}
	// The new set selection is a PK10-only contract. Validate it before push
	// handling so altered positions or push values cannot change its meaning.
	if m.Kind == "number_set" || strings.HasPrefix(m.ID, "sum.set_") {
		valid := g.GameID == "pk10" && m.Kind == "number_set" && m.RuleVersion == Version && m.ID == "sum.set_"+strings.ReplaceAll(m.Pick, ",", "_") && len(m.Positions) == 2 && m.Positions[0] == 1 && m.Positions[1] == 2 && len(m.PushValues) == 0
		known := false
		for _, set := range pk10SumSets {
			known = known || m.Pick == set.pick
		}
		if !valid || !known {
			return "", errors.New("不支持的冠亚和组合规则")
		}
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
	if strings.HasPrefix(m.ID, "au45.") {
		if g.ID != "lhc-au" || m.RuleVersion != 2 {
			return "", errors.New("不支持的悉尼旧45选8规则")
		}
		count, high = 8, 45
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

	// The exclusion follows the accepted snapshot: v1 size/parity and v2
	// single/double orders retain their original payouts after catalog changes.
	tripleExcluded := m.RuleVersion == 2 && (m.Kind == "size" || m.Kind == "parity") || m.RuleVersion == 3 && (m.Kind == "dice_face" || m.Kind == "dice_double")
	if g.GameID == "k3" && tripleExcluded && r.Numbers[0] == r.Numbers[1] && r.Numbers[1] == r.Numbers[2] {
		return "lose", nil
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
	case "number_set":
		for _, pick := range strings.Split(m.Pick, ",") {
			n, _ := strconv.Atoi(pick) // The canonical PK10 set was validated above.
			if value == n {
				won = true
				break
			}
		}
	case "triple":
		won = r.Numbers[0] == r.Numbers[1] && r.Numbers[1] == r.Numbers[2] && (m.Pick == "any" || m.Pick == strconv.Itoa(r.Numbers[0]))
	case "dice_face":
		n, _ := strconv.Atoi(m.Pick) // The versioned catalog was validated above.
		won = seen[n]
	case "dice_pair":
		picks := strings.Split(m.Pick, ",")
		a, _ := strconv.Atoi(picks[0])
		b, _ := strconv.Atoi(picks[1])
		won = seen[a] && seen[b]
	case "dice_double":
		n, _ := strconv.Atoi(m.Pick)
		matches := 0
		for _, value := range r.Numbers {
			if value == n {
				matches++
			}
		}
		won = matches >= 2
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
