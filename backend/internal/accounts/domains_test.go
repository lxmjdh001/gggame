package accounts

import (
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainRoutesRequireAdminSameOriginAndTrustedActor(t *testing.T) {
	s := agentFixture(t)
	var adminToken, userToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var e error
		adminToken, e = newLogin(tx, "admin")
		if e != nil {
			return e
		}
		userToken, e = newLogin(tx, "a")
		return e
	}))
	dir, e := os.MkdirTemp("/tmp", "sl-domain-")
	must(t, e)
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "control.sock")
	l, e := net.Listen("unix", socket)
	must(t, e)
	calls := make(chan map[string]any, 10)
	remote := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "GET" {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		calls <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"version":1,"slots":[]}}`)
	})}
	go remote.Serve(l)
	defer remote.Close()
	h := &HTTP{Store: s, DomainSocket: socket}
	mux := http.NewServeMux()
	h.Bind(mux)
	request := func(method, path, token, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost/api/v1/admin/domains"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {userToken, 403}, {adminToken, 200}} {
		if w := request("GET", "", tc.token, "", ""); w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	<-calls
	for _, tc := range []struct {
		method, path, token, origin, body string
		status                            int
	}{
		{"PUT", "", userToken, "http://localhost", `{"version":1,"domains":[]}`, 403},
		{"PUT", "", adminToken, "https://evil.example", `{"version":1,"domains":[]}`, 403},
		{"POST", "/actions", adminToken, "http://localhost", `{"version":1,"index":0,"action":"enable","actor":"spoofed"}`, 400},
		{"PUT", "", adminToken, "http://localhost", `{"version":1,"domains":["a.example.com","","","",""]}`, 200},
		{"POST", "/actions", adminToken, "http://localhost", `{"version":1,"index":0,"action":"disable"}`, 200},
	} {
		w := request(tc.method, tc.path, tc.token, tc.origin, tc.body)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code, w.Body.String())
		}
		if tc.status == 200 {
			body := <-calls
			if body["actor"] != "admin" {
				t.Fatal("caller controlled actor", body)
			}
		}
	}
	if len(calls) != 0 {
		t.Fatal("unauthorized request reached privileged service")
	}
	h.DomainSocket = ""
	w := request("GET", "", adminToken, "", "")
	if w.Code != 400 || !strings.Contains(w.Body.String(), "尚未安装") {
		t.Fatal(w.Code, w.Body.String())
	}
}
