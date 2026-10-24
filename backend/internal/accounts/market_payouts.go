package accounts

import (
	"errors"
	"fmt"
	"math/big"

	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func defaultMarketQuote(m markets.Market) Quote {
	q := Quote{MarketID: m.ID, OddsMilli: m.DefaultOdds, Enabled: true}
	if len(m.PayoutTiers) > 0 {
		q.TierOddsMilli = make(map[string]int64, len(m.PayoutTiers))
		for _, tier := range m.PayoutTiers {
			q.TierOddsMilli[tier.Key] = tier.DefaultOdds
		}
	}
	return q
}

func validateTierOdds(m markets.Market, odds map[string]int64) error {
	if len(odds) != len(m.PayoutTiers) {
		return errors.New("请提交该玩法完整的分档赔率，不可缺失或增加档位")
	}
	for _, tier := range m.PayoutTiers {
		value, exists := odds[tier.Key]
		if !exists || value < 1000 || value > 1000000 {
			return errors.New("分档赔率必须为1.000–1000.000且包含全部档位")
		}
	}
	return nil
}

func validateMarketQuote(m markets.Market, q Quote) error {
	if q.MarketID != m.ID || q.OddsMilli < 1000 || q.OddsMilli > 1000000 {
		return errors.New("赔率必须为1.000–1000.000")
	}
	if q.CurrentZodiacOddsMilli != 0 && (!supportsCurrentZodiacOdds(m) || q.CurrentZodiacOddsMilli < 1000 || q.CurrentZodiacOddsMilli > 1000000) {
		return errors.New("本年生肖赔率仅适用于生肖玩法，范围为1.000–1000.000")
	}
	if q.ZeroTailOddsMilli != 0 && (m.Kind != "lhc_link_tail" || q.ZeroTailOddsMilli < 1000 || q.ZeroTailOddsMilli > 1000000) {
		return errors.New("含0尾赔率仅适用于尾数连，范围为1.000–1000.000")
	}
	return validateTierOdds(m, q.TierOddsMilli)
}

func supportsCurrentZodiacOdds(m markets.Market) bool {
	return m.RuleVersion == 6 && (m.Kind == "lhc_zodiac" || m.Selection != nil && m.Selection.Type == "zodiac")
}

func selectionQuoteOdds(m markets.Market, q Quote) int64 {
	if q.CurrentZodiacOddsMilli != 0 && supportsCurrentZodiacOdds(m) && m.ZodiacYear != 0 {
		current := markets.MarkSixZodiac(1, m.ZodiacYear)
		if m.Selection == nil && m.Pick == fmt.Sprint(current) {
			return q.CurrentZodiacOddsMilli
		}
		for _, v := range m.SelectionValues {
			if v == current {
				return q.CurrentZodiacOddsMilli
			}
		}
	}
	if q.ZeroTailOddsMilli != 0 && m.Kind == "lhc_link_tail" {
		for _, v := range m.SelectionValues {
			if v == 0 {
				return q.ZeroTailOddsMilli
			}
		}
	}
	return q.OddsMilli
}

func maximumQuoteOdds(q Quote) int64 {
	if len(q.TierOddsMilli) == 0 {
		return q.OddsMilli
	}
	var maximum int64
	for _, value := range q.TierOddsMilli {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func marketPayoutLimit(m markets.Market) int64 {
	if m.MaxPayoutMultiplier == 0 {
		return 1
	}
	return m.MaxPayoutMultiplier
}

// Calculate the complete amount before rounding down to cents. Arbitrary
// precision avoids an intermediate overflow even for corrupt stored inputs.
func scaledMarketPayout(cost, odds, count int64) (int64, error) {
	if cost <= 0 || cost > maxMoney || odds < 1000 || odds > 1000000 || count <= 0 {
		return 0, errors.New("投注金额、锁定赔率或派彩次数不正确")
	}
	amount := new(big.Int).SetInt64(cost)
	amount.Mul(amount, big.NewInt(odds))
	amount.Mul(amount, big.NewInt(count))
	amount.Quo(amount, big.NewInt(1000))
	if !amount.IsInt64() || amount.Cmp(big.NewInt(maxMoney)) > 0 {
		return 0, errors.New("本注预计返还超过账户限额，请降低投注金额")
	}
	return amount.Int64(), nil
}

type marketSettlement struct {
	Outcome    string
	Payout     int64
	Tier       string
	Multiplier int64
	OddsMilli  int64
}

func calculateMarketDetailed(o Order, r games.Round) (marketSettlement, error) {
	var result marketSettlement
	if o.RuleSnapshot == nil || o.MarketID == "" || o.MarketID != o.RuleSnapshot.ID {
		return result, errors.New("旧版订单需人工结算或订单规则快照不匹配")
	}
	if o.Issue != r.Issue || r.Issue != r.DrawAt.In(model.Beijing).Format(model.IssueLayout) || r.SourceIssue != r.Issue {
		return result, errors.New("开奖结果期号不匹配")
	}
	g, exists := games.Find(o.SessionID)
	if !exists {
		return result, errors.New("场次不存在")
	}
	if r.SourceGroup != g.Group {
		return result, errors.New("开奖源组不匹配")
	}
	q := Quote{MarketID: o.MarketID, OddsMilli: o.OddsMilli, TierOddsMilli: o.TierOddsMilli}
	if err := validateMarketQuote(*o.RuleSnapshot, q); err != nil {
		return result, err
	}
	evaluation, err := markets.EvaluateDetailed(*o.RuleSnapshot, g, r)
	if err != nil {
		return result, err
	}
	result.Outcome = evaluation.Outcome
	switch evaluation.Outcome {
	case "win":
		if evaluation.Multiplier < 1 || evaluation.Multiplier > marketPayoutLimit(*o.RuleSnapshot) {
			return result, errors.New("派彩次数与锁定规则不匹配")
		}
		odds := o.OddsMilli
		if len(o.RuleSnapshot.PayoutTiers) > 0 {
			var exists bool
			odds, exists = o.TierOddsMilli[evaluation.Tier]
			if !exists {
				return result, errors.New("中奖档位与锁定赔率不匹配")
			}
		} else if evaluation.Tier != "" {
			return result, errors.New("非分档玩法返回了中奖档位")
		}
		result.Tier, result.Multiplier, result.OddsMilli = evaluation.Tier, evaluation.Multiplier, odds
		result.Payout, err = scaledMarketPayout(o.Cost, odds, evaluation.Multiplier)
		if err != nil {
			return marketSettlement{}, err
		}
	case "push", "lose":
		if evaluation.Tier != "" || evaluation.Multiplier != 0 {
			return result, errors.New("未中奖结果包含无效派彩档位或次数")
		}
		if o.Cost <= 0 || o.Cost > maxMoney {
			return result, errors.New("订单金额不正确")
		}
		if evaluation.Outcome == "push" {
			result.Payout = o.Cost
		}
	default:
		return result, errors.New("未知判奖结果")
	}
	return result, nil
}

func marketSettlementNote(m markets.Market, result marketSettlement) string {
	if result.Outcome == "push" {
		return "自动结算；和局退回本金"
	}
	if result.Outcome == "lose" {
		return "自动结算；未中奖"
	}
	label := ""
	for _, tier := range m.PayoutTiers {
		if tier.Key == result.Tier {
			label = tier.Label + "；"
		}
	}
	return fmt.Sprintf("自动结算；%s锁定赔率 %.3f；派彩%d次；win", label, float64(result.OddsMilli)/1000, result.Multiplier)
}
