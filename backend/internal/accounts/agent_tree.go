package accounts

import (
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
	"sort"
	"starlight/gameapi/pkg/model"
	"strings"
	"time"
)

type AgentNode struct {
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	Name            string    `json:"name,omitempty"`
	Role            string    `json:"role"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	InviterID       string    `json:"inviter_id,omitempty"`
	InviterUsername string    `json:"inviter_username,omitempty"`
	MissingParent   bool      `json:"missing_parent"`
	Deposit         int64     `json:"deposit_cents"`
	DirectCount     int       `json:"direct_count"`
	TeamCount       int       `json:"team_count"`
	TeamDeposit     int64     `json:"team_deposit_cents"`
	TodayStake      int64     `json:"today_stake_cents"`
	TeamTodayStake  int64     `json:"team_today_stake_cents"`
}
type AgentPage struct {
	Items      []AgentNode `json:"items"`
	Parent     *AgentNode  `json:"parent,omitempty"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	Day        string      `json:"day"`
	TodayStake int64       `json:"today_stake_cents"`
}

// Read-only snapshot. Descendant totals use leaf-to-root aggregation, not a
// depth-limited commission traversal; cycles fail explicitly rather than hang.
func (s *Store) AgentTree(actor, username, parentID string, page int) (AgentPage, error) {
	return s.agentTreeAt(actor, username, parentID, page, time.Now())
}
func (s *Store) agentTreeAt(actor, username, parentID string, page int, now time.Time) (AgentPage, error) {
	return s.agentTreeSnapshot(actor, username, parentID, page, now, false)
}

func (s *Store) agentTreeSnapshot(actor, username, parentID string, page int, now time.Time, full bool) (AgentPage, error) {
	out := AgentPage{Items: []AgentNode{}, PageSize: 20}
	local := now.In(model.Beijing)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, model.Beijing)
	end := start.AddDate(0, 0, 1)
	out.Day = start.Format("2006-01-02")
	username = strings.TrimSpace(username)
	parentID = strings.TrimSpace(parentID)
	if page < 1 || page > 1000000 || len(username) > 32 || len(parentID) > 128 || (username != "" && parentID != "") {
		return out, errors.New("代理查询参数不正确")
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		nodes := map[string]*AgentNode{}
		byName := map[string]string{}
		if err := tx.Bucket([]byte("users")).ForEach(func(_, raw []byte) error {
			var u storedUser
			if err := json.Unmarshal(raw, &u); err != nil {
				return err
			}
			nodes[u.ID] = &AgentNode{ID: u.ID, Username: u.Username, Name: u.Name, Role: u.Role, Status: u.Status, CreatedAt: u.CreatedAt, InviterID: u.InviterID}
			if u.Deleted {
				nodes[u.ID].Status = "deleted"
			}
			byName[strings.ToLower(u.Username)] = u.ID
			return nil
		}); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("ledger")).ForEach(func(_, raw []byte) error {
			var l Ledger
			if err := json.Unmarshal(raw, &l); err != nil {
				return err
			}
			if n := nodes[l.UserID]; n != nil && l.Amount > 0 {
				switch ledgerForDisplay(l).Kind {
				case "deposit_approved", "admin_deposit":
					n.Deposit += l.Amount
				}
			}
			return nil
		}); err != nil {
			return err
		}
		children := map[string][]string{}
		if err := tx.Bucket([]byte("orders")).ForEach(func(_, raw []byte) error {
			var o Order
			if e := json.Unmarshal(raw, &o); e != nil {
				return e
			}
			if n := nodes[o.UserID]; n != nil && (o.Status == "pending" || o.Status == "settled") && !o.CreatedAt.Before(start) && o.CreatedAt.Before(end) {
				n.TodayStake += o.Cost
			}
			return nil
		}); err != nil {
			return err
		}
		roots := []string{}
		pending := map[string]int{}
		for id, n := range nodes {
			if p := nodes[n.InviterID]; p != nil {
				n.InviterUsername = p.Username
				children[p.ID] = append(children[p.ID], id)
			} else {
				n.MissingParent = n.InviterID != ""
				roots = append(roots, id)
			}
		}
		queue := []string{}
		for id, n := range nodes {
			n.DirectCount = len(children[id])
			pending[id] = n.DirectCount
			if pending[id] == 0 {
				queue = append(queue, id)
			}
		}
		for i := 0; i < len(queue); i++ {
			n := nodes[queue[i]]
			if p := nodes[n.InviterID]; p != nil {
				p.TeamCount += n.TeamCount + 1
				p.TeamDeposit += n.TeamDeposit + n.Deposit
				p.TeamTodayStake += n.TeamTodayStake + n.TodayStake
				pending[p.ID]--
				if pending[p.ID] == 0 {
					queue = append(queue, p.ID)
				}
			}
		}
		if len(queue) != len(nodes) {
			return errors.New("代理邀请关系存在循环，请联系管理员核对")
		}
		ids := roots
		if username != "" {
			id, ok := byName[strings.ToLower(username)]
			if !ok {
				return ErrNotFound
			}
			ids = []string{id}
		}
		if parentID != "" {
			p := nodes[parentID]
			if p == nil {
				return ErrNotFound
			}
			copy := *p
			out.Parent = &copy
			ids = children[parentID]
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := nodes[ids[i]], nodes[ids[j]]
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(b.CreatedAt)
		})
		out.Total = len(ids)
		for _, id := range ids {
			out.TodayStake += nodes[id].TodayStake + nodes[id].TeamTodayStake
		}
		if full {
			// Iterative breadth-first traversal keeps every parent before its children.
			queue := append([]string(nil), ids...)
			for i := 0; i < len(queue); i++ {
				id := queue[i]
				out.Items = append(out.Items, *nodes[id])
				next := children[id]
				sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
				queue = append(queue, next...)
			}
			out.Total = len(out.Items)
			return nil
		}
		current, start, end := pageBounds(out.Total, page, out.PageSize)
		out.Page = current
		for _, id := range ids[start:end] {
			out.Items = append(out.Items, *nodes[id])
		}
		return nil
	})
	return out, err
}
