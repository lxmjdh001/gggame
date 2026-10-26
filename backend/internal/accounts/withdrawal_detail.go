package accounts

import (
	"errors"

	bolt "go.etcd.io/bbolt"
)

// Display the original hold ledger, never a balance reconstructed from today's
// account balance. Reads do not change the accepted request or stored history.
type WithdrawalDetail struct {
	Request       WalletRequest `json:"request"`
	BalanceBefore *int64        `json:"balance_before_cents"`
	BalanceAfter  *int64        `json:"balance_after_cents"`
}

func (s *Store) WithdrawalDetail(id, requestID string) (WithdrawalDetail, error) {
	var out WithdrawalDetail
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, err := active(tx, id); err != nil {
			return err
		}
		r, err := get[WalletRequest](tx, "requests", requestID)
		if err != nil {
			return err
		}
		if r.UserID != id || r.Kind != "withdraw" {
			return ErrNotFound
		}
		out.Request = r
		entries, err := list[Ledger](tx, "ledger")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.UserID != id || entry.Reference != r.ID || entry.Kind != "withdraw_hold" {
				continue
			}
			if out.BalanceAfter != nil || entry.Amount != -r.Amount {
				return errors.New("提款账变记录不一致，请联系客服核实")
			}
			before, after := entry.Balance-entry.Amount, entry.Balance
			out.BalanceBefore, out.BalanceAfter = &before, &after
		}
		return nil
	})
	return out, err
}
