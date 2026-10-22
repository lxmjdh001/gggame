package accounts

import (
	"errors"
	"strconv"
	"strings"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/markets"
)

// The pre-45-number migration must compare against the historical defaults,
// not a current catalog whose IDs and settlement rules may have changed.
func legacyMarkSixDefaultOdds(id string) (int64, bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "p") {
		return 0, false
	}
	position, err := strconv.Atoi(strings.TrimPrefix(parts[0], "p"))
	if err != nil || position < 1 || position > 7 {
		return 0, false
	}
	switch parts[1] {
	case "big", "small", "odd", "even":
		return 1960, true
	}
	if strings.HasPrefix(parts[1], "n") {
		number, err := strconv.Atoi(strings.TrimPrefix(parts[1], "n"))
		if err == nil && number >= 1 && number <= 49 {
			return 48020, true
		}
	}
	return 0, false
}

// Refuse a rule change while legacy stakes are outstanding. New market IDs also
// reject submissions from browsers which still display the old rules/odds.
func migrateAustraliaRules(tx *bolt.Tx) error {
	b, err := tx.CreateBucketIfNotExists([]byte("rule_migrations"))
	if err != nil {
		return err
	}
	if b.Get([]byte("au45-v2")) != nil {
		return migrateSydney49Rules(tx)
	}
	orders, err := list[Order](tx, "orders")
	if err != nil {
		return err
	}
	for _, order := range orders {
		if order.SessionID == "lhc-au" && order.Status == "pending" {
			return errors.New("澳大利亚尚有旧规则未结算订单，必须结算后再升级45选8")
		}
	}
	if tx.Bucket([]byte("market_quotes")).Get([]byte("lhc-au")) != nil {
		old, err := get[[]Quote](tx, "market_quotes", "lhc-au")
		if err != nil {
			return err
		}
		// The original migration must remain pinned to its historical catalog
		// even after Sydney's current catalog changes back to 49 numbers.
		quotes := []Quote{}
		for _, market := range markets.Catalog("lhc-au") {
			quotes = append(quotes, Quote{MarketID: market.ID, OddsMilli: market.DefaultOdds, Enabled: true})
		}
		for i := range quotes {
			id := strings.TrimPrefix(quotes[i].MarketID, "au45.")
			for _, q := range old {
				if q.MarketID != id {
					continue
				}
				quotes[i].Enabled = q.Enabled
				legacyOdds, ok := legacyMarkSixDefaultOdds(id)
				if ok && q.OddsMilli != legacyOdds {
					quotes[i].OddsMilli = q.OddsMilli
				}
			}
		}
		// Archive retired quotes in the same record; MarketBook exposes only active IDs.
		if err = put(tx, "market_quotes", "lhc-au", append(old, quotes...)); err != nil {
			return err
		}
		if err = audit(tx, "system", "rules.au45", "lhc-au", "45选8，6正码+2附加号；23为和；保留自定义赔率及旧订单记录"); err != nil {
			return err
		}
	}
	if err = b.Put([]byte("au45-v2"), []byte("done")); err != nil {
		return err
	}
	return migrateSydney49Rules(tx)
}
