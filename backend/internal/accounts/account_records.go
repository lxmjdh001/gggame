package accounts

import (
	"sort"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

type AccountRecordPage[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func accountRecordsPage[T any](s *Store, id string, page int, bucket string, owner func(T) string, display func(T) T) (AccountRecordPage[T], error) {
	out := AccountRecordPage[T]{Items: []T{}, PageSize: orderPageSize}
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, err := active(tx, id); err != nil {
			return err
		}
		all, err := list[T](tx, bucket)
		if err != nil {
			return err
		}
		own := []T{}
		for _, v := range all {
			if owner(v) == id {
				if display != nil {
					v = display(v)
				}
				own = append(own, v)
			}
		}
		out.Total = len(own)
		current, start, end := pageBounds(out.Total, page, out.PageSize)
		out.Page = current
		out.Items = own[start:end]
		return nil
	})
	return out, err
}
func (s *Store) LedgerPage(id string, page int) (AccountRecordPage[Ledger], error) {
	return accountRecordsPage(s, id, page, "ledger", func(v Ledger) string { return v.UserID }, ledgerForDisplay)
}
func (s *Store) TransactionsPage(id string, page int) (AccountRecordPage[WalletRequest], error) {
	return accountRecordsPage(s, id, page, "requests", func(v WalletRequest) string { return v.UserID }, nil)
}

type DailyResult struct {
	Date   string `json:"date"`
	Orders int    `json:"orders"`
	Stake  int64  `json:"stake_cents"`
	Payout int64  `json:"payout_cents"`
	Net    int64  `json:"net_cents"`
}

// Group all settled orders by Beijing bet date, before pagination. Refunds and
// pending orders do not represent a realized win or loss.
func (s *Store) DailyResultsPage(id string, page int) (AccountRecordPage[DailyResult], error) {
	out := AccountRecordPage[DailyResult]{Items: []DailyResult{}, PageSize: orderPageSize}
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, err := active(tx, id); err != nil {
			return err
		}
		orders, err := list[Order](tx, "orders")
		if err != nil {
			return err
		}
		byDate := map[string]DailyResult{}
		for _, o := range orders {
			if o.UserID != id || o.Status != "settled" || o.CreatedAt.IsZero() {
				continue
			}
			day := o.CreatedAt.In(model.Beijing).Format("2006-01-02")
			v := byDate[day]
			v.Date = day
			v.Orders++
			v.Stake += o.Cost
			v.Payout += o.Payout
			v.Net += o.Payout - o.Cost
			byDate[day] = v
		}
		days := make([]DailyResult, 0, len(byDate))
		for _, v := range byDate {
			days = append(days, v)
		}
		sort.Slice(days, func(i, j int) bool { return days[i].Date > days[j].Date })
		out.Total = len(days)
		current, start, end := pageBounds(out.Total, page, out.PageSize)
		out.Page = current
		out.Items = days[start:end]
		return nil
	})
	return out, err
}
