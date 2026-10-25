package accounts

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	bolt "go.etcd.io/bbolt"
)

type MemberInviterState struct {
	Username           string `json:"username"`
	InviterID          string `json:"inviter_id"`
	InviterUsername    string `json:"inviter_username"`
	NewInviterID       string `json:"new_inviter_id"`
	NewInviterUsername string `json:"new_inviter_username"`
	Descendants        int    `json:"descendants"`
	Revision           string `json:"revision"`
}
type MemberInviterInput struct {
	InviterUsername string `json:"inviter_username"`
	Detach          bool   `json:"detach"`
	Revision        string `json:"revision"`
	Note            string `json:"note"`
	Key             string `json:"key"`
}

func memberInviterSnapshot(tx *bolt.Tx, actor, id string, change *MemberInviterInput) (MemberInviterState, error) {
	var state MemberInviterState
	u, err := managementMember(tx, actor, id)
	if err != nil {
		return state, err
	}
	users, err := list[storedUser](tx, "users")
	if err != nil {
		return state, err
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	byID := map[string]storedUser{}
	byName := map[string]string{}
	children := map[string][]string{}
	// The CAS token covers the whole topology and status, not financial data.
	revision := [][4]string{}
	for _, x := range users {
		byID[x.ID] = x
		byName[strings.ToLower(x.Username)] = x.ID
		children[x.InviterID] = append(children[x.InviterID], x.ID)
		revision = append(revision, [4]string{x.ID, x.InviterID, x.Role, x.Status})
	}
	state.Username = u.Username
	state.InviterID = u.InviterID
	state.InviterUsername = byID[u.InviterID].Username
	raw, _ := json.Marshal(revision)
	state.Revision = tokenKey(string(raw))
	queue := []string{id}
	seen := map[string]bool{id: true}
	for i := 0; i < len(queue); i++ {
		for _, child := range children[queue[i]] {
			if seen[child] {
				return state, errors.New("现有邀请关系存在循环，请先核对")
			}
			seen[child] = true
			queue = append(queue, child)
		}
	}
	state.Descendants = len(queue) - 1
	if change == nil {
		return state, nil
	}
	name := strings.TrimSpace(change.InviterUsername)
	if len(name) > 32 || change.Detach && name != "" || !change.Detach && name == "" {
		return state, errors.New("请输入完整的新上级账号，或明确选择设为无上级")
	}
	if !change.Detach {
		newID := byName[strings.ToLower(name)]
		parent, ok := byID[newID]
		if !ok {
			return state, errors.New("新上级账号不存在")
		}
		if parent.Status != "active" {
			return state, errors.New("新上级账号已停用")
		}
		if seen[newID] {
			return state, errors.New("不能将本人或自己的下级设为上级")
		}
		parents := map[string]bool{}
		for next := newID; next != ""; next = byID[next].InviterID {
			if parents[next] {
				return state, errors.New("新上级的邀请关系存在循环")
			}
			parents[next] = true
			if _, ok := byID[next]; !ok {
				return state, errors.New("新上级的邀请链记录缺失，请先核对")
			}
		}
		state.NewInviterID = parent.ID
		state.NewInviterUsername = parent.Username
	}
	if state.NewInviterID == u.InviterID {
		return state, errors.New("上级未发生变化")
	}
	return state, nil
}

func (s *Store) MemberInviter(actor, id string) (MemberInviterState, error) {
	var out MemberInviterState
	err := s.db.View(func(tx *bolt.Tx) error { var e error; out, e = memberInviterSnapshot(tx, actor, id, nil); return e })
	return out, err
}
func (s *Store) PreviewMemberInviter(actor, id string, in MemberInviterInput) (MemberInviterState, error) {
	var out MemberInviterState
	err := s.db.View(func(tx *bolt.Tx) error { var e error; out, e = memberInviterSnapshot(tx, actor, id, &in); return e })
	return out, err
}
func (s *Store) ChangeMemberInviter(actor, id string, in MemberInviterInput) (MemberInviterState, error) {
	var out MemberInviterState
	note, err := managementNote(in.Note)
	if err != nil {
		return out, err
	}
	if in.Revision == "" || !validKey(in.Key) {
		return out, errors.New("请先预览并确认新的上级关系")
	}
	body := struct {
		ID    string
		Input MemberInviterInput
	}{id, in}
	err = s.db.Update(func(tx *bolt.Tx) error {
		u, e := managementMember(tx, actor, id)
		if e != nil {
			return e
		}
		ref, e := idem(tx, "inviter:"+actor, in.Key, body)
		if e != nil {
			return e
		}
		if ref != "" {
			out, e = memberInviterSnapshot(tx, actor, id, nil)
			return e
		}
		before, e := memberInviterSnapshot(tx, actor, id, &in)
		if e != nil {
			return e
		}
		if before.Revision != in.Revision {
			return errors.New("邀请关系或账号状态已变更，请重新预览后确认")
		}
		u.InviterID = before.NewInviterID
		u.InviterUsername = ""
		if e = put(tx, "users", id, u); e != nil {
			return e
		}
		if e = remember(tx, "inviter:"+actor, in.Key, id, body); e != nil {
			return e
		}
		detail, _ := json.Marshal(map[string]any{"before_inviter_id": before.InviterID, "after_inviter_id": u.InviterID, "descendants": before.Descendants, "note": note})
		if e = audit(tx, actor, "user.inviter.change", id, string(detail)); e != nil {
			return e
		}
		out, e = memberInviterSnapshot(tx, actor, id, nil)
		return e
	})
	return out, err
}
