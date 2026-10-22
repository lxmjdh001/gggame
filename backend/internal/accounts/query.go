package accounts

import (
	bolt "go.etcd.io/bbolt"
	"sort"
	"starlight/gameapi/pkg/model"
	"strings"
	"time"
)

type Member struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Level      int    `json:"level"`
	CreatedAt  string `json:"created_at"`
	Balance    int64  `json:"balance_cents"`
	TodayStake int64  `json:"today_stake_cents"`
}

// Normalize only the old system-generated caption; keep the original ledger immutable.
func ledgerForDisplay(v Ledger) Ledger {
	if v.Kind == "registration_bonus" && v.Note == "注册赠送（开发测试）" {
		v.Note = "注册赠送"
	}
	return v
}

func (s *Store) Account(id string) (map[string]any, error) {
	return s.accountAt(id, time.Now())
}

func (s *Store) accountAt(id string, now time.Time) (map[string]any, error) {
	local := now.In(model.Beijing)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, model.Beijing)
	dayEnd := dayStart.AddDate(0, 0, 1)
	out := map[string]any{}
	err := s.db.View(func(tx *bolt.Tx) error {
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		progress, e := wagering(tx, id, nil)
		if e != nil {
			return e
		}
		out["wagering"] = progress
		out["user"] = u.User
		if u.InviterID != "" {
			parent, err := get[storedUser](tx, "users", u.InviterID)
			if err != nil {
				return err
			}
			out["inviter"] = map[string]string{"id": parent.ID, "username": parent.Username}
			u.InviterUsername = parent.Username
			out["user"] = u.User
		}
		entries, e := list[Ledger](tx, "ledger")
		if e != nil {
			return e
		}
		mine := []Ledger{}
		for _, v := range entries {
			if v.UserID == id {
				mine = append(mine, ledgerForDisplay(v))
			}
		}
		if len(mine) > 200 {
			mine = mine[:200]
		}
		out["ledger"] = mine
		req, e := list[WalletRequest](tx, "requests")
		if e != nil {
			return e
		}
		requests := []WalletRequest{}
		for _, v := range req {
			if v.UserID == id {
				requests = append(requests, v)
			}
		}
		if len(requests) > 200 {
			requests = requests[:200]
		}
		out["requests"] = requests
		orders, e := list[Order](tx, "orders")
		if e != nil {
			return e
		}
		todayStakes := map[string]int64{}
		own := []Order{}
		bettingFrozen := int64(0)
		for _, v := range orders {
			if (v.Status == "pending" || v.Status == "settled") && !v.CreatedAt.Before(dayStart) && v.CreatedAt.Before(dayEnd) {
				todayStakes[v.UserID] += v.Cost
			}
			if v.UserID == id {
				own = append(own, v)
				if v.Status == "pending" {
					bettingFrozen += v.Cost
				}
			}
		}
		if len(own) > 200 {
			own = own[:200]
		}
		out["orders"] = own
		// Includes every pending order, before the 200-row display limit, across all issues.
		out["betting_frozen_cents"] = bettingFrozen
		cs, e := list[Commission](tx, "commissions")
		if e != nil {
			return e
		}
		commissions := []Commission{}
		available, total := int64(0), int64(0)
		for _, v := range cs {
			if v.UserID == id {
				total += v.Amount
				if v.Status == "available" {
					available += v.Amount
				}
				commissions = append(commissions, v)
			}
		}
		if len(commissions) > 200 {
			commissions = commissions[:200]
		}
		out["commissions"] = commissions
		out["commission_available_cents"] = available
		out["commission_total_cents"] = total
		users, e := list[storedUser](tx, "users")
		if e != nil {
			return e
		}
		byID := map[string]storedUser{}
		for _, v := range users {
			byID[v.ID] = v
		}
		members := []Member{}
		counts := [3]int{}
		for _, v := range users {
			parent := v.InviterID
			for level := 1; level <= 3 && parent != ""; level++ {
				if parent == id {
					members = append(members, Member{ID: v.ID, Username: v.Username, Level: level, CreatedAt: v.CreatedAt.Format("2006-01-02 15:04"), Balance: v.Balance, TodayStake: todayStakes[v.ID]})
					counts[level-1]++
					break
				}
				parent = byID[parent].InviterID
			}
		}
		sort.Slice(members, func(i, j int) bool { return members[i].CreatedAt > members[j].CreatedAt })
		if len(members) > 200 {
			members = members[:200]
		}
		out["members"] = members
		out["team_counts"] = counts
		return nil
	})
	return out, err
}
func (s *Store) AdminData(actor, resource, query string, page int) (map[string]any, error) {
	out := map[string]any{}
	err := s.db.View(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		rows := []any{}
		match := func(values ...string) bool {
			return query == "" || strings.Contains(strings.ToLower(strings.Join(values, " ")), strings.ToLower(query))
		}
		switch resource {
		case "users":
			v, e := list[storedUser](tx, "users")
			if e != nil {
				return e
			}
			orders, e := list[Order](tx, "orders")
			if e != nil {
				return e
			}
			bettingFrozen := map[string]int64{}
			for _, o := range orders {
				if o.Status == "pending" {
					bettingFrozen[o.UserID] += o.Cost
				}
			}
			sort.Slice(v, func(i, j int) bool { return v[i].CreatedAt.After(v[j].CreatedAt) })
			for _, x := range v {
				if match(x.Username, x.ID, x.InviteCode, x.Phone, x.Name) {
					if x.InviterID != "" {
						parent, err := get[storedUser](tx, "users", x.InviterID)
						if err != nil {
							return err
						}
						x.InviterUsername = parent.Username
					}
					rows = append(rows, struct {
						User
						BettingFrozen int64 `json:"betting_frozen_cents"`
					}{x.User, bettingFrozen[x.ID]})
				}
			}
		case "orders":
			v, e := list[Order](tx, "orders")
			if e != nil {
				return e
			}
			for _, x := range v {
				if match(x.Username, x.ID, x.Issue, x.Status) {
					rows = append(rows, x)
				}
			}
		case "requests":
			v, e := list[WalletRequest](tx, "requests")
			if e != nil {
				return e
			}
			for _, x := range v {
				if match(x.Username, x.ID, x.Status, x.Kind, x.PaymentMethod, x.PaymentNetwork, x.TransactionID) {
					rows = append(rows, x)
				}
			}
		case "commissions":
			v, e := list[Commission](tx, "commissions")
			if e != nil {
				return e
			}
			for _, x := range v {
				recipient, err := get[storedUser](tx, "users", x.UserID)
				if err != nil {
					return err
				}
				if match(x.FromUsername, x.UserID, x.OrderID, recipient.Username) {
					rows = append(rows, struct {
						Commission
						Username string `json:"username"`
					}{x, recipient.Username})
				}
			}
		case "ledger":
			v, e := list[Ledger](tx, "ledger")
			if e != nil {
				return e
			}
			for _, x := range v {
				owner, err := get[storedUser](tx, "users", x.UserID)
				if err != nil {
					return err
				}
				if match(x.UserID, x.Reference, x.Kind, owner.Username) {
					rows = append(rows, struct {
						Ledger
						Username string `json:"username"`
					}{ledgerForDisplay(x), owner.Username})
				}
			}
		case "audit":
			v, e := list[Audit](tx, "audit")
			if e != nil {
				return e
			}
			for _, x := range v {
				if match(x.Actor, x.Action, x.Target) {
					rows = append(rows, x)
				}
			}
		case "overview":
			users, e := list[storedUser](tx, "users")
			if e != nil {
				return e
			}
			var balance, frozen int64
			for _, u := range users {
				balance += u.Balance
				frozen += u.Frozen
			}
			orders, e := list[Order](tx, "orders")
			if e != nil {
				return e
			}
			pendingOrders := 0
			stake, bettingFrozen := int64(0), int64(0)
			for _, o := range orders {
				if o.Status == "pending" {
					pendingOrders++
					bettingFrozen += o.Cost
				}
				if o.Status == "settled" && o.Outcome != "push" {
					stake += o.Cost
				}
			}
			requests, e := list[WalletRequest](tx, "requests")
			if e != nil {
				return e
			}
			pendingRequests := 0
			for _, r := range requests {
				if r.Status == "pending" {
					pendingRequests++
				}
			}
			commissions, e := list[Commission](tx, "commissions")
			if e != nil {
				return e
			}
			ct := int64(0)
			for _, c := range commissions {
				ct += c.Amount
			}
			out = map[string]any{"users": len(users), "orders": len(orders), "pending_orders": pendingOrders, "pending_requests": pendingRequests, "balance_cents": balance, "frozen_cents": frozen, "betting_frozen_cents": bettingFrozen, "valid_stake_cents": stake, "commission_cents": ct}
			return nil
		default:
			return ErrNotFound
		}
		out["total"] = len(rows)
		size := 20
		if resource == "orders" {
			size = orderPageSize
		}
		current, start, end := pageBounds(len(rows), page, size)
		out["page"] = current
		out["page_size"] = size
		out["items"] = rows[start:end]
		return nil
	})
	return out, err
}
