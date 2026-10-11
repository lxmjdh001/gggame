package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"starlight/backend/internal/accounts"
	"starlight/backend/internal/engine"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"strings"
	"testing"
	"time"
)

func TestSessionAPI(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e := engine.Engine{Store: db}
	if err = e.Seed(time.Now()); err != nil {
		t.Fatal(err)
	}
	handler := (&API{Store: db}).Handler()
	for _, tc := range []struct {
		path string
		code int
	}{{"/api/v1/games", 200}, {"/api/v1/sessions/lhc-daily/state", 200}, {"/api/v1/sessions/pc28-5/draws?limit=100", 200}, {"/api/v1/sessions/no/state", 404}, {"/api/v1/sessions/ffc-3/draws?limit=101", 400}, {"/api/v1/sessions/ffc-3/draws?before=wrong", 400}, {"/api/v1/sessions/ffc-3/draws/200001010000", 404}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Errorf("%s status %d", tc.path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sessions/lhc-daily/state", nil))
	var state model.Envelope[games.State]
	if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Data.NextDrawAt.Hour() != 21 || state.Data.NextDrawAt.Minute() != 30 || state.Data.Latest == nil || len(state.Data.Latest.Numbers) != 7 {
		t.Fatal("daily draw metadata mismatch")
	}
	first, _ := db.History("pc28-5", 20, "")
	second, _ := db.History("pc28-5", 20, first.NextBefore)
	if len(first.Items) != 20 || len(second.Items) != 20 || first.Items[19].Issue <= second.Items[0].Issue {
		t.Fatal("pagination incorrect")
	}
}

func TestPreparedDrawOnlyVisibleToAdmin(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	accountDB, err := accounts.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer accountDB.Close()
	const password = "preview-test-password-123"
	if _, err = accountDB.Bootstrap("admin", password); err != nil {
		t.Fatal(err)
	}
	_, adminToken, err := accountDB.Login("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	_, userToken, err := accountDB.Register("preview_user", password, "")
	if err != nil {
		t.Fatal(err)
	}
	g, _ := games.Find("lhc-daily")
	at := g.Next(time.Now()).Add(-games.SourcePublishDelay)
	d := model.Draw{SchemaVersion: model.SchemaVersion, Issue: at.Format(model.IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: at.Add(-24 * time.Hour), Groups: map[string][]int{}}
	for _, rule := range model.Rules() {
		for i := 0; i < rule.Count; i++ {
			d.Groups[rule.ID] = append(d.Groups[rule.ID], rule.Min+i%(rule.Max-rule.Min+1))
		}
	}
	model.Display(&d)
	if err = db.CacheSources([]model.Draw{d}); err != nil {
		t.Fatal(err)
	}
	handler := (&API{Store: db, Accounts: &accounts.HTTP{Store: accountDB, Draws: db}}).Handler()
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {userToken, 403}, {adminToken, 200}} {
		r := httptest.NewRequest("GET", "http://localhost/api/v1/admin/game-control", nil)
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("unexpected access status %d: %s", w.Code, w.Body.String())
		}
		if tc.status == 200 {
			var result model.Envelope[accounts.GameControl]
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range result.Data.Sessions {
				if row.SessionID == g.ID {
					found = row.Next != nil && row.Next.Issue == d.Issue && len(row.Next.Numbers) == 7
				}
			}
			if !found {
				t.Fatal("administrator cannot see prepared draw")
			}
		}
	}
	// Both new actions require admin authentication and same-origin JSON.
	for _, action := range []string{"preview", "apply"} {
		for _, tc := range []struct {
			token, origin string
			status        int
		}{{"", "", 401}, {userToken, "", 403}, {adminToken, "https://attacker.test", 403}} {
			r := httptest.NewRequest("POST", "http://localhost/api/v1/admin/game-control/lhc-daily/"+action, strings.NewReader(`{"issue":"`+d.Issue+`","candidate_id":"invalid"}`))
			r.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.token != "" {
				r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%s access returned %d instead of %d", action, w.Code, tc.status)
			}
		}
	}
	for _, path := range []string{"/api/v1/sessions/lhc-daily/state", "/api/v1/sessions/lhc-daily/draws", "/api/v1/sessions/lhc-daily/draws/" + d.Issue} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if path == "/api/v1/sessions/lhc-daily/state" {
			var state model.Envelope[games.State]
			if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if state.Data.Latest != nil {
				t.Fatal("future draw leaked through public state")
			}
		} else if path == "/api/v1/sessions/lhc-daily/draws" {
			var history model.Envelope[games.History]
			if err = json.Unmarshal(w.Body.Bytes(), &history); err != nil {
				t.Fatal(err)
			}
			if len(history.Data.Items) != 0 {
				t.Fatal("future draw leaked through history")
			}
		} else if w.Code != 404 {
			t.Fatal("future draw leaked through public issue lookup")
		}
	}
}
