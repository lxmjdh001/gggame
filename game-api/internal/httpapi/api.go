package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"starlight/gameapi/internal/scheduler"
	"starlight/gameapi/internal/storage"
	"starlight/gameapi/pkg/model"
	"strconv"
	"strings"
	"time"
)

type Store interface {
	Latest() (model.Draw, error)
	Get(string) (model.Draw, error)
	List(int, string) (model.Page, error)
}
type API struct {
	Store  Store
	Token  string
	Logger *slog.Logger
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"status": "ok", "timezone": "Asia/Shanghai", "server_time": time.Now().In(model.Beijing), "next_scheduled_at": scheduler.NextDay(time.Now())})
	})
	mux.HandleFunc("GET /readyz", a.ready)
	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		success(w, model.Metadata{GenerationMode: "daily_pre_generated", BatchesPerDay: 1440, Timezone: "Asia/Shanghai", IntervalSeconds: 86400, TriggerSecond: 0, PositionBase: 1, Rules: model.Rules()})
	})
	mux.HandleFunc("GET /api/v1/draws", a.list)
	mux.HandleFunc("GET /api/v1/draws/{issue}", a.draw)
	mux.HandleFunc("GET /api/v1/draws/{issue}/groups/{group}", a.selection)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "NOT_FOUND", "endpoint not found") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") && (a.Token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			fail(w, 401, "UNAUTHORIZED", "valid bearer token required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, 405, "METHOD_NOT_ALLOWED", "read-only API")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func success(w http.ResponseWriter, data any) { write(w, 200, model.Envelope[any]{Data: data}) }
func fail(w http.ResponseWriter, status int, code, message string) {
	write(w, status, model.ErrorEnvelope{Error: model.ErrorBody{Code: code, Message: message}})
}
func (a *API) internal(w http.ResponseWriter, err error) {
	if a.Logger != nil {
		a.Logger.Error("API storage failure", "error", err)
	}
	fail(w, 500, "INTERNAL_ERROR", "storage unavailable")
}
func (a *API) lookup(w http.ResponseWriter, issue string) (model.Draw, bool) {
	var d model.Draw
	var err error
	if issue == "latest" {
		d, err = a.Store.Latest()
	} else {
		if _, e := model.ParseIssue(issue); e != nil {
			fail(w, 400, "INVALID_ISSUE", e.Error())
			return d, false
		}
		d, err = a.Store.Get(issue)
	}
	if errors.Is(err, storage.ErrNotFound) {
		if issue == "latest" {
			fail(w, 503, "NO_DRAW_YET", "waiting for first scheduled draw")
		} else {
			fail(w, 404, "DRAW_NOT_FOUND", "issue has not been generated")
		}
		return d, false
	}
	if err != nil {
		a.internal(w, err)
		return d, false
	}
	return d, true
}
func (a *API) draw(w http.ResponseWriter, r *http.Request) {
	if d, ok := a.lookup(w, r.PathValue("issue")); ok {
		success(w, d)
	}
}
func (a *API) selection(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	valid := false
	max := 0
	for _, rule := range model.Rules() {
		if group == rule.ID {
			valid = true
			max = rule.Count
		}
	}
	if !valid {
		fail(w, 400, "INVALID_GROUP", "group must be g1 through g11")
		return
	}
	positions := []int{}
	if raw, exists := r.URL.Query()["positions"]; exists {
		if len(raw) != 1 || raw[0] == "" {
			fail(w, 400, "INVALID_POSITIONS", "provide a comma-separated list of positions")
			return
		}
		fields := strings.Split(raw[0], ",")
		if len(fields) > max {
			fail(w, 400, "INVALID_POSITIONS", "too many positions")
			return
		}
		seen := map[int]bool{}
		for _, field := range fields {
			p, err := strconv.Atoi(strings.TrimSpace(field))
			if err != nil || p < 1 || p > max || seen[p] {
				fail(w, 400, "INVALID_POSITIONS", "positions must be distinct, one-based and within group size")
				return
			}
			seen[p] = true
			positions = append(positions, p)
		}
	}
	d, ok := a.lookup(w, r.PathValue("issue"))
	if !ok {
		return
	}
	if _, exists := d.Groups[group]; !exists {
		fail(w, 404, "GROUP_UNAVAILABLE", "group unavailable for this historical schema")
		return
	}
	selection, err := model.Select(d, group, positions)
	if err != nil {
		a.internal(w, err)
		return
	}
	success(w, selection)
}
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw, exists := r.URL.Query()["limit"]; exists {
		var err error
		if len(raw) != 1 {
			fail(w, 400, "INVALID_LIMIT", "limit must be 1..100")
			return
		}
		limit, err = strconv.Atoi(raw[0])
		if err != nil || limit < 1 || limit > 100 {
			fail(w, 400, "INVALID_LIMIT", "limit must be 1..100")
			return
		}
	}
	before := r.URL.Query().Get("before")
	if before != "" {
		if _, err := model.ParseIssue(before); err != nil {
			fail(w, 400, "INVALID_ISSUE", "invalid before cursor")
			return
		}
	}
	page, err := a.Store.List(limit, before)
	if err != nil {
		a.internal(w, err)
		return
	}
	success(w, page)
}
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	d, err := a.Store.Latest()
	if errors.Is(err, storage.ErrNotFound) {
		fail(w, 503, "NO_DRAW_YET", "waiting for first scheduled draw")
		return
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	end := scheduler.NextDay(time.Now()).Add(-time.Minute + time.Second)
	if !d.ScheduledAt.Equal(end) {
		fail(w, 503, "DAY_NOT_READY", "current daily number batch is not complete")
		return
	}
	success(w, map[string]any{"status": "ready", "latest_issue": d.Issue})
}
