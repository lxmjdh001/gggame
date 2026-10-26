package accounts

import (
	"encoding/json"
	"errors"
	"strings"

	bolt "go.etcd.io/bbolt"
)

type MemberDeletionState struct {
	Username            string   `json:"username"`
	Revision            string   `json:"revision"`
	Deleted             bool     `json:"deleted"`
	Balance             int64    `json:"balance_cents"`
	Frozen              int64    `json:"frozen_cents"`
	BettingFrozen       int64    `json:"betting_frozen_cents"`
	AvailableCommission int64    `json:"available_commission_cents"`
	PendingRequests     int      `json:"pending_requests"`
	PendingOrders       int      `json:"pending_orders"`
	DirectCount         int      `json:"direct_count"`
	CanDelete           bool     `json:"can_delete"`
	Blockers            []string `json:"blockers"`
}

type MemberDeletionInput struct {
	Revision string `json:"revision"`
	Username string `json:"confirm_username"`
	Note     string `json:"note"`
	Key      string `json:"key"`
}

type MemberDeletionResult struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Deleted  bool   `json:"deleted"`
	Status   string `json:"status"`
}

func deletionMember(tx *bolt.Tx, actor, id string) (storedUser, error) {
	if err := admin(tx, actor); err != nil {
		return storedUser{}, err
	}
	u, err := get[storedUser](tx, "users", id)
	if err != nil {
		return u, err
	}
	if u.Role != "user" || u.ID == actor {
		return u, errors.New("只能删除或恢复普通会员，不能操作管理员或本人")
	}
	return u, nil
}

// A deletion changes account availability, never financial or invitation history.
// The revision is checked again inside the write transaction, after current
// balances and pending work have been inspected, so no concurrent credit is lost.
func deletionSnapshot(tx *bolt.Tx, actor, id string) (MemberDeletionState, storedUser, error) {
	out := MemberDeletionState{Blockers: []string{}}
	u, err := deletionMember(tx, actor, id)
	if err != nil {
		return out, u, err
	}
	out.Username = u.Username
	out.Deleted = u.Deleted
	out.Balance = u.Balance
	out.Frozen = u.Frozen
	orders, err := list[Order](tx, "orders")
	if err != nil {
		return out, u, err
	}
	for _, o := range orders {
		if o.UserID == id && o.Status == "pending" {
			out.PendingOrders++
			out.BettingFrozen += o.Cost
		}
	}
	requests, err := list[WalletRequest](tx, "requests")
	if err != nil {
		return out, u, err
	}
	for _, r := range requests {
		if r.UserID == id && r.Status == "pending" {
			out.PendingRequests++
		}
	}
	commissions, err := list[Commission](tx, "commissions")
	if err != nil {
		return out, u, err
	}
	for _, c := range commissions {
		if c.UserID == id && c.Status == "available" && c.Amount > 0 {
			out.AvailableCommission += c.Amount
		}
	}
	users, err := list[storedUser](tx, "users")
	if err != nil {
		return out, u, err
	}
	for _, child := range users {
		if child.InviterID == id {
			out.DirectCount++
		}
	}
	if u.Deleted {
		out.Blockers = append(out.Blockers, "该会员已删除")
	}
	if u.Balance != 0 {
		out.Blockers = append(out.Blockers, "账户余额未清零，请先处理余额")
	}
	if u.Frozen != 0 {
		out.Blockers = append(out.Blockers, "存在提现冻结款项，请先处理")
	}
	if out.PendingOrders > 0 {
		out.Blockers = append(out.Blockers, "存在未结算投注，请先完成结算或撤销")
	}
	if out.PendingRequests > 0 {
		out.Blockers = append(out.Blockers, "存在待处理资金申请，请先审核")
	}
	if out.AvailableCommission > 0 {
		out.Blockers = append(out.Blockers, "存在未领取佣金，请先处理")
	}
	out.CanDelete = len(out.Blockers) == 0
	// DeletionRevision prevents an old preview from becoming valid again after
	// a delete/restore cycle, even when the visible values happen to match.
	raw, _ := json.Marshal(struct {
		State                                         MemberDeletionState
		ID, Role, Status, InviterID, DeletionRevision string
	}{out, u.ID, u.Role, u.Status, u.InviterID, u.DeletionRevision})
	out.Revision = tokenKey(string(raw))
	return out, u, nil
}

func (s *Store) MemberDeletionState(actor, id string) (MemberDeletionState, error) {
	var out MemberDeletionState
	err := s.db.View(func(tx *bolt.Tx) error { var e error; out, _, e = deletionSnapshot(tx, actor, id); return e })
	return out, err
}

func (s *Store) DeleteMember(actor, id string, in MemberDeletionInput) (MemberDeletionResult, error) {
	return s.changeMemberDeletion(actor, id, in, true)
}
func (s *Store) RestoreMember(actor, id string, in MemberDeletionInput) (MemberDeletionResult, error) {
	return s.changeMemberDeletion(actor, id, in, false)
}

func (s *Store) changeMemberDeletion(actor, id string, in MemberDeletionInput, deleted bool) (MemberDeletionResult, error) {
	var out MemberDeletionResult
	note, err := managementNote(in.Note)
	if err != nil {
		return out, err
	}
	if in.Revision == "" || !validKey(in.Key) {
		return out, errors.New("请先读取会员信息并确认操作")
	}
	body := struct {
		ID    string
		Input MemberDeletionInput
	}{id, in}
	action := "user.delete"
	if !deleted {
		action = "user.restore"
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		u, e := deletionMember(tx, actor, id)
		if e != nil {
			return e
		}
		if in.Username != u.Username {
			return errors.New("确认账号不一致，请输入完整会员账号")
		}
		ref, e := idem(tx, action+":"+actor, in.Key, body)
		if e != nil {
			return e
		}
		out = MemberDeletionResult{ID: u.ID, Username: u.Username, Deleted: deleted, Status: "disabled"}
		if ref != "" {
			return nil
		}
		state, current, e := deletionSnapshot(tx, actor, id)
		if e != nil {
			return e
		}
		if state.Revision != in.Revision {
			return errors.New("会员状态、余额或待处理记录已变化，请重新读取后确认")
		}
		if deleted && !state.CanDelete {
			return errors.New(strings.Join(state.Blockers, "；"))
		}
		if !deleted && !current.Deleted {
			return errors.New("该会员未删除，无需恢复")
		}
		beforeStatus := current.Status
		current.Deleted = deleted
		current.Status = "disabled"
		current.DeletionRevision = randomID()
		if e = put(tx, "users", id, current); e != nil {
			return e
		}
		if e = revoke(tx, id); e != nil {
			return e
		}
		if e = remember(tx, action+":"+actor, in.Key, id, body); e != nil {
			return e
		}
		detail, _ := json.Marshal(map[string]any{"username": u.Username, "before_status": beforeStatus, "after_status": "disabled", "deleted": deleted, "direct_count": state.DirectCount, "note": note})
		return audit(tx, actor, action, id, string(detail))
	})
	return out, err
}
