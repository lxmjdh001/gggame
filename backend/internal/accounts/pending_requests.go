package accounts

import (
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

type PendingRequests struct {
	Total          int             `json:"total"`
	DepositCount   int             `json:"deposit_count"`
	WithdrawCount  int             `json:"withdraw_count"`
	DepositAmount  int64           `json:"deposit_amount_cents"`
	WithdrawAmount int64           `json:"withdraw_amount_cents"`
	LatestID       string          `json:"latest_id"`
	Items          []WalletRequest `json:"items"`
}

// Count all pending requests, independent of the paginated/searchable review table.
// IDs are fixed-width increasing sequences, so the newest pending ID detects new
// arrivals even when another administrator processes a request at the same time.
func (s *Store) PendingRequests(actor string) (PendingRequests, error) {
	out := PendingRequests{Items: []WalletRequest{}}
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		c := tx.Bucket([]byte("requests")).Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var request WalletRequest
			if err := json.Unmarshal(v, &request); err != nil {
				return err
			}
			if request.Status != "pending" {
				continue
			}
			switch request.Kind {
			case "deposit":
				out.DepositCount++
				out.DepositAmount += request.Amount
			case "withdraw":
				out.WithdrawCount++
				out.WithdrawAmount += request.Amount
			default:
				continue
			}
			out.Total++
			if out.LatestID == "" {
				out.LatestID = request.ID
			}
			if len(out.Items) < 5 {
				out.Items = append(out.Items, request)
			}
		}
		return nil
	})
	return out, err
}
