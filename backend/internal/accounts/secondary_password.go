package accounts

import (
	"crypto/subtle"
	"errors"
	"time"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

var ErrSecondaryRequired = errors.New("请先验证二级密码后再进入游戏")
var errSecondaryLimited = errors.New("二级密码验证次数过多，请15分钟后再试")
var errSecondaryChanged = errors.New("二级密码设置已变更，请刷新后重试")

type SecondaryPasswordStatus struct {
	IsSet                bool       `json:"is_set"`
	Enabled              bool       `json:"enabled"`
	VerificationRequired bool       `json:"verification_required"`
	VerifiedUntil        *time.Time `json:"verified_until,omitempty"`
	ServerTime           time.Time  `json:"server_time"`
}

type SecondaryPasswordInput struct {
	LoginPassword   string `json:"login_password"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	Confirmation    string `json:"confirm_password"`
}

func secondaryPasswordValid(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= 1 && length <= 8
}

// Existing 9–12 rune secrets remain valid without changing their stored hashes.
func secondarySecretValid(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= 1 && length <= 12
}

func secondaryLogin(tx *bolt.Tx, token string, now time.Time) (Login, storedUser, error) {
	if token == "" || len(token) > 128 {
		return Login{}, storedUser{}, ErrUnauthorized
	}
	l, err := get[Login](tx, "logins", tokenKey(token))
	if err != nil || !now.Before(l.Expires) {
		return Login{}, storedUser{}, ErrUnauthorized
	}
	u, err := active(tx, l.UserID)
	return l, u, err
}

func secondaryStatus(u storedUser, l Login, now time.Time) SecondaryPasswordStatus {
	status := SecondaryPasswordStatus{IsSet: u.SecondaryPasswordSet, Enabled: u.SecondaryPasswordEnabled, ServerTime: now}
	if !status.Enabled {
		return status
	}
	status.VerificationRequired = true
	if !status.IsSet || u.SecondaryHash == "" || u.SecondarySalt == "" || u.SecondaryRevision == "" || l.SecondaryRevision != u.SecondaryRevision || l.SecondaryVerifiedAt.IsZero() || now.Before(l.SecondaryVerifiedAt) {
		return status
	}
	verified := l.SecondaryVerifiedAt.In(model.Beijing)
	nextDay := time.Date(verified.Year(), verified.Month(), verified.Day()+1, 0, 0, 0, 0, model.Beijing)
	until := l.SecondaryVerifiedAt.Add(time.Hour)
	if nextDay.Before(until) {
		until = nextDay
	}
	if l.Expires.Before(until) {
		until = l.Expires
	}
	if !now.Before(until) {
		return status
	}
	status.VerificationRequired = false
	status.VerifiedUntil = &until
	return status
}

func (s *Store) secondaryState(token string, now time.Time) (Login, storedUser, error) {
	var l Login
	var u storedUser
	err := s.db.View(func(tx *bolt.Tx) error { var err error; l, u, err = secondaryLogin(tx, token, now); return err })
	return l, u, err
}

func (s *Store) SecondaryPasswordStatus(token string) (SecondaryPasswordStatus, error) {
	now := time.Now()
	l, u, err := s.secondaryState(token, now)
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	return secondaryStatus(u, l, now), nil
}

func (s *Store) secondaryAttempt(id string) error {
	err := s.reserveSecurityAttempt("secondary_password", id)
	if errors.Is(err, errSecurityLimited) {
		return errSecondaryLimited
	}
	return err
}

func sameSecondary(a, b storedUser) bool {
	return a.Hash == b.Hash && a.PasswordSalt == b.PasswordSalt && a.SecondaryHash == b.SecondaryHash && a.SecondarySalt == b.SecondarySalt && a.SecondaryRevision == b.SecondaryRevision && a.SecondaryPasswordSet == b.SecondaryPasswordSet && a.SecondaryPasswordEnabled == b.SecondaryPasswordEnabled
}

func clearSecondaryAttempts(tx *bolt.Tx, id string) error {
	if b := tx.Bucket([]byte("auth_security_attempts")); b != nil {
		return b.Delete([]byte("secondary_password:" + id))
	}
	return nil
}

func (s *Store) checkSecondarySecret(u storedUser, password string) error {
	if err := s.secondaryAttempt(u.ID); err != nil {
		return err
	}
	if !secondarySecretValid(password) {
		return errors.New("二级密码不正确")
	}
	candidate, err := hash(password, u.SecondarySalt)
	if err != nil {
		return err
	}
	if !u.SecondaryPasswordSet || u.SecondaryHash == "" || u.SecondarySalt == "" || subtle.ConstantTimeCompare([]byte(candidate), []byte(u.SecondaryHash)) != 1 {
		return errors.New("二级密码不正确")
	}
	return nil
}

func (s *Store) SetSecondaryPassword(token string, in SecondaryPasswordInput) (SecondaryPasswordStatus, error) {
	if !secondaryPasswordValid(in.NewPassword) {
		return SecondaryPasswordStatus{}, errors.New("二级密码需1–8个字符，不要求大小写组合")
	}
	if in.NewPassword != in.Confirmation {
		return SecondaryPasswordStatus{}, errors.New("两次二级密码不一致")
	}
	_, before, err := s.secondaryState(token, time.Now())
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	if before.SecondaryPasswordSet {
		err = s.checkSecondarySecret(before, in.CurrentPassword)
	} else {
		if err = s.secondaryAttempt(before.ID); err != nil {
			return SecondaryPasswordStatus{}, err
		}
		if in.LoginPassword == "" || len(in.LoginPassword) > 128 {
			return SecondaryPasswordStatus{}, errors.New("登录密码不正确")
		}
		var candidate string
		candidate, err = hash(in.LoginPassword, before.PasswordSalt)
		if err == nil && subtle.ConstantTimeCompare([]byte(candidate), []byte(before.Hash)) != 1 {
			err = errors.New("登录密码不正确")
		}
	}
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	salt := randomID()
	encoded, err := hash(in.NewPassword, salt)
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		_, u, err := secondaryLogin(tx, token, time.Now())
		if err != nil {
			return err
		}
		if u.ID != before.ID || !sameSecondary(u, before) {
			return errSecondaryChanged
		}
		first := !u.SecondaryPasswordSet
		u.SecondaryHash, u.SecondarySalt, u.SecondaryRevision = encoded, salt, randomID()
		u.SecondaryPasswordSet = true
		if first {
			u.SecondaryPasswordEnabled = true
		}
		if err = put(tx, "users", u.ID, u); err != nil {
			return err
		}
		if err = clearSecondaryAttempts(tx, u.ID); err != nil {
			return err
		}
		return audit(tx, u.ID, "secondary_password.set", u.ID, "credentials updated; existing game grants invalidated")
	})
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	return s.SecondaryPasswordStatus(token)
}

func (s *Store) ToggleSecondaryPassword(token, password string, enabled bool) (SecondaryPasswordStatus, error) {
	_, before, err := s.secondaryState(token, time.Now())
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	if !before.SecondaryPasswordSet {
		return SecondaryPasswordStatus{}, errors.New("请先设置二级密码")
	}
	if err = s.checkSecondarySecret(before, password); err != nil {
		return SecondaryPasswordStatus{}, err
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		_, u, err := secondaryLogin(tx, token, time.Now())
		if err != nil {
			return err
		}
		if u.ID != before.ID || !sameSecondary(u, before) {
			return errSecondaryChanged
		}
		u.SecondaryPasswordEnabled, u.SecondaryRevision = enabled, randomID()
		if err = put(tx, "users", u.ID, u); err != nil {
			return err
		}
		if err = clearSecondaryAttempts(tx, u.ID); err != nil {
			return err
		}
		detail := "enabled=false"
		if enabled {
			detail = "enabled=true"
		}
		return audit(tx, u.ID, "secondary_password.toggle", u.ID, detail)
	})
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	return s.SecondaryPasswordStatus(token)
}

func (s *Store) VerifySecondaryPassword(token, password string) (SecondaryPasswordStatus, error) {
	return s.verifySecondaryPasswordAt(token, password, time.Time{})
}

func (s *Store) verifySecondaryPasswordAt(token, password string, now time.Time) (SecondaryPasswordStatus, error) {
	lookupTime := now
	if lookupTime.IsZero() {
		lookupTime = time.Now()
	}
	l, before, err := s.secondaryState(token, lookupTime)
	if err != nil {
		return SecondaryPasswordStatus{}, err
	}
	if !before.SecondaryPasswordEnabled {
		return secondaryStatus(before, l, lookupTime), nil
	}
	if err = s.checkSecondarySecret(before, password); err != nil {
		return SecondaryPasswordStatus{}, err
	}
	var out SecondaryPasswordStatus
	err = s.db.Update(func(tx *bolt.Tx) error {
		if now.IsZero() {
			now = time.Now()
		}
		l, u, err := secondaryLogin(tx, token, now)
		if err != nil {
			return err
		}
		if u.ID != before.ID || !sameSecondary(u, before) {
			return errSecondaryChanged
		}
		l.SecondaryVerifiedAt, l.SecondaryRevision = now, u.SecondaryRevision
		if err = put(tx, "logins", tokenKey(token), l); err != nil {
			return err
		}
		if err = clearSecondaryAttempts(tx, u.ID); err != nil {
			return err
		}
		out = secondaryStatus(u, l, now)
		return nil
	})
	return out, err
}

// Called inside the same write transaction as every order/idempotency check.
// The grant belongs to a login, not a user or client-supplied timestamp.
func requireGameAccess(tx *bolt.Tx, id, token string, now time.Time) error {
	u, err := active(tx, id)
	if err != nil {
		return err
	}
	if !u.SecondaryPasswordEnabled {
		return nil
	}
	l, current, err := secondaryLogin(tx, token, now)
	if err != nil || current.ID != id || secondaryStatus(current, l, now).VerificationRequired {
		return ErrSecondaryRequired
	}
	return nil
}
