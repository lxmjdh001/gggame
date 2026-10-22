package accounts

import (
	"fmt"
	"maps"
	"strings"
)

func markSixV6PreviousIDs(id string) []string {
	if !strings.HasPrefix(id, "lhc6.") {
		return nil
	}
	if strings.HasPrefix(id, "lhc6.combined_zodiac.in.") {
		return []string{strings.Replace(id, "lhc6.", "lhc5.", 1)}
	}
	base := strings.TrimPrefix(id, "lhc6.")
	if strings.HasPrefix(base, "special.") {
		base = "p7." + strings.TrimPrefix(base, "special.")
	}
	parts := strings.Split(base, ".")
	if len(parts) != 2 {
		return nil
	}
	var position int
	if _, err := fmt.Sscanf(parts[0], "p%d", &position); err != nil || position < 1 || position > 7 {
		return nil
	}
	switch parts[1] {
	case "big", "small":
		return []string{"size2." + base, base}
	case "odd", "even":
		return []string{"parity2." + base, base}
	}
	return nil
}

func inheritMarkSixV6Quote(q Quote, existing []Quote) Quote {
	for _, previousID := range markSixV6PreviousIDs(q.MarketID) {
		for _, old := range existing {
			if old.MarketID == previousID {
				q.OddsMilli, q.Enabled = old.OddsMilli, old.Enabled
				return q
			}
		}
	}
	return q
}

// An interrupted/older initialization may have persisted a base quote before
// its tier fields. Fill only missing fields, preserving every configured value.
func initializeMissingTierOdds(quotes, defaults []Quote) ([]Quote, []string) {
	byID := make(map[string]Quote, len(defaults))
	for _, q := range defaults {
		if len(q.TierOddsMilli) > 0 {
			byID[q.MarketID] = q
		}
	}
	var changed []string
	for i := range quotes {
		q := &quotes[i]
		defaults, exists := byID[q.MarketID]
		if !exists {
			continue
		}
		q.TierOddsMilli = maps.Clone(q.TierOddsMilli)
		for key, odds := range defaults.TierOddsMilli {
			if _, exists := q.TierOddsMilli[key]; exists {
				continue
			}
			if q.TierOddsMilli == nil {
				q.TierOddsMilli = map[string]int64{}
			}
			q.TierOddsMilli[key] = odds
			changed = append(changed, q.MarketID+":"+key)
		}
	}
	return quotes, changed
}
