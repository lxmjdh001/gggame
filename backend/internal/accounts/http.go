package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HTTP struct {
	Draws           GameControlDrawReader
	Store           *Store
	SecureCookie    bool
	ResultAvailable func(session, issue string) bool
	mu              sync.Mutex
	attempts        map[string]attempt
}
type attempt struct {
	Count int
	Reset time.Time
}

func response(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func result(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := 400
		if errors.Is(err, ErrUnauthorized) {
			status = 401
		}
		if errors.Is(err, ErrForbidden) {
			status = 403
		}
		if errors.Is(err, ErrNotFound) {
			status = 404
		}
		response(w, status, map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	response(w, 200, map[string]any{"data": v})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("请使用JSON请求")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 131072)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("请求格式不正确")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("请求格式不正确")
	}
	return nil
}
func cookieToken(r *http.Request) string {
	c, e := r.Cookie("starlight_session")
	if e != nil {
		return ""
	}
	return c.Value
}
func (h *HTTP) setCookie(w http.ResponseWriter, token string) {
	age := 604800
	if token == "" {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: "starlight_session", Value: token, Path: "/", HttpOnly: true, Secure: h.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: age})
}
func (h *HTTP) limited(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := host + ":" + r.URL.Path
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attempts == nil {
		h.attempts = map[string]attempt{}
	}
	now := time.Now()
	for k, v := range h.attempts {
		if now.After(v.Reset) {
			delete(h.attempts, k)
		}
	}
	v := h.attempts[key]
	if v.Reset.IsZero() {
		v.Reset = now.Add(time.Minute)
	}
	v.Count++
	h.attempts[key] = v
	return v.Count > 15
}
func (h *HTTP) Bind(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/site", func(w http.ResponseWriter, r *http.Request) { cfg, e := h.Store.Settings(); result(w, cfg, e) })
	for _, path := range []string{"login", "register"} {
		mux.HandleFunc("POST /api/v1/auth/"+path, func(w http.ResponseWriter, r *http.Request) {
			if h.limited(r) {
				response(w, 429, map[string]any{"error": map[string]string{"message": "操作过于频繁，请一分钟后再试"}})
				return
			}
			var in struct {
				Username string `json:"username"`
				Password string `json:"password"`
				Invite   string `json:"invite_code"`
			}
			if e := decode(w, r, &in); e != nil {
				result(w, nil, e)
				return
			}
			var u User
			var token string
			var e error
			if path == "register" {
				u, token, e = h.Store.Register(strings.TrimSpace(in.Username), in.Password, strings.TrimSpace(in.Invite))
			} else {
				u, token, e = h.Store.Login(strings.TrimSpace(in.Username), in.Password)
			}
			if e == nil {
				h.setCookie(w, token)
			}
			result(w, u, e)
		})
	}
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		e := h.Store.Logout(cookieToken(r))
		h.setCookie(w, "")
		result(w, true, e)
	})
	auth := func(adminOnly bool, f func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u, e := h.Store.Authenticate(cookieToken(r))
			if e != nil {
				result(w, nil, e)
				return
			}
			if adminOnly && u.Role != "admin" {
				result(w, nil, ErrForbidden)
				return
			}
			f(w, r, u)
		}
	}
	mux.HandleFunc("GET /api/v1/promotions", func(w http.ResponseWriter, r *http.Request) { v, e := h.Store.Promotions(true); result(w, v, e) })
	mux.HandleFunc("POST /api/v1/admin/game-control/{session}/preview", auth(true, h.previewDraw))
	mux.HandleFunc("POST /api/v1/admin/game-control/{session}/apply", auth(true, h.applyDraw))
	mux.HandleFunc("GET /api/v1/admin/game-control", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.GameControlWithDraws(time.Time{}, h.Draws)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/promotions", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.Promotions(false)
		result(w, v, e)
	}))
	mux.HandleFunc("PUT /api/v1/admin/promotions/{id}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in PromotionInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.SavePromotion(u.ID, r.PathValue("id"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("DELETE /api/v1/admin/promotions/{id}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Version int `json:"version"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.DeletePromotion(u.ID, r.PathValue("id"), in.Version)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("GET /api/v1/markets/{session}", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Store.MarketBook(r.PathValue("session"))
		result(w, v, e)
	})
	mux.HandleFunc("PUT /api/v1/admin/markets/{session}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Quotes []Quote `json:"quotes"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.SaveQuotes(u.ID, r.PathValue("session"), in.Quotes)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("POST /api/v1/market-orders", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MarketOrderInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.PlaceMarketOrder(u.ID, in, time.Time{})
		result(w, v, e)
	}))

	mux.HandleFunc("GET /api/v1/auth/me", auth(false, func(w http.ResponseWriter, r *http.Request, u User) { result(w, u, nil) }))
	mux.HandleFunc("POST /api/v1/auth/password", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		if h.limited(r) {
			result(w, nil, errors.New("操作过于频繁，请稍后再试"))
			return
		}
		var in struct {
			Old  string `json:"old_password"`
			Next string `json:"new_password"`
		}
		e := decode(w, r, &in)
		if e == nil {
			if len(in.Old) > 128 {
				e = errors.New("原密码不正确")
			} else {
				e = h.Store.ChangePassword(u.ID, in.Old, in.Next)
			}
		}
		if e == nil {
			h.setCookie(w, "")
		}
		result(w, true, e)
	}))
	mux.HandleFunc("GET /api/v1/account", auth(false, func(w http.ResponseWriter, r *http.Request, u User) { v, e := h.Store.Account(u.ID); result(w, v, e) }))
	mux.HandleFunc("POST /api/v1/wallet/requests", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in RequestInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.CreateRequest(u.ID, in)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/orders", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in OrderInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.PlaceOrder(u.ID, in, time.Time{})
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/commissions/claim", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.Claim(u.ID)
		result(w, map[string]any{"claimed_cents": v}, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/settings", auth(true, func(w http.ResponseWriter, r *http.Request, u User) { v, e := h.Store.Settings(); result(w, v, e) }))
	mux.HandleFunc("PUT /api/v1/admin/settings", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in Settings
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.SaveSettings(u.ID, in)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/{resource}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		page := 1
		if v := r.URL.Query().Get("page"); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > 1000000 {
				result(w, nil, errors.New("页码不正确"))
				return
			}
			page = n
		}
		v, e := h.Store.AdminData(u.ID, r.PathValue("resource"), r.URL.Query().Get("q"), page)
		result(w, v, e)
	}))
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Status string `json:"status"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.SetStatus(u.ID, r.PathValue("id"), in.Status)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/balance", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Amount int64  `json:"amount_cents"`
			Note   string `json:"note"`
			Key    string `json:"key"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.Adjust(u.ID, r.PathValue("id"), in.Amount, in.Note, in.Key)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/requests/{id}/review", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.ReviewRequest(u.ID, r.PathValue("id"), in.Decision, in.Note)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/orders/{id}/settle", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
			Payout   int64  `json:"payout_cents"`
		}
		e := decode(w, r, &in)
		if e == nil {
			o, err := h.Store.Order(r.PathValue("id"))
			e = err
			if e == nil {
				available := false
				if h.ResultAvailable != nil {
					available = h.ResultAvailable(o.SessionID, o.Issue)
				}
				e = h.Store.SettleOrder(u.ID, o.ID, in.Decision, in.Note, in.Payout, available)
			}
		}
		result(w, true, e)
	}))
}

// Same-origin JSON requests plus a strict cookie prevent cross-site mutations. No CORS is enabled.
func (h *HTTP) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, e := url.Parse(origin)
				if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
					result(w, nil, ErrForbidden)
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				result(w, nil, ErrForbidden)
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				result(w, nil, errors.New("请使用JSON请求"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
