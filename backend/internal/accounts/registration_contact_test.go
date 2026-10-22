package accounts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestRegistrationContactValidation(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	for _, tc := range []struct{ phone, name string }{
		{"", ""}, {" \t ", ""}, {"123", ""}, {"1380013800a", ""},
		{"138001380000", ""}, {"+0123456789", ""},
		{"13800138000", strings.Repeat("名", 41)}, {"13800138000", "张\n三"},
	} {
		if _, token, err := s.Register("invalidcontact", testPassword, "", tc.phone, tc.name); err == nil || token != "" {
			t.Fatal("invalid contact accepted by store")
		}
		body, err := json.Marshal(secureRegistrationFields(t, s, map[string]string{"username": "invalidcontact", "password": testPassword, "phone": tc.phone, "name": tc.name}))
		must(t, err)
		req := httptest.NewRequest("POST", "http://localhost/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, req)
		expectedError := "手机号"
		if tc.name != "" {
			expectedError = "姓名"
		}
		if w.Code != 400 || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), expectedError) {
			t.Fatalf("invalid contact not rejected for the expected reason over HTTP: %d %s", w.Code, w.Body.String())
		}
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("usernames")).Get([]byte("invalidcontact")) != nil || tx.Bucket([]byte("logins")).Stats().KeyN != 0 || tx.Bucket([]byte("ledger")).Stats().KeyN != 0 {
			t.Fatal("failed registration wrote user, login or bonus")
		}
		return nil
	}))
}

func TestRegistrationContactPersistenceAndHTTP(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	cfg, err := s.Settings()
	must(t, err)
	cfg.RegistrationBonusCents = 12300
	must(t, s.SaveSettings("admin", cfg))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	call := func(fields map[string]string) User {
		t.Helper()
		body, err := json.Marshal(secureRegistrationFields(t, s, fields))
		must(t, err)
		req := httptest.NewRequest("POST", "http://localhost/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, req)
		session := loginSecurityCookie(w.Result().Cookies(), "starlight_session")
		if w.Code != 200 || session == nil || session.Value == "" || session.MaxAge <= 0 {
			t.Fatalf("registration failed: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			Data User `json:"data"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &response))
		return response.Data
	}
	parent := call(map[string]string{"username": "contactparent", "password": testPassword, "phone": " +85261234567 ", "name": "李四"})
	if parent.Phone != "+85261234567" || parent.Name != "李四" {
		t.Fatal("international phone or required name not saved")
	}
	child := call(map[string]string{"username": "contactchild", "password": testPassword, "phone": " 13800138000 ", "name": " 张三 ", "invite_code": parent.InviteCode})
	if child.Phone != "13800138000" || child.Name != "张三" || child.Balance != 12300 || child.InviterID != parent.ID {
		t.Fatal("contact, bonus or invite not saved")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	u, _, err := s.Login(child.Username, testPassword)
	must(t, err)
	if u.Phone != child.Phone || u.Name != child.Name {
		t.Fatal("contact not persisted after restart")
	}
	account, err := s.Account(child.ID)
	must(t, err)
	if account["user"].(User).Phone != child.Phone || len(account["ledger"].([]Ledger)) != 1 {
		t.Fatal("account contact or bonus ledger incorrect")
	}
	for _, query := range []string{child.Phone, child.Name} {
		data, err := s.AdminData("admin", "users", query, 1)
		must(t, err)
		if len(data["items"].([]any)) != 1 {
			t.Fatal("admin cannot search contact")
		}
	}
	team, err := s.Account(parent.ID)
	must(t, err)
	encoded, err := json.Marshal(team["members"])
	must(t, err)
	if bytes.Contains(encoded, []byte(child.Phone)) || bytes.Contains(encoded, []byte(child.Name)) {
		t.Fatal("private contact leaked to inviter")
	}
}
