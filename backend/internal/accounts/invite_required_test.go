package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestInviteRequiredRegistration(t *testing.T) {
	s, admin := setup(t)
	cfg, e := s.Settings()
	must(t, e)
	if cfg.InviteRequired {
		t.Fatal("invite requirement should default off")
	}
	parent := register(t, s, "invite_parent", "")
	cfg.InviteRequired = true
	if e = s.SaveSettings(parent.ID, cfg); e != ErrForbidden {
		t.Fatal("non-admin changed registration policy")
	}
	must(t, s.SaveSettings(admin.ID, cfg))
	for _, code := range []string{"", " \t\n", "NONEXISTENT"} {
		if _, token, e := s.Register("invite_new", testPassword, code); e == nil || token != "" {
			t.Fatal("invalid registration accepted", code)
		}
	}
	child := register(t, s, "invite_new", "  "+strings.ToLower(parent.InviteCode)+"  ")
	if child.InviterID != parent.ID {
		t.Fatal("inviter not bound")
	}
	must(t, s.SetStatus(admin.ID, parent.ID, "disabled"))
	if _, _, e = s.Register("inactive_invite", testPassword, parent.InviteCode); e == nil {
		t.Fatal("disabled inviter accepted")
	}
	cfg.Registration = false
	must(t, s.SaveSettings(admin.ID, cfg))
	if _, _, e = s.Register("closed_user", testPassword, admin.InviteCode); e == nil {
		t.Fatal("closed registration accepted")
	}
	_, _, e = s.Login(child.Username, testPassword)
	must(t, e)
	cfg.Registration = true
	cfg.InviteRequired = false
	must(t, s.SaveSettings(admin.ID, cfg))
	independent := register(t, s, "independent_user", "")
	if independent.InviterID != "" {
		t.Fatal("unexpected inviter")
	}
	if _, _, e = s.Register("bad_optional", testPassword, "NONEXISTENT"); e == nil {
		t.Fatal("invalid optional code accepted")
	}
}

func TestInviteRequiredPersistenceAndHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	_, e = s.Bootstrap("admin", testPassword)
	must(t, e)
	admin, token, e := s.Login("admin", testPassword)
	must(t, e)
	cfg, e := s.Settings()
	must(t, e)
	cfg.InviteRequired = true
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	body, e := json.Marshal(cfg)
	must(t, e)
	req := httptest.NewRequest("PUT", "http://localhost/api/v1/admin/settings", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	defer s.Close()
	h.Store = s
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/api/v1/site", nil))
	var site struct {
		Data Settings `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &site))
	if w.Code != 200 || !site.Data.InviteRequired {
		t.Fatal("requirement not persisted/published")
	}
	for _, code := range []string{"", "  ", admin.InviteCode} {
		body, e = json.Marshal(map[string]string{"username": "http_invite_new", "password": testPassword, "invite_code": code})
		must(t, e)
		req = httptest.NewRequest("POST", "http://localhost/api/v1/auth/register", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if code == admin.InviteCode {
			if w.Code != 200 || len(w.Result().Cookies()) != 1 {
				t.Fatal("valid invite rejected", w.Body.String())
			}
		} else if w.Code != 400 || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), "注册需要邀请码") {
			t.Fatal("missing invite bypass", w.Code, w.Body.String())
		}
	}
}
