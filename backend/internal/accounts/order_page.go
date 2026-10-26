package accounts

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"starlight/gameapi/pkg/model"

	bolt "go.etcd.io/bbolt"
)

const orderPageSize = 10

type OrderPage struct {
	Items       []Order `json:"items"`
	Total       int     `json:"total"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
	TotalStake  int64   `json:"total_stake_cents"`
	TotalPayout int64   `json:"total_payout_cents"`
}

func requestPage(r *http.Request) (int, error) {
	if r.URL.Query().Get("page") == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 || page > 1000000 {
		return 0, errors.New("页码不正确")
	}
	return page, nil
}

func pageBounds(total, page, size int) (current, start, end int) {
	last := max(1, (total+size-1)/size)
	current = min(max(page, 1), last)
	start = (current - 1) * size
	end = min(start+size, total)
	return
}

// OrdersPage returns all of the authenticated user's history, without Account's
// recent-record limit. Filtering happens before counting and pagination.
func (s *Store) OrdersPage(id string, page int) (OrderPage, error) {
	return s.OrdersPageFiltered(id, page, "")
}

// Matches the member-facing result status, including historical manually settled orders.
func orderResultStatus(o Order) string {
	if o.Status != "settled" {
		return o.Status
	}
	if o.Outcome == "push" {
		return "push"
	}
	if o.Outcome == "win" || (o.Outcome == "" && o.Payout > 0) {
		return "win"
	}
	return "lose"
}

func (s *Store) OrdersPageFiltered(id string, page int, status string) (OrderPage, error) {
	return s.OrdersPageRange(id, page, status, "", "")
}

// orderDate parses a strict calendar date independently of the server's local timezone.
func orderDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	day, err := time.ParseInLocation("2006-01-02", value, model.Beijing)
	if err != nil || len(value) != 10 || day.Year() < 1 || day.Format("2006-01-02") != value {
		return time.Time{}, errors.New("投注日期不正确，请使用有效的YYYY-MM-DD日期")
	}
	return day, nil
}

// OrdersPageRange filters the full member history by status and Beijing creation
// dates before summarizing and paginating. Either date may be omitted; the end
// date includes its entire day, implemented as an exclusive next-day boundary.
func (s *Store) OrdersPageRange(id string, page int, status, startDate, endDate string) (OrderPage, error) {
	out := OrderPage{Items: []Order{}, PageSize: orderPageSize}
	switch status {
	case "", "pending", "win", "lose", "push", "refunded":
	default:
		return out, errors.New("投注状态不正确")
	}
	start, err := orderDate(startDate)
	if err != nil {
		return out, err
	}
	end, err := orderDate(endDate)
	if err != nil {
		return out, err
	}
	if startDate != "" && endDate != "" && start.After(end) {
		return out, errors.New("开始日期不能晚于结束日期")
	}
	if endDate != "" {
		end = end.AddDate(0, 0, 1)
	}
	err = s.db.View(func(tx *bolt.Tx) error {
		if _, err := active(tx, id); err != nil {
			return err
		}
		orders, err := list[Order](tx, "orders")
		if err != nil {
			return err
		}
		own := []Order{}
		for _, o := range orders {
			// Undated legacy records have no calendar day to match a date filter.
			if (startDate != "" || endDate != "") && o.CreatedAt.IsZero() {
				continue
			}
			if o.UserID == id && (status == "" || orderResultStatus(o) == status) &&
				(startDate == "" || !o.CreatedAt.Before(start)) && (endDate == "" || o.CreatedAt.Before(end)) {
				own = append(own, o)
				out.TotalStake += o.Cost
				// Withdrawn stakes are refunds, not prize payouts.
				if o.Status == "settled" {
					out.TotalPayout += o.Payout
				}
			}
		}
		out.Total = len(own)
		current, start, end := pageBounds(out.Total, page, orderPageSize)
		out.Page = current
		out.Items = own[start:end]
		return nil
	})
	return out, err
}
