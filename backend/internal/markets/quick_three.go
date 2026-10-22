package markets

import (
	"slices"
	"strconv"
	"strings"
)

// All nine rooms offer the same 31 selections. Retired pair/double markets
// remain in historical catalogs solely for accepted order snapshots.
func quickThreeCatalog() []Market {
	historical := quickThreeV3Catalog()
	result := make([]Market, 0, 31)
	for _, m := range historical {
		if m.Kind != "dice_pair" && m.Kind != "dice_double" {
			result = append(result, m)
		}
	}
	return result
}

// This complete v3 contract remains available to settlement validation even
// after a selection is retired from new betting.
func quickThreeV3Catalog() []Market {
	result := quickThreeV2Catalog()
	for i := range result {
		m := &result[i]
		if m.Kind != "dice_face" && m.Kind != "dice_double" {
			continue
		}
		m.ID = strings.Replace(m.ID, "k3v2.", "k3v3.", 1)
		m.RuleVersion = 3
		if m.Kind == "dice_face" {
			m.Rule = "三个骰子中任意一个与所选点数相同即中奖；出现1次或2次均只派彩一次，不翻倍。出现任意围骰时，三军不中奖，不退本金。"
		} else {
			m.Rule = "三个骰子中恰有两个与所选点数相同即中奖，顺序不限，每注仅派彩一次。出现任意围骰时，短牌不中奖，不退本金。"
		}
	}
	return result
}

// This contract is retained for accepted v2 snapshots. Do not change its
// settlement fields when changing the catalog available for new orders.
func quickThreeV2Catalog() []Market {
	result := make([]Market, 0, 52)
	add := func(id, group, label, category, kind, pick, rule string, small, big int) {
		result = append(result, Market{
			ID: "k3v2." + id, Group: group, Label: label, Category: category,
			Kind: kind, Positions: []int{1, 2, 3}, Pick: pick,
			SmallMax: small, BigMin: big, Rule: rule, RuleVersion: 2, DefaultOdds: 1000,
		})
	}
	for _, option := range []struct{ pick, label string }{{"big", "大"}, {"small", "小"}} {
		add("sum."+option.pick, "双面", option.label, "two_sides", "size", option.pick,
			"三个骰子的点数总和4–10为小，11–17为大；出现111、222、333、444、555或666时，大小均不中奖，不退本金。", 10, 11)
	}
	for _, option := range []struct{ pick, label string }{{"odd", "单"}, {"even", "双"}} {
		add("sum."+option.pick, "双面", option.label, "two_sides", "parity", option.pick,
			"按三个骰子的点数总和判定单双；出现111、222、333、444、555或666时，单双均不中奖，不退本金。", 0, 0)
	}
	for n := 1; n <= 6; n++ {
		pick := strconv.Itoa(n)
		add("single.n"+pick, "三军", pick, "regular", "dice_face", pick,
			"三个骰子中任意一个与所选点数相同即中奖；不论所选点数出现1次、2次或3次，每注仅派彩一次，不翻倍。", 0, 0)
	}
	for n := 1; n <= 6; n++ {
		pick := strconv.Itoa(n)
		label := strings.Repeat(pick, 3)
		add("triple.n"+pick, "围骰", label, "regular", "triple", pick,
			"三个骰子必须全部与所选点数相同才中奖，例如选择111仅在三个骰子均为1时中奖；其他围骰不中奖。", 0, 0)
	}
	add("triple.any", "全骰", "任意围骰", "regular", "triple", "any",
		"三个骰子点数相同即中奖：111、222、333、444、555、666均算中，每注只派彩一次。", 0, 0)
	for n := 4; n <= 17; n++ {
		pick := strconv.Itoa(n)
		add("sum.n"+pick, "点数", pick, "regular", "number", pick,
			"点数是三个骰子的总和，可选4–17；总和与所选点数相同即中奖，开出3或18不中奖。围骰仍按总和判断，例如222对应6。", 0, 0)
	}
	for a := 1; a <= 5; a++ {
		for b := a + 1; b <= 6; b++ {
			left, right := strconv.Itoa(a), strconv.Itoa(b)
			add("pair."+left+"_"+right, "长牌", left+"-"+right, "regular", "dice_pair", left+","+right,
				"选择两个不同点数；三个骰子中同时出现所选两个点数即中奖，顺序不限，每注仅派彩一次。例如开出1、2、3时，1-2、1-3、2-3均中奖。", 0, 0)
		}
	}
	for n := 1; n <= 6; n++ {
		pick := strconv.Itoa(n)
		add("double.n"+pick, "短牌", pick+"-"+pick, "regular", "dice_double", pick,
			"三个骰子中至少两个与所选点数相同即中奖，顺序不限；出现所选点数的围骰也中奖，每注仅派彩一次。", 0, 0)
	}
	return result
}

func validQuickThreeMarket(m Market) bool {
	catalog := quickThreeV3Catalog()
	if m.RuleVersion == 2 {
		catalog = quickThreeV2Catalog()
	}
	for _, expected := range catalog {
		if m.ID == expected.ID {
			return m.RuleVersion == expected.RuleVersion && m.Kind == expected.Kind && m.Pick == expected.Pick &&
				slices.Equal(m.Positions, expected.Positions) && len(m.PushValues) == 0 &&
				m.SmallMax == expected.SmallMax && m.BigMin == expected.BigMin
		}
	}
	return false
}
