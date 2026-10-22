package accounts

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	bolt "go.etcd.io/bbolt"
)

var shortInvitePattern = regexp.MustCompile(`^[1-9][0-9]{6}$`)
var shortInviteMigration = []byte("invite_code_seven_digits_v1")

const inviteCodeSpace = 9000000

func newInviteCode(tx *bolt.Tx) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(inviteCodeSpace))
	if err != nil {
		return "", err
	}
	return availableInviteCode(tx, int(n.Int64()))
}

// Lookup and reservation share the user creation transaction. Preserve all aliases.
func availableInviteCode(tx *bolt.Tx, start int) (string, error) {
	for i := 0; i < inviteCodeSpace; i++ {
		code := fmt.Sprintf("%d", 1000000+(start+i)%inviteCodeSpace)
		if tx.Bucket([]byte("invites")).Get([]byte(code)) == nil {
			return code, nil
		}
	}
	return "", errors.New("邀请码暂不可分配，请联系管理员")
}

func migrateInviteCodes(tx *bolt.Tx) error {
	meta := tx.Bucket([]byte("settings"))
	if meta.Get(shortInviteMigration) != nil {
		return nil
	}
	users, err := list[storedUser](tx, "users")
	if err != nil {
		return err
	}
	index := tx.Bucket([]byte("invites"))
	// Reserve existing codes before allocating any new code.
	for _, u := range users {
		old := strings.ToUpper(u.InviteCode)
		if old == "" {
			continue
		}
		if owner := index.Get([]byte(old)); owner != nil && string(owner) != u.ID {
			return errors.New("邀请码归属不一致，请核对账户记录")
		}
		if err := index.Put([]byte(old), []byte(u.ID)); err != nil {
			return err
		}
	}
	changed := 0
	for _, u := range users {
		if shortInvitePattern.MatchString(u.InviteCode) {
			continue
		}
		code, err := newInviteCode(tx)
		if err != nil {
			return err
		}
		u.InviteCode = code
		if err := put(tx, "users", u.ID, u); err != nil {
			return err
		}
		if err := index.Put([]byte(code), []byte(u.ID)); err != nil {
			return err
		}
		changed++
	}
	if changed > 0 {
		if err := audit(tx, "system", "invite_code.migrate", "users", fmt.Sprintf("%d个账户更新为7位数字邀请码，保留旧码兼容", changed)); err != nil {
			return err
		}
	}
	return meta.Put(shortInviteMigration, []byte("complete"))
}
