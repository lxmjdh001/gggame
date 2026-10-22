package games

import (
	"fmt"
	"starlight/gameapi/pkg/model"
	"time"
)

// Sydney49Start is an immutable issue boundary. Earlier source minutes retain
// the historical 45-number projection; their published results are never rewritten.
func Sydney49Start() time.Time {
	return time.Date(2026, 10, 16, 22, 0, DrawSecond, 0, model.Beijing)
}

// Compare issue minutes so both stored :01 timestamps and business :05
// timestamps select the same rule on the transition issue.
func (s Session) IsLegacySydney(drawAt time.Time) bool {
	return s.ID == "lhc-au" && drawAt.Truncate(time.Minute).Before(Sydney49Start().Truncate(time.Minute))
}
func (s Session) NumberRuleAt(drawAt time.Time) model.Rule {
	rule := s.NumberRule()
	if s.IsLegacySydney(drawAt) {
		rule.Max, rule.Count = 45, 8
	}
	return rule
}
func (s Session) MarketCatalogIDAt(drawAt time.Time) string {
	if s.IsLegacySydney(drawAt) {
		return "lhc-au"
	}
	return s.MarketCatalogID()
}

func (s Session) sydneySourcePositions(raw []int, drawAt time.Time) ([]int, error) {
	if len(raw) != 49 {
		return nil, fmt.Errorf("Sydney source must contain 49 numbers")
	}
	seen := map[int]bool{}
	eligible := make([]int, 0, 45)
	for i, n := range raw {
		if n < 1 || n > 49 || seen[n] {
			return nil, fmt.Errorf("invalid Sydney source permutation")
		}
		seen[n] = true
		if n <= 45 {
			eligible = append(eligible, i+1)
		}
	}
	if s.IsLegacySydney(drawAt) {
		// Keep original source indices for auditability; never reduce values modulo 45.
		return append([]int(nil), eligible[7:15]...), nil
	}
	return []int{8, 9, 10, 11, 12, 13, 14}, nil
}
