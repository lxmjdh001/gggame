package httpapi

import (
	"encoding/json"
	"fmt"
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
	g, _ := games.Find("lhc-daily")
	if !state.Data.NextDrawAt.Equal(g.Next(state.Data.ServerTime)) || !state.Data.BetCloseAt.Equal(g.CloseAt(state.Data.NextDrawAt)) || state.Data.Latest == nil || len(state.Data.Latest.Numbers) != 7 {
		t.Fatal("regional draw metadata mismatch")
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
	const password = "Preview-pass-123"
	if _, err = accountDB.Bootstrap("admin", password); err != nil {
		t.Fatal(err)
	}
	_, adminToken, err := accountDB.Login("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	_, userToken, err := accountDB.Register("preview_user", password, "", "13800138000", "")
	if err != nil {
		t.Fatal(err)
	}
	g, _ := games.Find("lhc-daily")
	// HTTP draw edits use the real clock. During the final seconds of an
	// issue (including the 00–05 publication gap), Next still points at a
	// draw whose editing deadline has passed or is about to pass. Start the
	// fixture in the next full window instead of racing that deadline.
	now := time.Now()
	due := g.Next(now)
	if due.Add(-games.SourcePublishDelay).Sub(now) < 10*time.Second {
		time.Sleep(time.Until(due) + 10*time.Millisecond)
	}
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
	// Draw actions require admin authentication and same-origin JSON.
	for _, action := range []string{"preview", "preview-batch", "apply"} {
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

	for _, count := range []int{0, 1, 5, 2, 6} {
		body := fmt.Sprintf(`{"issue":%q,"count":%d}`, d.Issue, count)
		req := httptest.NewRequest("POST", "http://localhost/api/v1/admin/game-control/lhc-daily/preview-batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "starlight_session", Value: adminToken})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		expected := 200
		if count == 2 || count == 6 {
			expected = 400
		}
		if w.Code != expected {
			t.Fatalf("batch %d returned %d: %s", count, w.Code, w.Body.String())
		}
		if expected == 200 {
			var response model.Envelope[struct {
				Current    games.Round `json:"current"`
				Candidates []struct {
					Candidate store.DrawCandidate   `json:"candidate"`
					Estimate  accounts.NextEstimate `json:"estimate"`
				} `json:"candidates"`
			}]
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Data.Candidates) != count || response.Data.Current.Issue != d.Issue {
				t.Fatal("incorrect batch response")
			}
			for _, candidate := range response.Data.Candidates {
				if len(candidate.Candidate.Round.Numbers) != 7 || candidate.Estimate.Net == nil || *candidate.Estimate.Net != 0 {
					t.Fatal("invalid marksix batch")
				}
			}
		}
	}
	// Manual drafts use the same protected route and remain private until adopted.
	for _, tc := range []struct {
		token, extra string
		status       int
	}{
		{"", "", 401}, {userToken, "", 403}, {adminToken, "", 200},
		{adminToken, `,"random":true`, 400},
	} {
		body := `{"issue":"` + d.Issue + `","numbers":[7,6,5,4,3,2,1],"expected_numbers":[1,2,3,4,5,6,7]` + tc.extra + `}`
		req := httptest.NewRequest("POST", "http://localhost/api/v1/admin/game-control/lhc-daily/preview", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("manual preview: got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
		if tc.status == 200 {
			var response model.Envelope[store.DrawPreview]
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Data.Candidate == nil || response.Data.Candidate.Round.Numbers[0] != 7 || response.Data.Current.Numbers[0] != 1 {
				t.Fatal("manual preview response incorrect")
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
