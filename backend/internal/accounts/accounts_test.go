package accounts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testPassword = "Test-password-12345"

func setup(t *testing.T) (*Store, User) {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if _, e = s.Bootstrap("admin", testPassword); e != nil {
		t.Fatal(e)
	}
	u, _, e := s.Login("admin", testPassword)
	if e != nil {
		t.Fatal(e)
	}
	return s, u
}
func register(t *testing.T, s *Store, name, invite string) User {
	t.Helper()
	u, _, e := s.Register(name, testPassword, invite, "13800138000", "")
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func input(now time.Time) OrderInput {
	g, _ := games.Find("ffc-1")
	return OrderInput{UnitCents: 200, SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), Numbers: []int{1, 2, 3, 4, 5}, Multiplier: 10, Key: randomID()}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestThreeLevelRelationshipSnapshotAndCommission(t *testing.T) {
	s, admin := setup(t)
	root := register(t, s, "root_user", "")
	a := register(t, s, "invite_a", root.InviteCode)
	b := register(t, s, "invite_b", a.InviteCode)
	c := register(t, s, "invite_c", b.InviteCode)
	d := register(t, s, "invite_d", c.InviteCode)
	if c.InviterID != b.ID {
		t.Fatal("C must retain B as its inviter")
	}
	profile, e := s.Account(c.ID)
	must(t, e)
	if profile["inviter"].(map[string]string)["id"] != b.ID {
		t.Fatal("inviter information missing from profile")
	}
	if profile["team_counts"].([3]int)[0] != 1 {
		t.Fatal("C must also be D's inviter")
	}
	cfg, e := s.Settings()
	must(t, e)
	cfg.CommissionBPS = [3]int64{500, 200, 100}
	must(t, s.SaveSettings(admin.ID, cfg))
	must(t, s.Adjust(admin.ID, d.ID, 10000, "测试账户入账", randomID()))
	now := time.Date(2026, 10, 11, 18, 20, 10, 0, model.Beijing)
	in := input(now)
	o, e := s.PlaceOrder(d.ID, in, now)
	must(t, e)
	if len(o.Ancestors) != 3 || o.Ancestors[0] != c.ID || o.Ancestors[1] != b.ID || o.Ancestors[2] != a.ID {
		t.Fatal("wrong three-level snapshot", o.Ancestors)
	}
	cfg.CommissionBPS = [3]int64{1000, 500, 200}
	must(t, s.SaveSettings(admin.ID, cfg))
	if e = s.SettleOrder(admin.ID, o.ID, "settled", "必须等待真实开奖", 0, false); e == nil {
		t.Fatal("settled without draw")
	}
	must(t, s.SettleOrder(admin.ID, o.ID, "settled", "核对结果，未中奖", 0, true))
	if e = s.SettleOrder(admin.ID, o.ID, "settled", "重复结算", 0, true); e == nil {
		t.Fatal("duplicate settlement accepted")
	}
	for i, tc := range []struct {
		u      User
		amount int64
	}{{c, 100}, {b, 40}, {a, 20}, {root, 0}} {
		data, e := s.Account(tc.u.ID)
		must(t, e)
		if got := data["commission_available_cents"].(int64); got != tc.amount {
			t.Fatalf("level %d commission %d wanted %d", i+1, got, tc.amount)
		}
		if tc.amount > 0 {
			items := data["commissions"].([]Commission)
			if len(items) != 1 || items[0].Level != i+1 {
				t.Fatal("incorrect commission level")
			}
			amount, e := s.Claim(tc.u.ID)
			must(t, e)
			if amount != tc.amount {
				t.Fatal("claim mismatch")
			}
			if _, e = s.Claim(tc.u.ID); e == nil {
				t.Fatal("duplicate claim accepted")
			}
		}
	}
	account, e := s.Account(a.ID)
	must(t, e)
	if account["team_counts"].([3]int) != [3]int{1, 1, 1} {
		t.Fatal("three-level team counts wrong")
	}
	in = input(now)
	o, e = s.PlaceOrder(d.ID, in, now)
	must(t, e)
	must(t, s.SettleOrder(admin.ID, o.ID, "refunded", "撤销退回", 0, false))
	after, e := s.Account(c.ID)
	must(t, e)
	if len(after["commissions"].([]Commission)) != 1 {
		t.Fatal("refund generated commission")
	}
}
func TestConcurrentWalletAndOrderIdempotency(t *testing.T) {
	s, admin := setup(t)
	u := register(t, s, "wallet_user", "")
	must(t, s.Adjust(admin.ID, u.ID, 10000, "初始调账", randomID()))
	now := time.Date(2026, 10, 11, 18, 20, 10, 0, model.Beijing)
	in := input(now)
	var wg sync.WaitGroup
	var errs atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.PlaceOrder(u.ID, in, now)
			if e != nil {
				errs.Add(1)
			}
		}()
	}
	wg.Wait()
	if errs.Load() != 0 {
		t.Fatal("identical retries failed")
	}
	data, e := s.Account(u.ID)
	must(t, e)
	if data["user"].(User).Balance != 8000 || len(data["orders"].([]Order)) != 1 {
		t.Fatal("duplicate debit")
	}
	changed := in
	changed.Multiplier = 2
	if _, e = s.PlaceOrder(u.ID, changed, now); e == nil {
		t.Fatal("reused key with altered payload")
	}
	req, e := s.CreateRequest(u.ID, RequestInput{Kind: "withdraw", Amount: 5000, Note: "测试提现账户", Key: randomID()})
	must(t, e)
	data, e = s.Account(u.ID)
	must(t, e)
	current := data["user"].(User)
	if current.Balance != 3000 || current.Frozen != 5000 {
		t.Fatal("withdrawal reservation failed")
	}
	if _, e = s.CreateRequest(u.ID, RequestInput{Kind: "withdraw", Amount: 4000, Note: "余额不足测试", Key: randomID()}); e == nil {
		t.Fatal("overspent reserved funds")
	}
	var accepted atomic.Int32
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.ReviewRequest(admin.ID, req.ID, "rejected", "申请驳回") == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("withdrawal reviewed repeatedly")
	}
	data, e = s.Account(u.ID)
	must(t, e)
	current = data["user"].(User)
	if current.Balance != 8000 || current.Frozen != 0 {
		t.Fatal("refund applied incorrectly")
	}
	req, e = s.CreateRequest(u.ID, RequestInput{Kind: "deposit", Amount: 2000, Note: "已核对测试到账", Key: randomID()})
	must(t, e)
	must(t, s.ReviewRequest(admin.ID, req.ID, "approved", "到账已确认"))
	if s.ReviewRequest(admin.ID, req.ID, "approved", "重复审核") == nil {
		t.Fatal("duplicate deposit")
	}
	req, e = s.CreateRequest(u.ID, RequestInput{Kind: "withdraw", Amount: 1000, Note: "测试出款信息", Key: randomID()})
	must(t, e)
	must(t, s.ReviewRequest(admin.ID, req.ID, "approved", "外部出款已核对"))
	data, e = s.Account(u.ID)
	must(t, e)
	current = data["user"].(User)
	if current.Balance != 9000 || current.Frozen != 0 {
		t.Fatal("approved withdrawal double debit or frozen leak", current)
	}
}
func TestOrderRulesAndPermissions(t *testing.T) {
	s, admin := setup(t)
	u := register(t, s, "rules_user", "")
	now := time.Date(2026, 10, 11, 18, 20, 10, 0, model.Beijing)
	in := input(now)
	if _, e := s.PlaceOrder(u.ID, in, now); e == nil {
		t.Fatal("zero balance order accepted")
	}
	must(t, s.Adjust(admin.ID, u.ID, 10000, "测试入账", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	if e = s.SaveSettings(u.ID, cfg); e != ErrForbidden {
		t.Fatal("user edited settings")
	}
	if e = s.Adjust(u.ID, u.ID, 1000, "提权测试", randomID()); e != ErrForbidden {
		t.Fatal("user adjusted balance")
	}
	for _, bad := range []OrderInput{{SessionID: "ssc-10", Numbers: []int{1, 1, 2, 3, 4}}, {SessionID: "pk10-10", Numbers: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}}, {SessionID: "lhc-daily", Numbers: []int{1, 2, 3, 4, 5, 50}}, {SessionID: "k3-1", Numbers: []int{0, 2, 3}}, {SessionID: "pc28-5", Numbers: []int{28}}} {
		bad.Issue = in.Issue
		bad.Key = randomID()
		bad.Multiplier = 1
		if _, e = s.PlaceOrder(u.ID, bad, now); e == nil {
			t.Fatal("invalid numbers accepted", bad)
		}
	}
	if _, e = s.PlaceOrder(u.ID, in, time.Date(2026, 10, 11, 18, 20, 59, 0, model.Beijing)); e == nil {
		t.Fatal("sealed issue accepted")
	}
	in.Issue = "200001010001"
	if _, e = s.PlaceOrder(u.ID, in, now); e == nil {
		t.Fatal("stale issue accepted")
	}
	priceMismatch := input(now)
	priceMismatch.UnitCents = 100
	if _, e = s.PlaceOrder(u.ID, priceMismatch, now); e == nil {
		t.Fatal("changed price accepted without confirmation")
	}
	cfg.Plays[1].Enabled = false
	must(t, s.SaveSettings(admin.ID, cfg))
	if _, e = s.PlaceOrder(u.ID, input(now), now); e == nil {
		t.Fatal("disabled play accepted")
	}
	cfg.CommissionBPS = [3]int64{9000, 2000, 1}
	if e = s.SaveSettings(admin.ID, cfg); e == nil {
		t.Fatal("invalid total commission")
	}
	_, token, e := s.Login(u.Username, testPassword)
	must(t, e)
	must(t, s.SetStatus(admin.ID, u.ID, "disabled"))
	if _, e = s.Authenticate(token); e == nil {
		t.Fatal("disabled session still valid")
	}
	must(t, s.SetStatus(admin.ID, u.ID, "active"))
	if _, e = s.Authenticate(token); e == nil {
		t.Fatal("old session revived")
	}
	_, token, e = s.Login(u.Username, testPassword)
	must(t, e)
	must(t, s.ChangePassword(u.ID, testPassword, "new-password-12345"))
	if _, e = s.Authenticate(token); e == nil {
		t.Fatal("password change did not revoke sessions")
	}
}
func TestHTTPAuthOriginAndDataIsolation(t *testing.T) {
	s, admin := setup(t)
	u, _ := loginSecurityRegister(t, s, "httpuser", securityTestQuestion, securityTestAnswer)
	other := register(t, s, "other_user", "")
	_, token, e := s.Login(u.Username, testPassword)
	must(t, e)
	_, adminToken, e := s.Login(admin.Username, testPassword)
	must(t, e)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, token, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:5173"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, token, origin, body string
		code                              int
	}{{"GET", "/api/v1/markets/k3-1", "", "", "", 200}, {"PUT", "/api/v1/admin/markets/k3-1", token, "", "{}", 403}, {"POST", "/api/v1/market-orders", "", "", "{}", 401}, {"GET", "/api/v1/account", "", "", "", 401}, {"GET", "/api/v1/admin/users", token, "", "", 403}, {"POST", "/api/v1/commissions/claim", token, "https://attacker.test", "{}", 403}, {"POST", "/api/v1/auth/register", "", "", `{"username":"new_user","password":"test-password-12345","role":"admin"}`, 400}, {"GET", "/api/v1/admin/users", adminToken, "", "", 200}, {"GET", "/api/v1/account", token, "", "", 200}} {
		w := call(tc.method, tc.path, tc.token, tc.origin, tc.body)
		if w.Code != tc.code {
			t.Fatalf("%s got %d %s", tc.path, w.Code, w.Body.String())
		}
		if bytes.Contains(w.Body.Bytes(), []byte("PasswordSalt")) || bytes.Contains(w.Body.Bytes(), []byte(`"Hash"`)) {
			t.Fatal("password data leaked")
		}
	}

	book, e := s.MarketBook("k3-1")
	must(t, e)
	quotesBody, e := json.Marshal(map[string]any{"quotes": book.Quotes})
	must(t, e)
	saved := call("PUT", "/api/v1/admin/markets/k3-1", adminToken, "http://localhost:5173", string(quotesBody))
	if saved.Code != 200 {
		t.Fatal("admin odds save failed", saved.Body.String())
	}
	if call("PUT", "/api/v1/admin/markets/k3-1", adminToken, "https://attacker.test", string(quotesBody)).Code != 403 {
		t.Fatal("cross-origin odds save allowed")
	}
	w := call("GET", "/api/v1/account", token, "", "")
	if strings.Contains(w.Body.String(), other.ID) {
		t.Fatal("unrelated account leaked")
	}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, u.Username, testPassword)
	w = call("POST", "/api/v1/auth/login", "", "http://localhost:5173", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	formal := loginSecurityCookie(cookies, "starlight_session")
	if formal == nil || formal.Value == "" || !formal.HttpOnly || formal.SameSite != http.SameSiteStrictMode || formal.Path != "/" {
		t.Fatal("direct login did not issue protected session")
	}
	var raw map[string]any
	must(t, json.Unmarshal(w.Body.Bytes(), &raw))
	if raw["data"].(map[string]any)["id"] != u.ID {
		t.Fatal("direct login did not return authenticated user")
	}
	validRegistration, e := json.Marshal(secureRegistrationFields(t, s, map[string]string{"username": "httpnewuser", "password": testPassword, "invite_code": u.InviteCode, "phone": "13800138000"}))
	must(t, e)
	registered := call("POST", "/api/v1/auth/register", "", "http://localhost:5173", string(validRegistration))
	if registered.Code != 200 || !strings.Contains(registered.Body.String(), u.ID) {
		t.Fatal("registration failed to bind inviter", registered.Body.String())
	}
	if session := loginSecurityCookie(registered.Result().Cookies(), "starlight_session"); session == nil || session.Value == "" || session.MaxAge <= 0 {
		t.Fatal("registration did not issue session")
	}
	for i := 0; i < 16; i++ {
		w = call("POST", "/api/v1/auth/login", "", "", `{"username":"no_user","password":"bad"}`)
	}
	if w.Code != 429 {
		t.Fatal("login not throttled")
	}
}
func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	_, e = s.Bootstrap("admin", testPassword)
	must(t, e)
	a, _, e := s.Login("admin", testPassword)
	must(t, e)
	u := register(t, s, "persist_user", "")
	must(t, s.Adjust(a.ID, u.ID, 12345, "持久化测试", randomID()))
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	defer s.Close()
	created, e := s.Bootstrap("another_admin", testPassword)
	must(t, e)
	if created {
		t.Fatal("restart created another administrator")
	}
	d, e := s.Account(u.ID)
	must(t, e)
	if d["user"].(User).Balance != 12345 {
		t.Fatal("balance lost on restart")
	}
}
