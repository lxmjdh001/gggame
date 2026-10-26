package accounts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

const secondaryTestLogin = "Test1234"
const secondaryTestSecret = "abc123"

func secondaryFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	s, path, _ := fundsFixture(t)
	t.Cleanup(func() { s.Close() })
	salt := randomID()
	encoded, err := hash(secondaryTestLogin, salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "member")
		if err != nil {
			return err
		}
		u.Hash, u.PasswordSalt = encoded, salt
		u.WithdrawalHash, u.WithdrawalSalt = "unchanged-withdrawal-hash", "unchanged-withdrawal-salt"
		u.WithdrawalPasswordSet = true
		return put(tx, "users", u.ID, u)
	}))
	_, token, err := s.Login("member", secondaryTestLogin)
	must(t, err)
	return s, path, token
}

func enableSecondary(t *testing.T, s *Store, token string) {
	t.Helper()
	state, err := s.SetSecondaryPassword(token, SecondaryPasswordInput{LoginPassword: secondaryTestLogin, NewPassword: secondaryTestSecret, Confirmation: secondaryTestSecret})
	must(t, err)
	if !state.IsSet || !state.Enabled || !state.VerificationRequired || state.VerifiedUntil != nil {
		t.Fatal("setup must enable but not grant game access", state)
	}
}

func secondaryAccessAt(s *Store, id, token string, now time.Time) error {
	return s.db.View(func(tx *bolt.Tx) error { return requireGameAccess(tx, id, token, now) })
}

func TestSecondaryPasswordLengthAndNoCaseCompositionRequirement(t *testing.T) {
	for _, v := range []struct {
		value string
		valid bool
	}{{"", false}, {"1", true}, {"a", true}, {"A", true}, {" ", true}, {"12345678", true}, {"abcdefgh", true}, {"ABCDEFGH", true}, {"123456789", false}, {"abcdefghijkl", false}, {"1234567890123", false}, {"密码六位测试", true}, {strings.Repeat("😀", 8), true}, {strings.Repeat("😀", 9), false}, {string([]byte{0xff}), false}} {
		if secondaryPasswordValid(v.value) != v.valid {
			t.Fatalf("wrong secondary length policy %q", v.value)
		}
	}
	for _, password := range []string{"", "a", strings.Repeat("a", 8), strings.Repeat("a", 9), strings.Repeat("a", 10), strings.Repeat("a", 11), strings.Repeat("a", 12), strings.Repeat("a", 13), strings.Repeat("😀", 12), strings.Repeat("😀", 13), string([]byte{0xff})} {
		length := len([]rune(password))
		want := password != string([]byte{0xff}) && length >= 1 && length <= 12
		if secondarySecretValid(password) != want {
			t.Fatalf("wrong existing secret policy %q", password)
		}
	}
	s, _, token := secondaryFixture(t)
	beforeSecondary := ""
	for _, password := range []string{"1", "abcdefgh", "ABCDEFGH", "密码六位测试", strings.Repeat("😀", 8), " "} {
		in := SecondaryPasswordInput{LoginPassword: secondaryTestLogin, CurrentPassword: beforeSecondary, NewPassword: password, Confirmation: password}
		_, err := s.SetSecondaryPassword(token, in)
		must(t, err)
		_, err = s.VerifySecondaryPassword(token, password)
		must(t, err)
		beforeSecondary = password
	}
}

// Seed a legacy hash directly: current setup must never create a 9–12 rune secret.
func secondaryLegacyHashFixture(t *testing.T, s *Store, secret string) {
	t.Helper()
	salt := "legacy-secondary-salt"
	encoded, err := hash(secret, salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "member")
		if err != nil {
			return err
		}
		u.SecondaryHash, u.SecondarySalt, u.SecondaryRevision = encoded, salt, "legacy-secondary-revision"
		u.SecondaryPasswordSet, u.SecondaryPasswordEnabled = true, true
		return put(tx, "users", u.ID, u)
	}))
}

func TestSecondaryPasswordLegacyHashesVerifyToggleAndChangeShorter(t *testing.T) {
	for _, secret := range []string{"Abcdefghi", "Abcdefghij", "Abcdefghijk", "Abcdefghijkl", strings.Repeat("😀", 12)} {
		t.Run(secret, func(t *testing.T) {
			s, _, token := secondaryFixture(t)
			secondaryLegacyHashFixture(t, s, secret)
			before := managementStored(t, s, "member")
			wrongSecrets := []string{string([]rune(secret)[:8])}
			if lower := strings.ToLower(secret); lower != secret {
				wrongSecrets = append(wrongSecrets, lower)
			}
			for _, wrong := range wrongSecrets {
				if _, err := s.VerifySecondaryPassword(token, wrong); err == nil {
					t.Fatal("legacy secret truncated or compared incorrectly")
				}
			}
			state, err := s.VerifySecondaryPassword(token, secret)
			must(t, err)
			if state.VerificationRequired || !reflect.DeepEqual(before, managementStored(t, s, "member")) {
				t.Fatal("legacy verification changed stored credentials or did not grant access")
			}
			for _, enabled := range []bool{false, true} {
				state, err = s.ToggleSecondaryPassword(token, secret, enabled)
				must(t, err)
				after := managementStored(t, s, "member")
				if state.Enabled != enabled || state.VerificationRequired != enabled || after.SecondaryHash != before.SecondaryHash || after.SecondarySalt != before.SecondarySalt {
					t.Fatal("legacy toggle changed the secret or retained an old grant")
				}
			}
			before = managementStored(t, s, "member")
			for _, invalid := range []string{"", "123456789", "123456789012", strings.Repeat("😀", 9)} {
				if _, err = s.SetSecondaryPassword(token, SecondaryPasswordInput{CurrentPassword: secret, NewPassword: invalid, Confirmation: invalid}); err == nil {
					t.Fatal("accepted invalid replacement", invalid)
				}
				if !reflect.DeepEqual(before, managementStored(t, s, "member")) {
					t.Fatal("rejected replacement changed legacy account")
				}
			}
			state, err = s.SetSecondaryPassword(token, SecondaryPasswordInput{CurrentPassword: secret, NewPassword: "A", Confirmation: "A"})
			must(t, err)
			if !state.VerificationRequired {
				t.Fatal("replacement reused prior grant")
			}
			for _, wrong := range []string{secret, "a"} {
				if _, err = s.VerifySecondaryPassword(token, wrong); err == nil {
					t.Fatal("old or case-mismatched secret accepted")
				}
			}
			_, err = s.VerifySecondaryPassword(token, "A")
			must(t, err)
		})
	}
}

func TestSecondaryPasswordSetupToggleAndDataIsolation(t *testing.T) {
	s, _, token := secondaryFixture(t)
	before := managementStored(t, s, "member")
	financial := managementFinancialSnapshot(t, s)
	state, err := s.SecondaryPasswordStatus(token)
	must(t, err)
	if state.IsSet || state.Enabled || state.VerificationRequired {
		t.Fatal("legacy accounts should default off")
	}
	for _, in := range []SecondaryPasswordInput{
		{LoginPassword: "wrong", NewPassword: "123456", Confirmation: "123456"},
		{LoginPassword: secondaryTestLogin, NewPassword: "", Confirmation: ""},
		{LoginPassword: secondaryTestLogin, NewPassword: "123456789", Confirmation: "123456789"},
		{LoginPassword: secondaryTestLogin, NewPassword: "123456789012", Confirmation: "123456789012"},
		{LoginPassword: secondaryTestLogin, NewPassword: "123456", Confirmation: "654321"},
	} {
		if _, err = s.SetSecondaryPassword(token, in); err == nil {
			t.Fatal("invalid setup accepted")
		}
		if managementStored(t, s, "member").SecondaryPasswordSet {
			t.Fatal("invalid setup wrote credentials")
		}
	}
	enableSecondary(t, s, token)
	after := managementStored(t, s, "member")
	if after.Hash != before.Hash || after.PasswordSalt != before.PasswordSalt || after.WithdrawalHash != before.WithdrawalHash || after.WithdrawalSalt != before.WithdrawalSalt || after.Balance != before.Balance || after.Frozen != before.Frozen {
		t.Fatal("secondary setup changed primary credentials or funds")
	}
	if after.SecondaryHash == secondaryTestSecret || after.SecondarySalt == "" {
		t.Fatal("secondary password not securely hashed")
	}
	if _, err = s.ToggleSecondaryPassword(token, "wrong1", false); err == nil {
		t.Fatal("disabled without secondary password")
	}
	_, err = s.VerifySecondaryPassword(token, secondaryTestSecret)
	must(t, err)
	state, err = s.ToggleSecondaryPassword(token, secondaryTestSecret, false)
	must(t, err)
	if state.Enabled || state.VerificationRequired || !state.IsSet {
		t.Fatal("disable did not preserve password", state)
	}
	_, err = s.SetSecondaryPassword(token, SecondaryPasswordInput{LoginPassword: secondaryTestLogin, CurrentPassword: secondaryTestSecret, NewPassword: "12345678", Confirmation: "12345678"})
	must(t, err)
	state, err = s.SecondaryPasswordStatus(token)
	must(t, err)
	if state.Enabled {
		t.Fatal("changing a disabled password unexpectedly enabled it")
	}
	state, err = s.ToggleSecondaryPassword(token, "12345678", true)
	must(t, err)
	if !state.VerificationRequired {
		t.Fatal("re-enable reused previous verification")
	}
	if financial != managementFinancialSnapshot(t, s) {
		t.Fatal("secondary settings changed financial state")
	}
	if _, err = s.Authenticate(token); err != nil {
		t.Fatal("secondary settings revoked primary login", err)
	}
}

func TestSecondaryPasswordHourlyMidnightAndLoginBinding(t *testing.T) {
	s, _, token := secondaryFixture(t)
	enableSecondary(t, s, token)
	today := time.Now().In(model.Beijing)
	base := time.Date(today.Year(), today.Month(), today.Day(), 10, 10, 0, 0, model.Beijing)
	state, err := s.verifySecondaryPasswordAt(token, secondaryTestSecret, base)
	must(t, err)
	if state.VerificationRequired || state.VerifiedUntil == nil || !state.VerifiedUntil.Equal(base.Add(time.Hour)) {
		t.Fatal("hourly grant not fixed", state)
	}
	for _, v := range []struct {
		at      time.Time
		allowed bool
	}{{base, true}, {base.Add(59*time.Minute + 59*time.Second), true}, {base.Add(time.Hour), false}, {base.Add(-time.Second), false}} {
		err = secondaryAccessAt(s, "member", token, v.at)
		if (err == nil) != v.allowed {
			t.Fatal("grant boundary", v.at, err)
		}
	}
	// Reading/entering another game does not slide the expiry.
	must(t, secondaryAccessAt(s, "member", token, base.Add(45*time.Minute)))
	if !errors.Is(secondaryAccessAt(s, "member", token, base.Add(time.Hour)), ErrSecondaryRequired) {
		t.Fatal("entry extended the hour")
	}
	late := time.Date(today.Year(), today.Month(), today.Day(), 23, 40, 0, 0, model.Beijing)
	state, err = s.verifySecondaryPasswordAt(token, secondaryTestSecret, late)
	must(t, err)
	midnight := time.Date(today.Year(), today.Month(), today.Day()+1, 0, 0, 0, 0, model.Beijing)
	if state.VerifiedUntil == nil || !state.VerifiedUntil.Equal(midnight) {
		t.Fatal("verification did not stop at Beijing midnight")
	}
	must(t, secondaryAccessAt(s, "member", token, midnight.Add(-time.Nanosecond)))
	if !errors.Is(secondaryAccessAt(s, "member", token, midnight), ErrSecondaryRequired) {
		t.Fatal("cross-day grant accepted")
	}
	_, other, err := s.Login("member", secondaryTestLogin)
	must(t, err)
	if !errors.Is(secondaryAccessAt(s, "member", other, late), ErrSecondaryRequired) {
		t.Fatal("another login inherited grant")
	}
	must(t, s.Logout(token))
	if _, err = s.VerifySecondaryPassword(token, secondaryTestSecret); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logged-out token verified", err)
	}
	if !errors.Is(secondaryAccessAt(s, "member", other, late), ErrSecondaryRequired) {
		t.Fatal("relogin inherited logged-out verification")
	}
}

func TestSecondaryPasswordChangeInvalidatesAllGrantsAndIsCaseSensitive(t *testing.T) {
	s, _, token := secondaryFixture(t)
	enableSecondary(t, s, token)
	_, second, err := s.Login("member", secondaryTestLogin)
	must(t, err)
	_, err = s.VerifySecondaryPassword(token, secondaryTestSecret)
	must(t, err)
	_, err = s.VerifySecondaryPassword(second, secondaryTestSecret)
	must(t, err)
	_, err = s.SetSecondaryPassword(token, SecondaryPasswordInput{CurrentPassword: secondaryTestSecret, NewPassword: "ABCDEF", Confirmation: "ABCDEF"})
	must(t, err)
	for _, login := range []string{token, second} {
		state, e := s.SecondaryPasswordStatus(login)
		must(t, e)
		if !state.VerificationRequired {
			t.Fatal("old grant survived password change")
		}
	}
	if _, err = s.VerifySecondaryPassword(token, "abcdef"); err == nil {
		t.Fatal("case insensitive password comparison")
	}
	state, err := s.VerifySecondaryPassword(token, "ABCDEF")
	must(t, err)
	if state.VerificationRequired {
		t.Fatal("correct uppercase-only password rejected")
	}
}

func TestSecondaryPasswordForeignExpiredAndRevokedLoginCannotAuthorize(t *testing.T) {
	s, _, token := secondaryFixture(t)
	enableSecondary(t, s, token)
	now := time.Now()
	_, err := s.verifySecondaryPasswordAt(token, secondaryTestSecret, now)
	must(t, err)
	var other string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var e error; other, e = newLogin(tx, "admin"); return e }))
	if !errors.Is(secondaryAccessAt(s, "member", other, now), ErrSecondaryRequired) {
		t.Fatal("another account's valid login authorized member")
	}
	if _, err = s.VerifySecondaryPassword(token, "wrong1"); err == nil {
		t.Fatal("wrong verification accepted")
	}
	if !errors.Is(secondaryAccessAt(s, "member", token, now.Add(time.Hour)), ErrSecondaryRequired) {
		t.Fatal("failed verification extended grant")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		l, e := get[Login](tx, "logins", tokenKey(token))
		if e != nil {
			return e
		}
		l.Expires = now.Add(time.Minute)
		return put(tx, "logins", tokenKey(token), l)
	}))
	l, u, err := s.secondaryState(token, now)
	must(t, err)
	status := secondaryStatus(u, l, now)
	if status.VerifiedUntil == nil || !status.VerifiedUntil.Equal(l.Expires) {
		t.Fatal("grant exceeded login lifetime")
	}
	if !errors.Is(secondaryAccessAt(s, "member", token, l.Expires), ErrSecondaryRequired) {
		t.Fatal("expired login authorized game")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error { return revoke(tx, "member") }))
	if !errors.Is(secondaryAccessAt(s, "member", token, now), ErrSecondaryRequired) {
		t.Fatal("revoked login authorized game")
	}
}

func TestSecondaryPasswordRateLimitPersistsAndIsShared(t *testing.T) {
	s, path, token := secondaryFixture(t)
	enableSecondary(t, s, token)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.VerifySecondaryPassword(token, "wrong1"); err == nil {
				t.Error("wrong password accepted")
			}
		}()
	}
	wg.Wait()
	must(t, s.Close())
	s, err := Open(path)
	must(t, err)
	defer s.Close()
	if _, err = s.VerifySecondaryPassword(token, secondaryTestSecret); !errors.Is(err, errSecondaryLimited) {
		t.Fatal("rate limit lost on restart", err)
	}
	if _, err = s.ToggleSecondaryPassword(token, secondaryTestSecret, false); !errors.Is(err, errSecondaryLimited) {
		t.Fatal("settings bypassed shared rate limit", err)
	}
	state, err := s.SecondaryPasswordStatus(token)
	must(t, err)
	if !state.VerificationRequired {
		t.Fatal("failed attempts granted access")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "auth_security_attempts", "secondary_password:member", securityAttempt{Count: 5, Reset: time.Now().Add(-time.Second)})
	}))
	_, err = s.VerifySecondaryPassword(token, secondaryTestSecret)
	must(t, err)
}

func TestSecondaryPasswordAllOrderPathsAndIdempotentRetry(t *testing.T) {
	s, _, token := secondaryFixture(t)
	enableSecondary(t, s, token)
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	financial := managementFinancialSnapshot(t, s)
	market := marketInput(now, "p1.big")
	market.gameToken = token
	batch := batchInput(now)
	batch.gameToken = token
	g, _ := games.Find("ffc-1")
	classic := OrderInput{gameToken: token, SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), Numbers: []int{1, 2, 3, 4, 5}, UnitCents: 200, Multiplier: 1, Key: randomID()}
	if _, err := s.PlaceOrder("member", classic, now); !errors.Is(err, ErrSecondaryRequired) {
		t.Fatal("classic bypass", err)
	}
	if _, err := s.PlaceMarketOrder("member", market, now); !errors.Is(err, ErrSecondaryRequired) {
		t.Fatal("single bypass", err)
	}
	if _, err := s.PlaceMarketBatch("member", batch, now); !errors.Is(err, ErrSecondaryRequired) {
		t.Fatal("batch bypass", err)
	}
	if financial != managementFinancialSnapshot(t, s) {
		t.Fatal("rejected orders changed money/idempotency")
	}
	_, err := s.verifySecondaryPasswordAt(token, secondaryTestSecret, now)
	must(t, err)
	first, err := s.PlaceMarketOrder("member", market, now)
	must(t, err)
	repeated, err := s.PlaceMarketOrder("member", market, now)
	must(t, err)
	if first.ID != repeated.ID {
		t.Fatal("retry duplicated order")
	}
	_, err = s.PlaceMarketBatch("member", batch, now)
	must(t, err)
	_, err = s.PlaceOrder("member", classic, now)
	must(t, err)
	financial = managementFinancialSnapshot(t, s)
	if _, err = s.PlaceMarketOrder("member", market, now.Add(time.Hour)); !errors.Is(err, ErrSecondaryRequired) {
		t.Fatal("expired grant accepted retry", err)
	}
	if financial != managementFinancialSnapshot(t, s) {
		t.Fatal("expired retry mutated data")
	}
	missing := market
	missing.gameToken = ""
	missing.Key = randomID()
	if _, err = s.PlaceMarketOrder("member", missing, now); !errors.Is(err, ErrSecondaryRequired) {
		t.Fatal("direct store call bypass", err)
	}
}

func TestSecondaryPasswordHTTPContractsNoLeaksAndCSRF(t *testing.T) {
	s, _, token := secondaryFixture(t)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, body, cookie, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v1/auth/secondary-password/status", "/api/v1/auth/secondary-password", "/api/v1/auth/secondary-password/verify", "/api/v1/auth/secondary-password/toggle"} {
		method := "POST"
		if strings.HasSuffix(path, "/status") {
			method = "GET"
		}
		if w := call(method, path, "{}", "", ""); w.Code != 401 {
			t.Fatal("unauthenticated secondary route", path, w.Code)
		}
	}
	body := `{"login_password":"Test1234","new_password":"abc123","confirm_password":"abc123"}`
	if w := call("POST", "/api/v1/auth/secondary-password", body, token, "https://other.invalid"); w.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	if w := call("POST", "/api/v1/auth/secondary-password", body, token, ""); w.Code != 200 {
		t.Fatal("setup HTTP", w.Body.String())
	}
	for _, path := range []string{"/api/v1/orders", "/api/v1/market-orders", "/api/v1/market-orders/batch"} {
		w := call("POST", path, `{"session_id":"ffc-1"}`, token, "")
		if w.Code != 403 || !strings.Contains(w.Body.String(), "SECONDARY_PASSWORD_REQUIRED") {
			t.Fatal("HTTP order bypass", path, w.Code, w.Body.String())
		}
	}
	if w := call("POST", "/api/v1/auth/secondary-password/toggle", `{"password":"abc123"}`, token, ""); w.Code != 400 {
		t.Fatal("missing enabled silently disabled")
	}
	if w := call("POST", "/api/v1/auth/secondary-password/verify", `{"password":"abc123"}`, token, ""); w.Code != 200 {
		t.Fatal("verify HTTP", w.Body.String())
	}
	before := managementStored(t, s, "member")
	for _, path := range []string{"/api/v1/auth/secondary-password/status", "/api/v1/auth/me", "/api/v1/account"} {
		w := call("GET", path, "", token, "")
		for _, secret := range []string{secondaryTestSecret, before.SecondaryHash, before.SecondarySalt, before.SecondaryRevision, "SecondaryHash", "SecondarySalt", "SecondaryVerifiedAt"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("secondary credential leaked", path)
			}
		}
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("status contract", path, w.Code)
		}
	}
	// Cookie binding cannot be replaced by a caller-provided gameToken field.
	if w := call("POST", "/api/v1/market-orders", `{"gameToken":"forged"}`, token, ""); w.Code != 400 {
		t.Fatal("client supplied internal token")
	}
	encoded, err := json.Marshal(MarketOrderInput{gameToken: "secret-cookie"})
	must(t, err)
	if strings.Contains(string(encoded), "secret-cookie") {
		t.Fatal("session leaked into idempotency JSON")
	}
	after := managementStored(t, s, "member")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only checks changed user")
	}
}
