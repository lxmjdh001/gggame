package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
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
		if errors.Is(err, ErrSecondaryRequired) {
			response(w, 403, map[string]any{"error": map[string]string{"code": "SECONDARY_PASSWORD_REQUIRED", "message": err.Error()}})
			return
		}
		if errors.Is(err, errSecondaryLimited) {
			response(w, 429, map[string]any{"error": map[string]string{"code": "SECONDARY_PASSWORD_RATE_LIMITED", "message": err.Error()}})
			return
		}
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
	return decodeLimit(w, r, v, 131072)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("请使用JSON请求")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
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
	mux.HandleFunc("GET /api/v1/auth/captcha", func(w http.ResponseWriter, r *http.Request) {
		if h.limited(r) {
			response(w, 429, map[string]any{"error": map[string]string{"message": "操作过于频繁，请一分钟后再试"}})
			return
		}
		challenge, err := h.Store.NewCaptcha()
		result(w, challenge, err)
	})
	mux.HandleFunc("GET /api/v1/auth/security-questions", func(w http.ResponseWriter, r *http.Request) {
		result(w, SecurityQuestions(), nil)
	})
	for _, path := range []string{"login", "register"} {
		mux.HandleFunc("POST /api/v1/auth/"+path, func(w http.ResponseWriter, r *http.Request) {
			if h.limited(r) {
				response(w, 429, map[string]any{"error": map[string]string{"message": "操作过于频繁，请一分钟后再试"}})
				return
			}
			var in SecureRegistrationInput
			if e := decode(w, r, &in); e != nil {
				result(w, nil, e)
				return
			}
			if path == "login" {
				u, token, err := h.Store.Login(strings.TrimSpace(in.Username), in.Password)
				if err == nil {
					_ = h.Store.CancelLoginSecurity(pendingLoginToken(r))
					h.setLoginChallengeCookie(w, "")
					h.setCookie(w, token)
				}
				result(w, u, err)
				return
			}
			u, token, e := h.Store.RegisterSecure(in)
			if e == nil {
				_ = h.Store.CancelLoginSecurity(pendingLoginToken(r))
				h.setLoginChallengeCookie(w, "")
				h.setCookie(w, token)
			}
			result(w, u, e)
		})
	}
	mux.HandleFunc("POST /api/v1/auth/login/verify", func(w http.ResponseWriter, r *http.Request) {
		if h.limited(r) {
			response(w, 429, map[string]any{"error": map[string]string{"message": "操作过于频繁，请一分钟后再试"}})
			return
		}
		var in LoginSecurityInput
		if err := decode(w, r, &in); err != nil {
			result(w, nil, err)
			return
		}
		u, token, err := h.Store.CompleteLoginSecurity(pendingLoginToken(r), in)
		if err == nil {
			h.setLoginChallengeCookie(w, "")
			h.setCookie(w, token)
		} else if errors.Is(err, errLoginChallengeInvalid) || errors.Is(err, ErrForbidden) {
			h.setLoginChallengeCookie(w, "")
		}
		loginSecurityResult(w, u, err)
	})
	mux.HandleFunc("POST /api/v1/auth/login/cancel", func(w http.ResponseWriter, r *http.Request) {
		err := h.Store.CancelLoginSecurity(pendingLoginToken(r))
		h.setLoginChallengeCookie(w, "")
		result(w, true, err)
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		e := h.Store.Logout(cookieToken(r))
		if err := h.Store.CancelLoginSecurity(pendingLoginToken(r)); e == nil {
			e = err
		}
		h.setLoginChallengeCookie(w, "")
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
	mux.HandleFunc("GET /api/v1/help-articles", func(w http.ResponseWriter, r *http.Request) { v, e := h.Store.HelpArticles(true); result(w, v, e) })
	mux.HandleFunc("GET /api/v1/admin/help-articles", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.HelpArticles(false)
		result(w, v, e)
	}))
	mux.HandleFunc("PUT /api/v1/admin/help-articles/{id}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in HelpArticleInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.SaveHelpArticle(u.ID, r.PathValue("id"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("DELETE /api/v1/admin/help-articles/{id}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Version int `json:"version"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.DeleteHelpArticle(u.ID, r.PathValue("id"), in.Version)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/game-control/{session}/preview", auth(true, h.previewDraw))
	mux.HandleFunc("POST /api/v1/admin/game-control/{session}/preview-batch", auth(true, h.previewDrawBatch))
	mux.HandleFunc("POST /api/v1/admin/game-control/{session}/apply", auth(true, h.applyDraw))
	mux.HandleFunc("GET /api/v1/admin/user-status", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.UserStatus(r.URL.Query().Get("username"), time.Time{}, h.Draws)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/game-control", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.GameControlWithDraws(time.Time{}, h.Draws)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/game-control/reset", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in struct {
			Key string `json:"key"`
		}
		if err := decode(w, r, &in); err != nil {
			result(w, nil, err)
			return
		}
		v, err := h.Store.ResetGameControl(u.ID, in.Key, time.Time{})
		result(w, v, err)
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
	mux.HandleFunc("GET /api/v1/admin/marksix-odds-profile/{session}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.MarkSixOddsPreset(r.PathValue("session"))
		result(w, v, e)
	}))
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
		in.gameToken = cookieToken(r)
		v, e := h.Store.PlaceMarketOrder(u.ID, in, time.Time{})
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/market-orders/batch", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MarketBatchInput
		if err := decode(w, r, &in); err != nil {
			result(w, nil, err)
			return
		}
		in.gameToken = cookieToken(r)
		v, err := h.Store.PlaceMarketBatch(u.ID, in, time.Time{})
		result(w, v, err)
	}))

	mux.HandleFunc("GET /api/v1/auth/me", auth(false, func(w http.ResponseWriter, r *http.Request, u User) { result(w, u, nil) }))
	mux.HandleFunc("GET /api/v1/auth/secondary-password/status", auth(false, func(w http.ResponseWriter, r *http.Request, _ User) {
		v, e := h.Store.SecondaryPasswordStatus(cookieToken(r))
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/auth/secondary-password", auth(false, func(w http.ResponseWriter, r *http.Request, _ User) {
		var in SecondaryPasswordInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.SetSecondaryPassword(cookieToken(r), in)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/auth/secondary-password/verify", auth(false, func(w http.ResponseWriter, r *http.Request, _ User) {
		var in struct {
			Password string `json:"password"`
		}
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.VerifySecondaryPassword(cookieToken(r), in.Password)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/auth/secondary-password/toggle", auth(false, func(w http.ResponseWriter, r *http.Request, _ User) {
		var in struct {
			Password string `json:"password"`
			Enabled  *bool  `json:"enabled"`
		}
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		if in.Enabled == nil {
			result(w, nil, errors.New("请选择开启或关闭"))
			return
		}
		v, e := h.Store.ToggleSecondaryPassword(cookieToken(r), in.Password, *in.Enabled)
		result(w, v, e)
	}))
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
	mux.HandleFunc("POST /api/v1/auth/withdrawal-password", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		if h.limited(r) {
			response(w, 429, map[string]any{"error": map[string]string{"message": "操作过于频繁，请一分钟后再试"}})
			return
		}
		var in struct {
			CurrentWithdrawalPassword string `json:"current_withdrawal_password"`
			WithdrawalPassword        string `json:"withdrawal_password"`
			Confirmation              string `json:"withdrawal_password_confirmation"`
		}
		if err := decode(w, r, &in); err != nil {
			result(w, nil, err)
			return
		}
		if in.WithdrawalPassword != in.Confirmation {
			result(w, nil, errors.New("两次提现密码不一致"))
			return
		}
		updated, err := h.Store.SetWithdrawalPassword(u.ID, in.CurrentWithdrawalPassword, in.WithdrawalPassword)
		result(w, updated, err)
	}))
	mux.HandleFunc("GET /api/v1/account", auth(false, func(w http.ResponseWriter, r *http.Request, u User) { v, e := h.Store.Account(u.ID); result(w, v, e) }))
	for _, kind := range []string{"ledger", "transactions", "daily-results"} {
		mux.HandleFunc("GET /api/v1/account/"+kind, auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
			page, err := requestPage(r)
			if err != nil {
				result(w, nil, err)
				return
			}
			switch kind {
			case "ledger":
				v, err := h.Store.LedgerPage(u.ID, page)
				result(w, v, err)
			case "transactions":
				v, err := h.Store.TransactionsPage(u.ID, page)
				result(w, v, err)
			case "daily-results":
				v, err := h.Store.DailyResultsPage(u.ID, page)
				result(w, v, err)
			}
		}))
	}
	mux.HandleFunc("GET /api/v1/orders", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		page, err := requestPage(r)
		if err != nil {
			result(w, nil, err)
			return
		}
		query := r.URL.Query()
		v, err := h.Store.OrdersPageRange(u.ID, page, query.Get("status"), query.Get("start_date"), query.Get("end_date"))
		result(w, v, err)
	}))
	mux.HandleFunc("GET /api/v1/wallet/withdrawal-settings", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.WithdrawalSettings(u.ID, false)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/wallet/withdrawal-addresses", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.WithdrawalAddresses(u.ID)
		result(w, v, e)
	}))
	mux.HandleFunc("PUT /api/v1/wallet/withdrawal-addresses/{network}", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in WithdrawalAddress
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.SaveWithdrawalAddress(u.ID, r.PathValue("network"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/withdrawal-settings", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.WithdrawalSettings(u.ID, true)
		result(w, v, e)
	}))
	mux.HandleFunc("PUT /api/v1/admin/withdrawal-settings", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in WithdrawalSettings
		e := decode(w, r, &in)
		var v WithdrawalSettings
		if e == nil {
			v, e = h.Store.SaveWithdrawalSettings(u.ID, in)
		}
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/users/{id}/wagering", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.Wagering(u.ID, r.PathValue("id"))
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/wagering", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in WagerEdit
		e := decode(w, r, &in)
		var v Wagering
		if e == nil {
			v, e = h.Store.EditWagering(u.ID, r.PathValue("id"), in)
		}
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/wallet/requests", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in WalletSubmission
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.SubmitUSDTWalletRequest(u.ID, in)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/wallet/payment-settings", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.DepositPaymentSettings(u.ID)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/payment-settings", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.PaymentSettings(u.ID)
		result(w, v, e)
	}))
	mux.HandleFunc("PUT /api/v1/admin/payment-settings", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in PaymentSettings
		if e := decodeLimit(w, r, &in, 786432); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.SavePaymentSettings(u.ID, in)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/orders", auth(false, func(w http.ResponseWriter, r *http.Request, u User) {
		var in OrderInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		in.gameToken = cookieToken(r)
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
	mux.HandleFunc("GET /api/v1/admin/requests/pending", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.PendingRequests(u.ID)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/agents/export", auth(true, h.exportAgents))
	mux.HandleFunc("GET /api/v1/admin/agents", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		page, err := requestPage(r)
		if err != nil {
			result(w, nil, err)
			return
		}
		v, err := h.Store.AgentTree(u.ID, r.URL.Query().Get("username"), r.URL.Query().Get("parent_id"), page)
		result(w, v, err)
	}))
	mux.HandleFunc("GET /api/v1/admin/{resource}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		page, err := requestPage(r)
		if err != nil {
			result(w, nil, err)
			return
		}
		v, e := h.Store.AdminData(u.ID, r.PathValue("resource"), r.URL.Query().Get("q"), page)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/users/{id}/password", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.MemberPasswordState(u.ID, r.PathValue("id"))
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/users/{id}/inviter", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.MemberInviter(u.ID, r.PathValue("id"))
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/inviter/preview", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MemberInviterInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.PreviewMemberInviter(u.ID, r.PathValue("id"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/inviter", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MemberInviterInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.ChangeMemberInviter(u.ID, r.PathValue("id"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/password", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MemberPasswordInput
		e := decode(w, r, &in)
		if e == nil {
			e = h.Store.ResetMemberPassword(u.ID, r.PathValue("id"), in)
		}
		result(w, true, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/users/{id}/descendants", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.Descendants(u.ID, r.PathValue("id"))
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/disable-descendants", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in DisableDescendantsInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.DisableDescendants(u.ID, r.PathValue("id"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("GET /api/v1/admin/users/{id}/deletion", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		v, e := h.Store.MemberDeletionState(u.ID, r.PathValue("id"))
		result(w, v, e)
	}))
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MemberDeletionInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.DeleteMember(u.ID, r.PathValue("id"), in)
		result(w, v, e)
	}))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/restore", auth(true, func(w http.ResponseWriter, r *http.Request, u User) {
		var in MemberDeletionInput
		if e := decode(w, r, &in); e != nil {
			result(w, nil, e)
			return
		}
		v, e := h.Store.RestoreMember(u.ID, r.PathValue("id"), in)
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
			Kind   string `json:"kind"`
			Amount int64  `json:"amount_cents"`
			Note   string `json:"note"`
			Key    string `json:"key"`
		}
		e := decode(w, r, &in)
		if e == nil {
			if in.Kind == "" {
				e = h.Store.Adjust(u.ID, r.PathValue("id"), in.Amount, in.Note, in.Key)
			} else {
				e = h.Store.AdjustWithKind(u.ID, r.PathValue("id"), in.Amount, in.Note, in.Key, in.Kind)
			}
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
