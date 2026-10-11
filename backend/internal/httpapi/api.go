package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"starlight/backend/internal/accounts"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"strconv"
	"time"
)

type API struct {
	Store    *store.Store
	Accounts *accounts.HTTP
}

func send(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func ok(w http.ResponseWriter, data any) { send(w, 200, model.Envelope[any]{Data: data}) }
func fail(w http.ResponseWriter, status int, code, msg string) {
	send(w, status, model.ErrorEnvelope{Error: model.ErrorBody{Code: code, Message: msg}})
}
func session(w http.ResponseWriter, r *http.Request) (games.Session, bool) {
	s, found := games.Find(r.PathValue("session"))
	if !found {
		fail(w, 404, "SESSION_NOT_FOUND", "unknown session")
	}
	return s, found
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	if a.Accounts != nil {
		a.Accounts.Bind(mux)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"status": "ok", "server_time": time.Now().In(model.Beijing)})
	})
	mux.HandleFunc("GET /api/v1/games", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"timezone": "Asia/Shanghai", "sessions": games.All()})
	})
	mux.HandleFunc("GET /api/v1/sessions/{session}/state", func(w http.ResponseWriter, r *http.Request) {
		s, found := session(w, r)
		if !found {
			return
		}
		history, err := a.Store.History(s.ID, 1, "")
		if err != nil {
			fail(w, 500, "STORAGE_ERROR", "read failed")
			return
		}
		now := time.Now().In(model.Beijing)
		state := games.State{Session: s, ServerTime: now, ExpectedIssue: s.Due(now).Format(model.IssueLayout), NextDrawAt: s.Next(now), Pending: true}
		state.BetCloseAt = s.CloseAt(state.NextDrawAt)
		state.BettingOpen = s.BettingOpen(now)
		if len(history.Items) > 0 {
			state.Latest = &history.Items[0]
			state.Pending = state.Latest.Issue != state.ExpectedIssue
		}
		ok(w, state)
	})
	mux.HandleFunc("GET /api/v1/sessions/{session}/draws", func(w http.ResponseWriter, r *http.Request) {
		s, found := session(w, r)
		if !found {
			return
		}
		limit := 20
		var err error
		if r.URL.Query().Has("limit") {
			limit, err = strconv.Atoi(r.URL.Query().Get("limit"))
			if err != nil || limit < 1 || limit > 100 {
				fail(w, 400, "INVALID_LIMIT", "limit must be 1..100")
				return
			}
		}
		before := r.URL.Query().Get("before")
		if before != "" {
			if _, err = model.ParseIssue(before); err != nil {
				fail(w, 400, "INVALID_ISSUE", "invalid cursor")
				return
			}
		}
		result, err := a.Store.History(s.ID, limit, before)
		if err != nil {
			fail(w, 500, "STORAGE_ERROR", "read failed")
			return
		}
		ok(w, result)
	})
	mux.HandleFunc("GET /api/v1/sessions/{session}/draws/{issue}", func(w http.ResponseWriter, r *http.Request) {
		s, found := session(w, r)
		if !found {
			return
		}
		issue := r.PathValue("issue")
		if _, err := model.ParseIssue(issue); err != nil {
			fail(w, 400, "INVALID_ISSUE", "invalid issue")
			return
		}
		round, err := a.Store.Get(s.ID, issue)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 404, "DRAW_NOT_FOUND", "issue not found")
			return
		}
		if err != nil {
			fail(w, 500, "STORAGE_ERROR", "read failed")
			return
		}
		ok(w, round)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "NOT_FOUND", "endpoint not found") })
	if a.Accounts != nil {
		return a.Accounts.Protect(mux)
	}
	return mux
}
