package markets

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

// PayoutTier describes one mutually exclusive winning result of one wager.
// The account layer locks each tier's actual quote when accepting the order.
type PayoutTier struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	DefaultOdds int64  `json:"default_odds_milli"`
}
type Evaluation struct {
	Outcome    string
	Tier       string
	Multiplier int64
}

// Evaluate retains the historical outcome API. Money settlement must use
// EvaluateDetailed, which also reports the winning tier or zodiac hit count.
func Evaluate(m Market, g games.Session, r games.Round) (string, error) {
	result, err := EvaluateDetailed(m, g, r)
	return result.Outcome, err
}
func EvaluateDetailed(m Market, g games.Session, r games.Round) (Evaluation, error) {
	if m.RuleVersion == 6 || strings.HasPrefix(m.ID, "lhc6.") {
		if g.GameID != "lhc" || g.MarketCatalogIDAt(r.DrawAt) != "lhc" {
			return Evaluation{}, errors.New("不支持的六合彩规则或场次")
		}
		if r.SessionID != g.ID || r.SourceGroup != g.Group || r.Origin != "scheduled" || !g.MatchesStored(r.DrawAt) || r.Issue != r.DrawAt.In(model.Beijing).Format(model.IssueLayout) || r.SourceIssue != r.Issue {
			return Evaluation{}, errors.New("开奖来源、期号或时刻不匹配")
		}
		return evaluateMarkSixV6(m, r.Numbers, r.DrawAt)
	}
	outcome, err := evaluateLegacy(m, g, r)
	if err != nil {
		return Evaluation{}, err
	}
	result := Evaluation{Outcome: outcome}
	if outcome == "win" {
		result.Multiplier = 1
	}
	return result, nil
}

func markSixV6Catalog() []Market {
	all := make([]Market, 0, 720)
	add := func(id, group, label, ui, kind, pick, rule string, positions []int, push bool) *Market {
		m := Market{ID: "lhc6." + id, Group: group, Label: label, Category: "regular", Kind: kind, Pick: pick, Positions: append([]int(nil), positions...), Rule: rule, RuleVersion: 6, DefaultOdds: 1000, UIGroup: ui}
		if push {
			m.PushValues = []int{49}
		}
		all = append(all, m)
		return &all[len(all)-1]
	}
	regular, seven := []int{1, 2, 3, 4, 5, 6}, []int{1, 2, 3, 4, 5, 6, 7}
	faces := []struct{ pick, label string }{{"big", "大"}, {"small", "小"}, {"odd", "单"}, {"even", "双"}}
	colors := []struct{ pick, label string }{{"red", "红"}, {"blue", "蓝"}, {"green", "绿"}}
	for p := 1; p <= 7; p++ {
		prefix, group, ui := fmt.Sprintf("p%d", p), fmt.Sprintf("正码%d", p), "position_faces"
		exactGroup, exactUI := group, "position_numbers"
		if p == 7 {
			prefix, group, ui = "special", "特码", "special"
			exactGroup, exactUI = "特码", "special"
		}
		positions := []int{p}
		for _, x := range faces {
			kind := "lhc_size"
			if x.pick == "odd" || x.pick == "even" {
				kind = "lhc_parity"
			}
			m := add(prefix+"."+x.pick, group+"两面", x.label, ui, kind, x.pick, "按所选位置号码判定：01–24小、25–48大，单双按奇偶；49时大、小、单、双均退回本金。", positions, true)
			m.Category = "two_sides"
			if kind == "lhc_size" {
				m.SmallMax, m.BigMin = 24, 25
			}
		}
		for _, x := range faces {
			kind := "lhc_digit_size"
			if x.pick == "odd" || x.pick == "even" {
				kind = "lhc_digit_parity"
			}
			m := add(prefix+".digit."+x.pick, group+"合数", "合"+x.label, ui, kind, x.pick, "所选位置号码十位与个位相加：1–6合小、7–12合大，单双按数字和奇偶；49时合大、合小、合单、合双均退本。", positions, true)
			if kind == "lhc_digit_size" {
				m.SmallMax, m.BigMin = 6, 7
			}
		}
		for _, x := range faces[:2] {
			m := add(prefix+".tail."+x.pick, group+"尾大小", "尾"+x.label, ui, "lhc_tail_size", x.pick, "所选位置号码个位0–4尾小、5–9尾大；49时尾大、尾小均退本。", positions, true)
			m.SmallMax, m.BigMin = 4, 5
		}
		for _, color := range colors {
			add(prefix+".color."+color.pick, group+"色波", color.label+"波", ui, "lhc_color", color.pick, "所选位置号码与投注色波相同即中奖；49为绿波，正常判奖，不退本。", positions, false)
		}
		for n := 1; n <= 49; n++ {
			all = append(all, Market{ID: fmt.Sprintf("p%d.n%d", p, n), Group: exactGroup, Label: fmt.Sprintf("%02d", n), Category: "regular", Kind: "number", Positions: append([]int(nil), positions...), Pick: strconv.Itoa(n), Rule: "仅比较所选位置的开奖号码，其他位置不影响本注。", RuleVersion: Version, DefaultOdds: 48020, UIGroup: exactUI})
		}
	}
	// 正特码 has its own two-sided/color contracts. Never reuse the quote IDs
	// of 正特1–6: administrators price the two offerings independently, and
	// accepted historical snapshots must retain their original identity.
	var positionSpecial []Market
	for _, m := range all {
		if m.UIGroup != "position_faces" {
			continue
		}
		m.ID = strings.Replace(m.ID, "lhc6.", "lhc6.position_special.", 1)
		m.Group = strings.Replace(m.Group, "正码", "正特码", 1)
		m.UIGroup = "position_numbers"
		positionSpecial = append(positionSpecial, m)
	}
	all = append(all, positionSpecial...)
	for _, x := range faces {
		kind := "lhc_size"
		if x.pick == "odd" || x.pick == "even" {
			kind = "lhc_parity"
		}
		m := add("sum."+x.pick, "总和", x.label, "sum", kind, x.pick, "七球总和小于等于174为小、大于等于175为大；单双按总和奇偶判定，无和局。", seven, false)
		m.Category = "two_sides"
		if kind == "lhc_size" {
			m.SmallMax, m.BigMin = 174, 175
		}
	}
	combos := []struct{ pick, label string }{{"big_odd", "大单"}, {"big_even", "大双"}, {"small_odd", "小单"}, {"small_even", "小双"}}
	for _, x := range combos {
		add("special.combo."+x.pick, "特码半特", x.label, "special", "lhc_size_parity", x.pick, "特码同时符合大小及单双才中奖：01–24小、25–48大；49时全部退本。", []int{7}, true)
	}
	for _, x := range []struct{ pick, label string }{{"sky", "天肖"}, {"earth", "地肖"}, {"front", "前肖"}, {"back", "后肖"}, {"domestic", "家肖"}, {"wild", "野肖"}} {
		add("special.zodiac_group."+x.pick, "特码生肖组", x.label, "special", "lhc_zodiac_group", x.pick, "天肖牛兔龙马猴猪，地肖鼠虎蛇羊鸡狗；前肖鼠牛虎兔龙蛇，后肖马羊猴鸡狗猪；家肖牛马羊鸡狗猪，野肖鼠虎龙蛇兔猴。只看特码，49时全部退本。", []int{7}, true)
	}
	for _, color := range colors {
		for _, face := range faces {
			add("special.half."+color.pick+"_"+face.pick, "特码半波", color.label+face.label, "half_wave", "lhc_half_wave", color.pick+"_"+face.pick, "特码同时符合色波与大小或单双才中奖，01–24小、25–48大；49时所有半波退本。", []int{7}, true)
		}
		for _, face := range faces[2:] {
			add("special.halfdigit."+color.pick+"_"+face.pick, "特码半波合数", color.label+"合"+face.label, "half_wave", "lhc_half_digit", color.pick+"_"+face.pick, "特码同时符合色波及十位与个位之和的单双才中奖；49时全部退本。", []int{7}, true)
		}
		for _, x := range combos {
			add("special.halfhalf."+color.pick+"_"+x.pick, "特码半半波", color.label+x.label, "special", "lhc_half_half_wave", color.pick+"_"+x.pick, "特码同时符合色波、大小及单双才中奖；49时所有半半波退本。", []int{7}, true)
		}
	}
	for n := 0; n <= 4; n++ {
		add(fmt.Sprintf("special.head.n%d", n), "特码头数", fmt.Sprintf("%d头", n), "special", "lhc_head", strconv.Itoa(n), "仅按特码十位数判定，49为4头，正常判奖。", []int{7}, false)
	}
	for n := 0; n <= 9; n++ {
		add(fmt.Sprintf("special.tail.n%d", n), "特码尾数", fmt.Sprintf("%d尾", n), "special", "lhc_tail", strconv.Itoa(n), "仅按特码个位数判定，49为9尾，正常判奖。", []int{7}, false)
	}
	for _, x := range []struct{ pick, label string }{{"metal", "金"}, {"wood", "木"}, {"water", "水"}, {"fire", "火"}, {"earth", "土"}} {
		add("special.element."+x.pick, "五行", x.label, "special", "lhc_element", x.pick, "仅按特码所属五行判奖，使用本玩法固定号码表；49属火，正常判奖。", []int{7}, false)
	}
	for n := 1; n <= 49; n++ {
		add("regular.n"+strconv.Itoa(n), "正码", fmt.Sprintf("%02d", n), "regular", "lhc_any_number", strconv.Itoa(n), "所选号码出现在前六个正码任意位置即中奖，特码不计，49正常判奖。", regular, false)
	}
	for zodiac, label := range markSixAnimals {
		add("any.zodiac.z"+strconv.Itoa(zodiac), "一肖", label, "zodiac_tail", "lhc_zodiac", strconv.Itoa(zodiac), "七球中含所选生肖即中奖，同肖出现多次只派彩一次，49正常判奖。", seven, false)
		m := add("regular.zodiac.z"+strconv.Itoa(zodiac), "正肖", label, "zodiac_tail", "lhc_zodiac_count", strconv.Itoa(zodiac), "只计算前六个正码：所选生肖出现几次，就按锁定的含本金赔率派彩几次；特码不计，49正常判奖。", regular, false)
		m.MaxPayoutMultiplier = 5
		add("special.zodiac.z"+strconv.Itoa(zodiac), "特码生肖", label, "special_zodiac", "lhc_zodiac", strconv.Itoa(zodiac), "仅特码生肖与所选生肖相同即中奖；49正常判奖，不退本。", []int{7}, false)
	}
	for tail := 0; tail < 10; tail++ {
		add("any.tail.t"+strconv.Itoa(tail), "正特尾数", fmt.Sprintf("%d尾", tail), "zodiac_tail", "lhc_tail", strconv.Itoa(tail), "七球中含所选尾数即中奖，同尾出现多次只派彩一次；0尾为10、20、30、40，49正常判奖。", seven, false)
	}
	for count := 2; count <= 7; count++ {
		add(fmt.Sprintf("total_zodiac.n%d", count), "总肖", fmt.Sprintf("%d肖", count), "sum", "lhc_total_zodiac", strconv.Itoa(count), "计算七球中不同生肖的数量，数量与所选值相同即中奖；同肖只计一类，49参与计算。", seven, false)
	}
	for _, x := range []struct{ pick, label string }{{"odd", "总肖单"}, {"even", "总肖双"}} {
		add("total_zodiac."+x.pick, "总肖", x.label, "sum", "lhc_total_zodiac", x.pick, "按七球中不同生肖数量的奇偶判奖；同肖只计一类，49参与计算。", seven, false)
	}
	for _, x := range []struct{ pick, label string }{{"red", "红波"}, {"blue", "蓝波"}, {"green", "绿波"}, {"tie", "和局"}} {
		add("seven_wave."+x.pick, "七色波", x.label, "sum", "lhc_seven_wave", x.pick, "正码每球对应色波计1分，特码计1.5分，最高得分的颜色中奖；最高分并列时所有颜色投注退本，和局投注中奖。", seven, false)
	}
	combination := func(id, group, label, ui, kind, pick, rule, domain, mode string, count, maxPicks int, positions []int, push bool) *Market {
		m := add(id, group, label, ui, kind, pick, rule, positions, push)
		low, high := 1, 49
		if domain == "zodiac" {
			low, high = 0, 11
		}
		if domain == "tail" {
			low, high = 0, 9
		}
		m.Selection = &SelectionSchema{Type: domain, Mode: mode, PickCount: count, MinValue: low, MaxValue: high, MaxPicks: maxPicks}
		return m
	}
	for _, x := range []struct {
		id, label, rule string
		count           int
	}{
		{"three_two", "三中二", "每组3个号码：恰好2个在前六正码按中二档派彩，3个全在前六正码按中三档派彩；同组只派命中的一档，不拆单、不重复派彩。", 3},
		{"three_all", "三全中", "每组3个号码，全部出现在前六正码才中奖；特码不计。", 3},
		{"two_all", "二全中", "每组2个号码，全部出现在前六正码才中奖；特码不计。", 2},
		{"two_special", "二中特", "每组2个号码：1正码加1特码按中特档，2个正码按中二档；同组只派一个命中档。", 2},
		{"special_pair", "特串", "每组2个号码，必须1个正码加1个特码才中奖；2个正码不中奖。", 2},
		{"four_all", "四全中", "每组4个号码，全部出现在前六正码才中奖；特码不计。", 4},
	} {
		m := combination("link."+x.id, "连码", x.label, "linked_numbers", "lhc_link_numbers", x.id, x.rule, "number", "combination", x.count, 49, seven, false)
		if x.id == "three_two" {
			m.PayoutTiers = []PayoutTier{{Key: "hit2", Label: "中二", DefaultOdds: 1000}, {Key: "hit3", Label: "中三", DefaultOdds: 1000}}
		}
		if x.id == "two_special" {
			m.PayoutTiers = []PayoutTier{{Key: "special", Label: "中特", DefaultOdds: 1000}, {Key: "hit2", Label: "中二", DefaultOdds: 1000}}
		}
	}
	for count := 2; count <= 5; count++ {
		combination(fmt.Sprintf("link_zodiac.%d", count), "连肖", fmt.Sprintf("%d连肖中", count), "linked_zodiac", "lhc_link_zodiac", strconv.Itoa(count), "全部所选生肖都须出现在开奖七球中，才算本组合中奖；同肖多次只派一次，49正常判奖。", "zodiac", "combination", count, 12, seven, false)
		combination(fmt.Sprintf("link_tail.%d", count), "尾数连", fmt.Sprintf("%d连尾", count), "linked_tail", "lhc_link_tail", strconv.Itoa(count), "全部所选尾数都须出现在开奖七球中，才算本组合中奖；同尾多次只派一次，49正常判奖。", "tail", "combination", count, 10, seven, false)
	}
	for count := 2; count <= 4; count++ {
		combination(fmt.Sprintf("miss_zodiac.%d", count), "连肖不中", fmt.Sprintf("%d连肖不中", count), "linked_zodiac", "lhc_miss_zodiac", strconv.Itoa(count), "全部所选生肖均未出现在开奖七球中才中奖，任一生肖开出即输；49参与判奖。", "zodiac", "combination", count, 12, seven, false)
	}
	for count := 2; count <= 11; count++ {
		combination(fmt.Sprintf("combined_zodiac.in.%d", count), "合肖", fmt.Sprintf("%d肖中", count), "combined_zodiac", "lhc_combined_zodiac", "in", "2–11个不同生肖合为一注，仅特码生肖在组内即中奖，前六正码不参与；特码49时退回本金。", "zodiac", "set", count, count, []int{7}, true)
	}
	for count := 5; count <= 12; count++ {
		combination(fmt.Sprintf("miss.%d", count), "全不中", fmt.Sprintf("%d不中", count), "miss", "lhc_miss", strconv.Itoa(count), "本组所有号码均未出现在正码及特码中才中奖，任一个开出即输；49参与判奖。复式各组分别计费及判奖。", "number", "combination", count, 49, seven, false)
	}
	return all
}

var markSixV6Contracts = func() map[string]Market {
	result := map[string]Market{}
	for _, m := range markSixV6Catalog() {
		if m.RuleVersion == 6 {
			result[m.ID] = m
		}
	}
	return result
}()

func sameMarkSixV6Contract(m, expected Market) bool {
	return m.RuleVersion == 6 && m.ID == expected.ID && m.Kind == expected.Kind && m.Pick == expected.Pick && m.SmallMax == expected.SmallMax && m.BigMin == expected.BigMin && m.MaxPayoutMultiplier == expected.MaxPayoutMultiplier && reflect.DeepEqual(m.Positions, expected.Positions) && reflect.DeepEqual(m.PushValues, expected.PushValues) && reflect.DeepEqual(m.Selection, expected.Selection) && reflect.DeepEqual(m.PayoutTiers, expected.PayoutTiers)
}
func markSixV6ZodiacMarket(m Market) bool {
	switch m.Kind {
	case "lhc_zodiac", "lhc_zodiac_count", "lhc_zodiac_group", "lhc_total_zodiac", "lhc_link_zodiac", "lhc_miss_zodiac", "lhc_combined_zodiac":
		return true
	}
	return false
}
func bindMarkSixV6(m Market, values []int, drawAt time.Time) (Market, error) {
	expected, ok := markSixV6Contracts[m.ID]
	if !ok || !sameMarkSixV6Contract(m, expected) {
		return Market{}, errors.New("不支持的新版六合彩合约")
	}
	picks, err := selectionValues(m, values)
	if err != nil {
		return Market{}, err
	}
	m.SelectionValues = picks
	m.ZodiacYear = 0
	if markSixV6ZodiacMarket(m) {
		m.ZodiacYear = MarkSixLunarYear(drawAt)
	}
	return m, nil
}
func markSixV6Element(n int) string {
	switch n {
	case 4, 5, 12, 13, 26, 27, 34, 35, 42, 43:
		return "metal"
	case 8, 9, 16, 17, 24, 25, 38, 39, 46, 47:
		return "wood"
	case 1, 14, 15, 22, 23, 30, 31, 44, 45:
		return "water"
	case 2, 3, 10, 11, 18, 19, 32, 33, 40, 41, 48, 49:
		return "fire"
	case 6, 7, 20, 21, 28, 29, 36, 37:
		return "earth"
	}
	return ""
}
func markSixV6ZodiacGroup(z int, pick string) bool {
	switch pick {
	case "sky":
		return z == 1 || z == 3 || z == 4 || z == 6 || z == 8 || z == 11
	case "earth":
		return z == 0 || z == 2 || z == 5 || z == 7 || z == 9 || z == 10
	case "front":
		return z >= 0 && z <= 5
	case "back":
		return z >= 6 && z <= 11
	case "domestic":
		return z == 1 || z == 6 || z == 7 || z == 9 || z == 10 || z == 11
	case "wild":
		return z == 0 || z == 2 || z == 3 || z == 4 || z == 5 || z == 8
	}
	return false
}
func evaluateMarkSixV6(m Market, numbers []int, drawAt time.Time) (Evaluation, error) {
	expected, ok := markSixV6Contracts[m.ID]
	if !ok || !sameMarkSixV6Contract(m, expected) {
		return Evaluation{}, errors.New("不支持的新版六合彩合约")
	}
	values, err := selectionValues(m, m.SelectionValues)
	if err != nil || !reflect.DeepEqual(values, m.SelectionValues) {
		return Evaluation{}, errors.New("六合彩组选快照无效")
	}
	if markSixV6ZodiacMarket(m) {
		if m.ZodiacYear != MarkSixLunarYear(drawAt) {
			return Evaluation{}, errors.New("生肖年份快照与开奖期不一致")
		}
	} else if m.ZodiacYear != 0 {
		return Evaluation{}, errors.New("非生肖玩法含无效年份快照")
	}
	if len(numbers) != 7 {
		return Evaluation{}, errors.New("六合彩应开出7个号码")
	}
	seen := [50]bool{}
	for _, n := range numbers {
		if n < 1 || n > 49 || seen[n] {
			return Evaluation{}, errors.New("六合彩号码应为01–49且不可重复")
		}
		seen[n] = true
	}
	selected := make([]int, 0, len(m.Positions))
	for _, p := range m.Positions {
		if p < 1 || p > 7 {
			return Evaluation{}, errors.New("六合彩位置无效")
		}
		selected = append(selected, numbers[p-1])
	}
	n := selected[0]
	if len(m.PushValues) > 0 && n == 49 {
		return Evaluation{Outcome: "push"}, nil
	}
	pickNumber, _ := strconv.Atoi(m.Pick)
	win, tier, multiplier := false, "", int64(1)
	face := func(v int, pick string) bool {
		switch pick {
		case "big":
			return v >= 25
		case "small":
			return v <= 24
		case "odd":
			return v%2 == 1
		case "even":
			return v%2 == 0
		}
		return false
	}
	switch m.Kind {
	case "lhc_size", "lhc_parity":
		sum := 0
		for _, v := range selected {
			sum += v
		}
		if m.Kind == "lhc_size" {
			win = m.Pick == "small" && sum <= m.SmallMax || m.Pick == "big" && sum >= m.BigMin
		} else {
			win = m.Pick == "odd" && sum%2 == 1 || m.Pick == "even" && sum%2 == 0
		}
	case "lhc_digit_size":
		sum := n/10 + n%10
		win = m.Pick == "small" && sum <= m.SmallMax || m.Pick == "big" && sum >= m.BigMin
	case "lhc_digit_parity":
		sum := n/10 + n%10
		win = m.Pick == "odd" && sum%2 == 1 || m.Pick == "even" && sum%2 == 0
	case "lhc_tail_size":
		win = m.Pick == "small" && n%10 <= m.SmallMax || m.Pick == "big" && n%10 >= m.BigMin
	case "lhc_color":
		win = MarkSixColor(n) == m.Pick
	case "lhc_size_parity":
		parts := strings.Split(m.Pick, "_")
		win = face(n, parts[0]) && face(n, parts[1])
	case "lhc_half_wave":
		parts := strings.Split(m.Pick, "_")
		win = MarkSixColor(n) == parts[0] && face(n, parts[1])
	case "lhc_half_digit":
		parts := strings.Split(m.Pick, "_")
		win = MarkSixColor(n) == parts[0] && face(n/10+n%10, parts[1])
	case "lhc_half_half_wave":
		parts := strings.Split(m.Pick, "_")
		win = MarkSixColor(n) == parts[0] && face(n, parts[1]) && face(n, parts[2])
	case "lhc_head":
		win = n/10 == pickNumber
	case "lhc_element":
		win = markSixV6Element(n) == m.Pick
	case "lhc_zodiac_group":
		win = markSixV6ZodiacGroup(MarkSixZodiac(n, m.ZodiacYear), m.Pick)
	case "lhc_any_number":
		for _, v := range selected {
			win = win || v == pickNumber
		}
	case "lhc_zodiac", "lhc_zodiac_count":
		count := int64(0)
		for _, v := range selected {
			if MarkSixZodiac(v, m.ZodiacYear) == pickNumber {
				count++
			}
		}
		win = count > 0
		if m.Kind == "lhc_zodiac_count" {
			multiplier = count
		}
	case "lhc_tail":
		for _, v := range selected {
			win = win || v%10 == pickNumber
		}
	case "lhc_total_zodiac":
		classes := map[int]bool{}
		for _, v := range selected {
			classes[MarkSixZodiac(v, m.ZodiacYear)] = true
		}
		count := len(classes)
		win = count == pickNumber || m.Pick == "odd" && count%2 == 1 || m.Pick == "even" && count%2 == 0
	case "lhc_seven_wave":
		scores := map[string]int{"red": 0, "blue": 0, "green": 0}
		for i, v := range numbers {
			score := 2
			if i == 6 {
				score = 3
			}
			scores[MarkSixColor(v)] += score
		}
		highest, ties, winner := 0, 0, ""
		for color, score := range scores {
			if score > highest {
				highest, ties, winner = score, 1, color
			} else if score == highest {
				ties++
			}
		}
		if ties > 1 {
			if m.Pick == "tie" {
				win = true
			} else {
				return Evaluation{Outcome: "push"}, nil
			}
		} else {
			win = m.Pick == winner
		}
	case "lhc_link_numbers":
		regularHits, specialHits := 0, 0
		for _, v := range values {
			for _, number := range numbers[:6] {
				if number == v {
					regularHits++
				}
			}
			if numbers[6] == v {
				specialHits++
			}
		}
		switch m.Pick {
		case "three_two":
			if regularHits == 3 {
				win, tier = true, "hit3"
			} else if regularHits == 2 {
				win, tier = true, "hit2"
			}
		case "two_special":
			if regularHits == 1 && specialHits == 1 {
				win, tier = true, "special"
			} else if regularHits == 2 {
				win, tier = true, "hit2"
			}
		case "three_all", "two_all", "four_all":
			win = regularHits == len(values)
		case "special_pair":
			win = regularHits == 1 && specialHits == 1
		default:
			return Evaluation{}, errors.New("不支持的连码规则")
		}
	case "lhc_link_zodiac", "lhc_link_tail", "lhc_miss_zodiac":
		classes := map[int]bool{}
		for _, v := range numbers {
			if m.Kind != "lhc_link_tail" {
				classes[MarkSixZodiac(v, m.ZodiacYear)] = true
			} else {
				classes[v%10] = true
			}
		}
		win = true
		for _, v := range values {
			if m.Kind == "lhc_miss_zodiac" {
				win = win && !classes[v]
			} else {
				win = win && classes[v]
			}
		}
	case "lhc_combined_zodiac":
		zodiac := MarkSixZodiac(numbers[6], m.ZodiacYear)
		for _, v := range values {
			win = win || v == zodiac
		}
	case "lhc_miss":
		win = true
		for _, v := range values {
			win = win && !seen[v]
		}
	default:
		return Evaluation{}, errors.New("不支持的六合彩玩法")
	}
	if win {
		return Evaluation{Outcome: "win", Tier: tier, Multiplier: multiplier}, nil
	}
	return Evaluation{Outcome: "lose"}, nil
}
