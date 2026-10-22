package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpLifecycleAndNoReseed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	must(t, err)
	_, err = s.Bootstrap("admin", testPassword)
	must(t, err)
	a, _, err := s.Login("admin", testPassword)
	must(t, err)
	items, err := s.HelpArticles(true)
	must(t, err)
	if len(items) != 4 {
		t.Fatal("missing default help")
	}
	in := HelpArticleInput{Title: "新增帮助", Content: "第一行\n第二行", Sort: 1}
	p, err := s.SaveHelpArticle(a.ID, "new_help", in)
	must(t, err)
	items, err = s.HelpArticles(true)
	must(t, err)
	if len(items) != 4 {
		t.Fatal("draft leaked")
	}
	in = p.HelpArticleInput
	in.Enabled = true
	p, err = s.SaveHelpArticle(a.ID, p.ID, in)
	must(t, err)
	items, err = s.HelpArticles(true)
	must(t, err)
	if len(items) != 5 || items[0].ID != p.ID {
		t.Fatal("publish/sort failed")
	}
	if _, err = s.SaveHelpArticle(a.ID, p.ID, in); err == nil {
		t.Fatal("stale edit allowed")
	}
	if s.DeleteHelpArticle(a.ID, p.ID, in.Version) == nil {
		t.Fatal("stale delete allowed")
	}
	for _, bad := range []HelpArticleInput{{Title: " ", Content: "text"}, {Title: "text", Content: " "}, {Title: strings.Repeat("字", 81), Content: "text"}, {Title: "text", Content: strings.Repeat("字", 10001)}, {Title: "text", Content: "text", Sort: -1}, {Title: "text", Content: "text", Sort: 10000}} {
		if _, err := s.SaveHelpArticle(a.ID, "bad_help", bad); err == nil {
			t.Fatal("invalid content allowed")
		}
	}
	in = p.HelpArticleInput
	in.Enabled = false
	p, err = s.SaveHelpArticle(a.ID, p.ID, in)
	must(t, err)
	items, err = s.HelpArticles(true)
	must(t, err)
	if len(items) != 4 {
		t.Fatal("unpublish failed")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	all, err := s.HelpArticles(false)
	must(t, err)
	if len(all) != 5 || all[0].Content != "第一行\n第二行" || all[0].Version != 3 {
		t.Fatal("content not persistent")
	}
	for _, x := range all {
		must(t, s.DeleteHelpArticle(a.ID, x.ID, x.Version))
	}
	if _, err = s.SaveHelpArticle(a.ID, p.ID, p.HelpArticleInput); err == nil {
		t.Fatal("stale editor recreated deleted content")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer s.Close()
	all, err = s.HelpArticles(false)
	must(t, err)
	if len(all) != 0 {
		t.Fatal("deleted defaults recreated")
	}
	log, err := s.AdminData(a.ID, "audit", "help.", 1)
	must(t, err)
	if log["total"].(int) < 8 {
		t.Fatal("help audit missing", log)
	}
}

func TestHelpHTTPPermissionsAndPublication(t *testing.T) {
	s, a := setup(t)
	u := register(t, s, "help_user", "")
	_, at, err := s.Login(a.Username, testPassword)
	must(t, err)
	_, ut, err := s.Login(u.Username, testPassword)
	must(t, err)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	in := HelpArticleInput{Title: "HTTP帮助", Content: "<script>alert(1)</script>\n纯文本", Sort: 0}
	raw, err := json.Marshal(in)
	must(t, err)
	for _, tc := range []struct {
		method, path, token, body, origin string
		status                            int
	}{
		{"GET", "/api/v1/help-articles", "", "", "", 200},
		{"GET", "/api/v1/admin/help-articles", "", "", "", 401},
		{"GET", "/api/v1/admin/help-articles", ut, "", "", 403},
		{"PUT", "/api/v1/admin/help-articles/http_help", "", string(raw), "", 401},
		{"PUT", "/api/v1/admin/help-articles/http_help", ut, string(raw), "", 403},
		{"DELETE", "/api/v1/admin/help-articles/betting", ut, `{"version":1}`, "", 403},
		{"PUT", "/api/v1/admin/help-articles/http_help", at, string(raw), "https://attacker.invalid", 403},
		{"DELETE", "/api/v1/admin/help-articles/betting", at, `{"version":1}`, "https://attacker.invalid", 403},
		{"PUT", "/api/v1/admin/help-articles/http_help", at, string(raw), "http://localhost", 200},
	} {
		w := call(tc.method, tc.path, tc.token, tc.body, tc.origin)
		if w.Code != tc.status {
			t.Fatal(tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	w := call("GET", "/api/v1/help-articles", "", "", "")
	if strings.Contains(w.Body.String(), "http_help") {
		t.Fatal("draft leaked")
	}
	in.Version = 1
	in.Enabled = true
	raw, err = json.Marshal(in)
	must(t, err)
	if w = call("PUT", "/api/v1/admin/help-articles/http_help", at, string(raw), ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/v1/help-articles", "", "", "")
	if !strings.Contains(w.Body.String(), "http_help") || strings.Contains(w.Body.String(), "<script>") {
		t.Fatal("public publication or escaping failed")
	}
	if w = call("DELETE", "/api/v1/admin/help-articles/http_help", at, `{"version":2}`, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/v1/help-articles", "", "", "")
	if strings.Contains(w.Body.String(), "http_help") {
		t.Fatal("deleted article remains")
	}
}
