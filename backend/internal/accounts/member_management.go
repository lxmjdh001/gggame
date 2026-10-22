package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

type MemberPasswordState struct {
	Username string `json:"username"`
	Revision string `json:"revision"`
}
type MemberPasswordInput struct {
	Revision     string `json:"revision"`
	Password     string `json:"new_password"`
	Confirmation string `json:"password_confirmation"`
	Note         string `json:"note"`
}
type DescendantState struct {
	Username string `json:"username"`
	Total    int    `json:"total"`
	Active   int    `json:"active"`
	Disabled int    `json:"disabled"`
	Revision string `json:"revision"`
}
type DisableDescendantsInput struct {
	Revision string `json:"revision"`
	Note     string `json:"note"`
}

func managementMember(tx *bolt.Tx, actor, id string) (storedUser, error) {
	if err := admin(tx, actor); err != nil {
		return storedUser{}, err
	}
	u, err := get[storedUser](tx, "users", id)
	if err != nil {
		return u, err
	}
	if u.Role != "user" || u.ID == actor {
		return u, errors.New("此功能仅可操作会员账户")
	}
	return u, nil
}
func managementNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if n := utf8.RuneCountInString(note); n < 2 || n > 500 {
		return "", errors.New("操作原因需2–500字")
	}
	return note, nil
}
func memberPasswordRevision(u storedUser) string {
	return tokenKey(u.ID + ":" + u.Hash + ":" + u.PasswordSalt)
}
func (s *Store) MemberPasswordState(actor, id string) (MemberPasswordState, error) {
	var result MemberPasswordState
	err := s.db.View(func(tx *bolt.Tx) error {
		u, err := managementMember(tx, actor, id)
		if err != nil {
			return err
		}
		result = MemberPasswordState{u.Username, memberPasswordRevision(u)}
		return nil
	})
	return result, err
}
func (s *Store) ResetMemberPassword(actor, id string, in MemberPasswordInput) error {
	note, err := managementNote(in.Note)
	if err != nil {
		return err
	}
	if !passwordValid(in.Password) || !uppercasePasswordPattern.MatchString(in.Password) || !lowercasePasswordPattern.MatchString(in.Password) {
		return errors.New("新登录密码需10–128位，且同时包含大写和小写字母")
	}
	if in.Password != in.Confirmation {
		return errors.New("两次输入的新密码不一致")
	}
	if in.Revision == "" {
		return errors.New("请重新读取会员信息")
	}
	// Check authorization before expensive password hashing; check it again in the write transaction.
	err = s.db.View(func(tx *bolt.Tx) error {
		u, e := managementMember(tx, actor, id)
		if e != nil {
			return e
		}
		if memberPasswordRevision(u) != in.Revision {
			return errors.New("会员密码已变更，请重新读取后操作")
		}
		return nil
	})
	if err != nil {
		return err
	}
	salt := randomID()
	encoded, err := hash(in.Password, salt)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		u, e := managementMember(tx, actor, id)
		if e != nil {
			return e
		}
		if memberPasswordRevision(u) != in.Revision {
			return errors.New("会员密码已变更，请重新读取后操作")
		}
		u.Hash, u.PasswordSalt = encoded, salt
		if e = put(tx, "users", id, u); e != nil {
			return e
		}
		if e = revoke(tx, id); e != nil {
			return e
		}
		return audit(tx, actor, "user.password.reset", id, note)
	})
}

func descendantSnapshot(tx *bolt.Tx, actor, id string) (DescendantState, []storedUser, error) {
	root, err := managementMember(tx, actor, id)
	if err != nil {
		return DescendantState{}, nil, err
	}
	users, err := list[storedUser](tx, "users")
	if err != nil {
		return DescendantState{}, nil, err
	}
	children := map[string][]storedUser{}
	for _, u := range users {
		if u.InviterID != "" {
			children[u.InviterID] = append(children[u.InviterID], u)
		}
	}
	seen := map[string]bool{id: true}
	queue := []string{id}
	members := []storedUser{}
	// Walk invitation links, with no commission-depth or page-size limit.
	for i := 0; i < len(queue); i++ {
		for _, u := range children[queue[i]] {
			if seen[u.ID] {
				return DescendantState{}, nil, errors.New("邀请关系异常，无法批量停用")
			}
			seen[u.ID] = true
			queue = append(queue, u.ID)
			if u.Role == "user" {
				members = append(members, u)
			}
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	// The preview revision covers membership and account state, never balances or passwords.
	states := []string{root.ID, root.Role, root.Status}
	result := DescendantState{Username: root.Username, Total: len(members)}
	for _, u := range members {
		states = append(states, u.ID, u.InviterID, u.Role, u.Status)
		if u.Status == "active" {
			result.Active++
		} else if u.Status == "disabled" {
			result.Disabled++
		}
	}
	raw, _ := json.Marshal(states)
	result.Revision = tokenKey(string(raw))
	return result, members, nil
}
func (s *Store) Descendants(actor, id string) (DescendantState, error) {
	var state DescendantState
	err := s.db.View(func(tx *bolt.Tx) error { var e error; state, _, e = descendantSnapshot(tx, actor, id); return e })
	return state, err
}
func (s *Store) DisableDescendants(actor, id string, in DisableDescendantsInput) (DescendantState, error) {
	var state DescendantState
	note, err := managementNote(in.Note)
	if err != nil {
		return state, err
	}
	if in.Revision == "" {
		return state, errors.New("请先读取下级人数")
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		before, members, e := descendantSnapshot(tx, actor, id)
		if e != nil {
			return e
		}
		if before.Revision != in.Revision {
			return errors.New("下级人数或状态已变更，请重新读取后确认")
		}
		if before.Active == 0 {
			return errors.New("没有需要停用的下级会员")
		}
		ids := make(map[string]bool, len(members))
		for _, u := range members {
			ids[u.ID] = true
			if u.Status != "active" {
				continue
			}
			u.Status = "disabled"
			if e = put(tx, "users", u.ID, u); e != nil {
				return e
			}
			if e = audit(tx, actor, "user.status", u.ID, "disabled；批量停用下级："+id+"；"+note); e != nil {
				return e
			}
		}
		c := tx.Bucket([]byte("logins")).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var login Login
			if e = json.Unmarshal(v, &login); e != nil {
				return e
			}
			if ids[login.UserID] {
				if e = c.Delete(); e != nil {
					return e
				}
			}
		}
		for _, u := range members {
			if e = invalidateLoginSecurity(tx, u.ID); e != nil {
				return e
			}
		}
		if e = audit(tx, actor, "user.descendants.disable", id, fmt.Sprintf("停用%d名下级；共%d名；原已停用%d名；原因：%s", before.Active, before.Total, before.Disabled, note)); e != nil {
			return e
		}
		state = before
		return nil
	})
	return state, err
}
