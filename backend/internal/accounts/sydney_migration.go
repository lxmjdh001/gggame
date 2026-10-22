package accounts

import (
	"fmt"
	"strconv"
	"strings"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/markets"
)

// Sydney now shares the standard 49-number catalog. This migration changes
// quotes only: accepted order snapshots and retired quotes remain untouched.
func migrateSydney49Rules(tx *bolt.Tx) error {
	b := tx.Bucket([]byte("rule_migrations"))
	if b.Get([]byte("au49-v1")) != nil {
		return nil
	}
	quotes := []Quote{}
	if tx.Bucket([]byte("market_quotes")).Get([]byte("lhc-au")) != nil {
		var err error
		quotes, err = get[[]Quote](tx, "market_quotes", "lhc-au")
		if err != nil {
			return err
		}
	}
	// Preserve the historical catalog internally for accepted legacy orders and
	// historical-clock recovery; MarketBook never exposes these retired IDs.
	indices := map[string]int{}
	previous := map[string]Quote{}
	for i, q := range quotes {
		indices[q.MarketID] = i
		previous[q.MarketID] = q
	}
	for _, m := range markets.Catalog("lhc-au") {
		if _, exists := indices[m.ID]; !exists {
			indices[m.ID] = len(quotes)
			quotes = append(quotes, Quote{MarketID: m.ID, OddsMilli: m.DefaultOdds, Enabled: true})
		}
	}
	inherited := 0
	for _, m := range markets.LegacyMarkSixCatalog() {
		q := Quote{MarketID: m.ID, OddsMilli: m.DefaultOdds, Enabled: true}
		if existing, ok := indices[m.ID]; ok {
			q = quotes[existing]
		}
		if oldID := sydney49PreviousMarketID(m.ID); oldID != "" {
			if old, ok := previous[oldID]; ok {
				// A matching standard ID may be an archive from before the 45/8
				// migration. The latest au45 price and switch take precedence.
				q.OddsMilli, q.Enabled = old.OddsMilli, old.Enabled
				inherited++
			}
		}
		if existing, ok := indices[m.ID]; ok {
			quotes[existing] = q
		} else {
			indices[m.ID] = len(quotes)
			quotes = append(quotes, q)
		}
	}
	if err := put(tx, "market_quotes", "lhc-au", quotes); err != nil {
		return err
	}
	if err := audit(tx, "system", "rules.au49", "lhc-au", fmt.Sprintf("悉尼改为49选7（6正码＋1特码）；继承%d项位置对应报价，保留旧45选8报价和订单快照", inherited)); err != nil {
		return err
	}
	return b.Put([]byte("au49-v1"), []byte("done"))
}

func sydney49PreviousMarketID(id string) string {
	for _, prefix := range []string{"lhc4.", "size2.", "parity2."} {
		id = strings.TrimPrefix(id, prefix)
	}
	parts := strings.Split(id, ".")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "p") {
		return ""
	}
	position, err := strconv.Atoi(strings.TrimPrefix(parts[0], "p"))
	if err != nil || position < 1 || position > 7 {
		return ""
	}
	switch parts[1] {
	case "big", "small", "odd", "even":
		return "au45." + id
	}
	if strings.HasPrefix(parts[1], "n") {
		number, err := strconv.Atoi(strings.TrimPrefix(parts[1], "n"))
		if err == nil && number >= 1 && number <= 45 {
			return "au45." + id
		}
	}
	return ""
}

// Until the first 49-number issue, administrators still edit the active au45
// book. Carry those later edits into the already prepared replacement book.
func syncUpcomingSydney49Quotes(quotes []Quote) []Quote {
	indices := make(map[string]int, len(quotes))
	for i, q := range quotes {
		indices[q.MarketID] = i
	}
	for _, m := range markets.LegacyMarkSixCatalog() {
		old, ok := indices[sydney49PreviousMarketID(m.ID)]
		if !ok {
			continue
		}
		q := Quote{MarketID: m.ID, OddsMilli: quotes[old].OddsMilli, Enabled: quotes[old].Enabled}
		if existing, ok := indices[m.ID]; ok {
			quotes[existing] = q
		} else {
			indices[m.ID] = len(quotes)
			quotes = append(quotes, q)
		}
	}
	return quotes
}
