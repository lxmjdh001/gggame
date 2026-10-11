package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func activityInput() PromotionInput {
	return PromotionInput{Title: "测试活动", Tagline: "限时福利", Summary: "活动简介", Content: "参与方式\n活动规则", Button: "查看详情", Action: "details", Icon: "gift", Sort: 5}
}
func TestPromotionLifecycleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	_, e = s.Bootstrap("admin", testPassword)
	must(t, e)
	a, _, e := s.Login("admin", testPassword)
	must(t, e)
	items, e := s.Promotions(true)
	must(t, e)
	if len(items) != 2 {
		t.Fatal("default promotions not seeded")
	}
	in := activityInput()
	p, e := s.SavePromotion(a.ID, "new_activity", in)
	must(t, e)
	if p.Theme != "peach" || p.Version != 1 {
		t.Fatal("automatic theme or version incorrect", p)
	}
	items, e = s.Promotions(true)
	must(t, e)
	if len(items) != 2 {
		t.Fatal("draft leaked publicly")
	}
	stale := in
	in = p.PromotionInput
	in.Enabled = true
	p, e = s.SavePromotion(a.ID, p.ID, in)
	must(t, e)
	items, e = s.Promotions(true)
	must(t, e)
	if len(items) != 3 || items[0].ID != p.ID {
		t.Fatal("publish or sort failed")
	}
	if _, e = s.SavePromotion(a.ID, p.ID, stale); e == nil {
		t.Fatal("stale editor overwrote activity")
	}
	for _, bad := range []PromotionInput{func() PromotionInput { v := in; v.Theme = "evil"; return v }(), func() PromotionInput { v := in; v.Action = "javascript:alert(1)"; return v }(), func() PromotionInput { v := in; v.Title = " "; return v }(), func() PromotionInput { v := in; v.Sort = -1; return v }()} {
		if _, e = s.SavePromotion(a.ID, "bad_activity", bad); e == nil {
			t.Fatal("invalid activity accepted")
		}
	}
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	items, e = s.Promotions(true)
	must(t, e)
	if len(items) != 3 || items[0].Content != "参与方式\n活动规则" {
		t.Fatal("content did not persist")
	}
	in = p.PromotionInput
	in.Enabled = false
	p, e = s.SavePromotion(a.ID, p.ID, in)
	must(t, e)
	items, e = s.Promotions(true)
	must(t, e)
	if len(items) != 2 {
		t.Fatal("unpublish failed")
	}
	if s.DeletePromotion(a.ID, p.ID, p.Version-1) == nil {
		t.Fatal("stale deletion allowed")
	}
	all, e := s.Promotions(false)
	must(t, e)
	for _, x := range all {
		must(t, s.DeletePromotion(a.ID, x.ID, x.Version))
	}
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	defer s.Close()
	all, e = s.Promotions(false)
	must(t, e)
	if len(all) != 0 {
		t.Fatal("deleted defaults recreated")
	}
}
func TestPromotionHTTPPermissions(t *testing.T) {
	s, a := setup(t)
	u := register(t, s, "promotion_user", "")
	_, at, e := s.Login(a.Username, testPassword)
	must(t, e)
	_, ut, e := s.Login(u.Username, testPassword)
	must(t, e)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	in := activityInput()
	raw, e := json.Marshal(in)
	must(t, e)
	for _, tc := range []struct {
		method, path, token, body string
		want                      int
	}{
		{"GET", "/api/v1/promotions", "", "", 200}, {"GET", "/api/v1/admin/promotions", "", "", 401}, {"GET", "/api/v1/admin/promotions", ut, "", 403},
		{"PUT", "/api/v1/admin/promotions/http_activity", ut, string(raw), 403}, {"DELETE", "/api/v1/admin/promotions/invitation", ut, `{"version":1}`, 403},
		{"PUT", "/api/v1/admin/promotions/http_activity", at, string(raw), 200},
	} {
		w := call(tc.method, tc.path, tc.token, tc.body)
		if w.Code != tc.want {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
	}
	w := call("GET", "/api/v1/promotions", "", "")
	if strings.Contains(w.Body.String(), "http_activity") {
		t.Fatal("draft leaked via public HTTP")
	}
	in.Version = 1
	in.Enabled = true
	raw, e = json.Marshal(in)
	must(t, e)
	w = call("PUT", "/api/v1/admin/promotions/http_activity", at, string(raw))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/v1/promotions", "", "")
	if !strings.Contains(w.Body.String(), "http_activity") {
		t.Fatal("published activity missing")
	}
	w = call("DELETE", "/api/v1/admin/promotions/http_activity", at, `{"version":2}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
