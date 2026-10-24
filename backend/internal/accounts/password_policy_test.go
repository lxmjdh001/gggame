package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestPasswordLengthEightToSixteenBoundaries(t *testing.T) {
	for _, n := range []int{0, 7, 8, 9, 16, 17, 128} {
		if passwordValid(strings.Repeat("a", n)) != (n >= 8 && n <= 16) {
			t.Fatal("wrong password boundary", n)
		}
	}
}

func TestPasswordPolicySecureRegistrationAndAdminReset(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	for _, n := range []int{8, 16} {
		password := "Ab" + strings.Repeat("1", n-2)
		in := secureRegistrationInput(t, s, "length"+strings.Repeat("n", n))
		in.Password = password
		u, token, err := s.RegisterSecure(in)
		must(t, err)
		if token == "" {
			t.Fatal("valid boundary registration did not sign in")
		}
		_, _, err = s.Login(u.Username, password)
		must(t, err)
		for _, rejected := range []string{"Ab12345", "Ab" + strings.Repeat("1", 15)} {
			state, e := s.MemberPasswordState("admin", u.ID)
			must(t, e)
			before := markSixCombinedStoredState(t, s)
			if s.ResetMemberPassword("admin", u.ID, MemberPasswordInput{Revision: state.Revision, Password: rejected, Confirmation: rejected, Note: "长度边界验证"}) == nil {
				t.Fatal("invalid reset accepted", len(rejected))
			}
			if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
				t.Fatal("rejected reset wrote data")
			}
		}
		state, err := s.MemberPasswordState("admin", u.ID)
		must(t, err)
		newPassword := "Cd" + strings.Repeat("2", n-2)
		must(t, s.ResetMemberPassword("admin", u.ID, MemberPasswordInput{Revision: state.Revision, Password: newPassword, Confirmation: newPassword, Note: "长度边界验证"}))
		if _, e := s.Authenticate(token); e == nil {
			t.Fatal("reset did not revoke original session")
		}
		_, _, err = s.Login(u.Username, newPassword)
		must(t, err)
	}
}

func TestPasswordPolicyLegacyLoginAndOwnPasswordChange(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	legacy := "Legacy-admin-" + strings.Repeat("1", 27)
	salt := randomID()
	encoded, err := hash(legacy, salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "admin")
		if e != nil {
			return e
		}
		u.Hash, u.PasswordSalt = encoded, salt
		return put(tx, "users", u.ID, u)
	}))
	financeBefore := managementFinancialSnapshot(t, s)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	created, err := s.Bootstrap("admin", "Newpass8")
	must(t, err)
	if created {
		t.Fatal("existing administrator reset")
	}
	stored := managementStored(t, s, "admin")
	if stored.Hash != encoded || stored.PasswordSalt != salt {
		t.Fatal("legacy credentials changed on startup")
	}
	_, _, err = s.Login("admin", legacy)
	must(t, err)
	// The actual HTTP login route must also accept the previous long password.
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": legacy})
	r := httptest.NewRequest("POST", "http://localhost:5173/api/v1/auth/login", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.Protect(mux).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("legacy HTTP login rejected", w.Code)
	}
	current := legacy
	for _, next := range []string{"Aa123456", "Ab12345678901234"} {
		for _, invalid := range []string{"Ab12345", "Ab123456789012345"} {
			before := markSixCombinedStoredState(t, s)
			if s.ChangePassword("admin", current, invalid) == nil {
				t.Fatal("invalid own password accepted", len(invalid))
			}
			if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
				t.Fatal("rejected change mutated account")
			}
		}
		_, token, e := s.Login("admin", current)
		must(t, e)
		must(t, s.ChangePassword("admin", current, next))
		if _, e = s.Authenticate(token); e == nil {
			t.Fatal("password change did not revoke session")
		}
		_, _, err = s.Login("admin", next)
		must(t, err)
		current = next
	}
	if financeBefore != managementFinancialSnapshot(t, s) {
		t.Fatal("password policy touched financial records")
	}
}
