package accounts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

const securityTestPIN = "246810"
const securityTestCaptcha = "A7b9C"
const securityTestQuestion = "first_school"
const securityTestAnswer = "Primary School"

func seedSecurityCaptcha(t *testing.T, s *Store, answer string, expires time.Time) string {
	t.Helper()
	id := randomID()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("auth_captchas")); err != nil {
			return err
		}
		return put(tx, "auth_captchas", id, captchaRecord{Digest: captchaDigest(id, answer), ExpiresAt: expires})
	}))
	return id
}

// Existing HTTP tests still exercise their original registration rules, with
// fresh security fields so they cannot pass solely because a CAPTCHA is absent.
func secureRegistrationFields(t *testing.T, s *Store, fields map[string]string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(fields)+6)
	for k, v := range fields {
		out[k] = v
	}
	if _, ok := out["name"]; !ok {
		out["name"] = "张三"
	}
	if _, ok := out["invite_code"]; !ok {
		out["invite_code"] = registrationTestInvite(t, s)
	}
	out["wechat"] = "test_wechat"
	out["withdrawal_password"] = securityTestPIN
	out["captcha_id"] = seedSecurityCaptcha(t, s, securityTestCaptcha, time.Now().Add(5*time.Minute))
	out["captcha_answer"] = securityTestCaptcha
	return out
}

func registrationTestInvite(t *testing.T, s *Store) string {
	t.Helper()
	var code string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		users, err := list[storedUser](tx, "users")
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Role != "admin" {
				continue
			}
			if u.InviteCode == "" {
				u.InviteCode, err = newInviteCode(tx)
				if err != nil {
					return err
				}
				if err = put(tx, "users", u.ID, u); err != nil {
					return err
				}
				if err = tx.Bucket([]byte("invites")).Put([]byte(u.InviteCode), []byte(u.ID)); err != nil {
					return err
				}
			}
			code = u.InviteCode
			break
		}
		return nil
	}))
	if code == "" {
		t.Fatal("missing test administrator invite")
	}
	return code
}

func secureRegistrationInput(t *testing.T, s *Store, username string) SecureRegistrationInput {
	t.Helper()
	return SecureRegistrationInput{
		Username: username, Password: testPassword, InviteCode: registrationTestInvite(t, s), Phone: "13800138000", Name: "张三", Wechat: "wechat_secure",
		WithdrawalPassword: securityTestPIN,
		CaptchaID:          seedSecurityCaptcha(t, s, securityTestCaptcha, time.Now().Add(5*time.Minute)), CaptchaAnswer: securityTestCaptcha,
	}
}

func registrationCounts(t *testing.T, s *Store) map[string]int {
	t.Helper()
	counts := map[string]int{}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, name := range []string{"users", "usernames", "invites", "logins", "ledger"} {
			counts[name] = tx.Bucket([]byte(name)).Stats().KeyN
		}
		return nil
	}))
	return counts
}

func assertRegistrationUnchanged(t *testing.T, s *Store, before map[string]int) {
	t.Helper()
	if after := registrationCounts(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed registration changed persistent records: before=%v after=%v", before, after)
	}
}

func storedSecurityUser(t *testing.T, s *Store, id string) storedUser {
	t.Helper()
	var u storedUser
	must(t, s.db.View(func(tx *bolt.Tx) error {
		var err error
		u, err = get[storedUser](tx, "users", id)
		return err
	}))
	return u
}

func assertNoSecuritySecrets(t *testing.T, value any, secrets ...string) {
	t.Helper()
	raw, err := json.Marshal(value)
	must(t, err)
	for _, key := range []string{"Hash", "PasswordSalt", "WithdrawalHash", "WithdrawalSalt", "SecurityHash", "SecuritySalt", "password", "withdrawal_password", "captcha_answer", "security_answer", "answer", "answer_confirmation", "hash", "password_salt", "withdrawal_hash", "withdrawal_salt", "security_hash", "security_salt"} {
		if bytes.Contains(raw, []byte(`"`+key+`":`)) {
			t.Fatalf("public JSON exposed secret field %q", key)
		}
	}
	for _, secret := range secrets {
		if secret != "" && bytes.Contains(raw, []byte(`"`+secret+`"`)) {
			t.Fatal("public JSON exposed a credential value")
		}
	}
}

func TestAuthCaptchaPNGAndPublicChallenge(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	before := time.Now()
	c, err := s.NewCaptcha()
	must(t, err)
	after := time.Now()
	if c.ID == "" || c.ExpiresAt.Before(before.Add(5*time.Minute-time.Second)) || c.ExpiresAt.After(after.Add(5*time.Minute+time.Second)) {
		t.Fatal("captcha is missing its ID or five-minute expiry")
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(c.ImageDataURL, prefix) {
		t.Fatal("captcha must contain a PNG data URL")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(c.ImageDataURL, prefix))
	must(t, err)
	img, err := png.Decode(bytes.NewReader(data))
	must(t, err)
	if img.Bounds().Dx() < 5 || img.Bounds().Dy() < 5 {
		t.Fatal("captcha image is empty")
	}
	first := img.At(img.Bounds().Min.X, img.Bounds().Min.Y)
	visible := false
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y && !visible; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			fr, fg, fb, fa := first.RGBA()
			if r != fr || g != fg || b != fb || a != fa {
				visible = true
				break
			}
		}
	}
	if !visible {
		t.Fatal("captcha image has no visible content")
	}
	raw, err := json.Marshal(c)
	must(t, err)
	var public map[string]any
	must(t, json.Unmarshal(raw, &public))
	for key := range public {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "answer") || strings.Contains(lower, "digest") || strings.Contains(lower, "solution") {
			t.Fatalf("challenge exposes its answer through %q", key)
		}
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		record, e := get[captchaRecord](tx, "auth_captchas", c.ID)
		if e != nil {
			return e
		}
		if record.Digest == "" || !record.ExpiresAt.Equal(c.ExpiresAt) || bytes.Contains(raw, []byte(record.Digest)) {
			t.Fatal("captcha digest is missing, exposed, or has a different expiry")
		}
		return nil
	}))
	second, err := s.NewCaptcha()
	must(t, err)
	if second.ID == c.ID || second.ImageDataURL == c.ImageDataURL {
		t.Fatal("new captcha reused the previous challenge")
	}
}

func TestAuthCaptchaFailureConsumesChallengeWithoutRegistration(t *testing.T) {
	for _, name := range []string{"wrong", "expired", "missing", "wrong_length", "invalid_pin", "invalid_contact"} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := fundsFixture(t)
			defer s.Close()
			in := secureRegistrationInput(t, s, "captchafailure")
			switch name {
			case "wrong":
				in.CaptchaAnswer = "ZZZZZ"
			case "expired":
				in.CaptchaID = seedSecurityCaptcha(t, s, securityTestCaptcha, time.Now().Add(-time.Second))
			case "missing":
				in.CaptchaID = randomID()
			case "wrong_length":
				in.CaptchaAnswer = "A7b9"
			case "invalid_pin":
				in.WithdrawalPassword = "12345"
			case "invalid_contact":
				in.Phone = "invalid"
			}
			before := registrationCounts(t, s)
			if _, token, err := s.RegisterSecure(in); err == nil || token != "" {
				t.Fatal("invalid secure registration succeeded or issued a login")
			}
			assertRegistrationUnchanged(t, s, before)
			in.CaptchaAnswer, in.WithdrawalPassword, in.Phone = securityTestCaptcha, securityTestPIN, "13800138000"
			if _, token, err := s.RegisterSecure(in); err == nil || token != "" {
				t.Fatal("failed attempt left its captcha reusable")
			}
			assertRegistrationUnchanged(t, s, before)
		})
	}
}

func TestAuthCaptchaConcurrentSingleUseAndRestart(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	in := secureRegistrationInput(t, s, "concurrentcaptcha")
	before := registrationCounts(t, s)
	type result struct {
		user  User
		token string
		err   error
	}
	results := make(chan result, 8)
	for i := 0; i < cap(results); i++ {
		request := in
		request.Username += string(rune('a' + i))
		go func() {
			u, token, err := s.RegisterSecure(request)
			results <- result{u, token, err}
		}()
	}
	successes := 0
	for i := 0; i < cap(results); i++ {
		got := <-results
		if got.err == nil {
			successes++
			if got.user.ID == "" || got.token == "" {
				t.Error("accepted registration is missing its user or session")
			}
		} else if got.token != "" {
			t.Error("rejected concurrent registration issued a session")
		}
	}
	if successes != 1 {
		t.Fatalf("one captcha accepted %d concurrent registrations", successes)
	}
	after := registrationCounts(t, s)
	for _, bucket := range []string{"users", "usernames", "invites", "logins"} {
		if after[bucket] != before[bucket]+1 {
			t.Fatalf("concurrent registration left unexpected %s records", bucket)
		}
	}
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	if _, token, err := s.RegisterSecure(in); err == nil || token != "" {
		t.Fatal("restart restored a previously consumed captcha")
	}
	assertRegistrationUnchanged(t, s, after)
}

func TestSecureRegistrationPersistsIndependentCredentialsAndConsumesCaptcha(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	cfg, err := s.Settings()
	must(t, err)
	cfg.RegistrationBonusCents = 12300
	must(t, s.SaveSettings("admin", cfg))
	in := secureRegistrationInput(t, s, "secureregistered")
	in.CaptchaAnswer = strings.ToLower(in.CaptchaAnswer)
	u, token, err := s.RegisterSecure(in)
	must(t, err)
	if token == "" || u.Wechat != in.Wechat || !u.WithdrawalPasswordSet || u.Balance != 12300 || u.Phone != in.Phone || u.Name != in.Name {
		t.Fatal("registration did not atomically return its contact, bonus and security state")
	}
	stored := storedSecurityUser(t, s, u.ID)
	if stored.WithdrawalHash == "" || stored.WithdrawalSalt == "" || stored.WithdrawalHash == securityTestPIN || stored.WithdrawalHash == stored.Hash || stored.WithdrawalSalt == stored.PasswordSalt {
		t.Fatal("withdrawal credential is absent, plain text or reuses login credentials")
	}
	if stored.Wechat != in.Wechat || !stored.WithdrawalPasswordSet {
		t.Fatal("registration persisted an incomplete security profile")
	}
	must(t, s.VerifyWithdrawalPassword(u.ID, securityTestPIN))
	if err = s.VerifyWithdrawalPassword(u.ID, testPassword); err == nil {
		t.Fatal("login password accepted as withdrawal PIN")
	}
	before := registrationCounts(t, s)
	in.Username = "captchareplayed"
	if _, retryToken, err := s.RegisterSecure(in); err == nil || retryToken != "" {
		t.Fatal("successful registration did not consume its captcha")
	}
	assertRegistrationUnchanged(t, s, before)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	current, err := s.Authenticate(token)
	must(t, err)
	if current.Wechat != in.Wechat || !current.WithdrawalPasswordSet {
		t.Fatal("security profile did not survive restart")
	}
	must(t, s.VerifyWithdrawalPassword(u.ID, securityTestPIN))
	loggedIn, _, err := s.Login(u.Username, testPassword)
	must(t, err)
	account, err := s.Account(u.ID)
	must(t, err)
	adminData, err := s.AdminData("admin", "users", u.Username, 1)
	must(t, err)
	for _, public := range []any{u, current, loggedIn, account, adminData} {
		assertNoSecuritySecrets(t, public, testPassword, securityTestPIN, stored.Hash, stored.PasswordSalt, stored.WithdrawalHash, stored.WithdrawalSalt)
	}
	if len(account["ledger"].([]Ledger)) != 1 {
		t.Fatal("registration bonus was duplicated")
	}
}

func TestSecureRegistrationRollbackAfterUserCreation(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	// Inject an invalid bonus to fail the ledger write after user and security
	// records are created, proving that none of the partial registration commits.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		cfg, err := get[Settings](tx, "settings", "global")
		if err != nil {
			return err
		}
		cfg.RegistrationBonusCents = maxMoney + 1
		return put(tx, "settings", "global", cfg)
	}))
	in := secureRegistrationInput(t, s, "rolledbacksecure")
	before := registrationCounts(t, s)
	if _, token, err := s.RegisterSecure(in); err == nil || token != "" {
		t.Fatal("registration with a failing bonus ledger succeeded")
	}
	assertRegistrationUnchanged(t, s, before)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		cfg, err := get[Settings](tx, "settings", "global")
		if err != nil {
			return err
		}
		cfg.RegistrationBonusCents = 0
		return put(tx, "settings", "global", cfg)
	}))
	if _, _, err := s.RegisterSecure(in); err == nil {
		t.Fatal("registration rollback restored a consumed captcha")
	}
	assertRegistrationUnchanged(t, s, before)
}

func TestWithdrawalPasswordLegacySetupChangeAndValidation(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	u := register(t, s, "legacy_secure", "")
	for _, pin := range []string{"", "12345", "1234567", "12a456", "１２３４５６", " 246810"} {
		if _, err := s.SetWithdrawalPassword(u.ID, "", pin); err == nil {
			t.Fatal("invalid PIN accepted")
		}
	}
	updated, err := s.SetWithdrawalPassword(u.ID, "", securityTestPIN)
	must(t, err)
	if !updated.WithdrawalPasswordSet {
		t.Fatal("initial setup missing")
	}
	before := storedSecurityUser(t, s, u.ID)
	for _, secret := range []string{"", testPassword, "000000"} {
		if _, err = s.SetWithdrawalPassword(u.ID, secret, "135790"); err == nil {
			t.Fatal("PIN changed without original withdrawal PIN")
		}
	}
	must(t, s.VerifyWithdrawalPassword(u.ID, securityTestPIN))
	_, err = s.SetWithdrawalPassword(u.ID, securityTestPIN, "135790")
	must(t, err)
	after := storedSecurityUser(t, s, u.ID)
	if before.Hash != after.Hash || before.PasswordSalt != after.PasswordSalt || before.WithdrawalHash == after.WithdrawalHash || before.WithdrawalSalt == after.WithdrawalSalt {
		t.Fatal("PIN did not rotate independently")
	}
	if s.VerifyWithdrawalPassword(u.ID, securityTestPIN) == nil {
		t.Fatal("previous PIN remained valid")
	}
	must(t, s.VerifyWithdrawalPassword(u.ID, "135790"))
	_, _, err = s.Login(u.Username, testPassword)
	must(t, err)
}

func TestWithdrawalPasswordSharedAttemptLimitAndRestart(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	u := register(t, s, "pin_attempt_user", "")
	_, err := s.SetWithdrawalPassword(u.ID, "", securityTestPIN)
	must(t, err)
	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			_, err = s.SetWithdrawalPassword(u.ID, "000000", "135790")
		} else {
			err = s.VerifyWithdrawalPassword(u.ID, "000000")
		}
		if err == nil {
			t.Fatal("wrong PIN accepted")
		}
	}
	if s.VerifyWithdrawalPassword(u.ID, securityTestPIN) == nil {
		t.Fatal("withdrawal bypassed shared lock")
	}
	if _, err = s.SetWithdrawalPassword(u.ID, securityTestPIN, "135790"); err == nil {
		t.Fatal("change bypassed shared lock")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	if s.VerifyWithdrawalPassword(u.ID, securityTestPIN) == nil {
		t.Fatal("restart cleared PIN lock")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		v, err := get[securityAttempt](tx, "auth_security_attempts", "withdrawal:"+u.ID)
		if err != nil {
			return err
		}
		v.Reset = time.Now().Add(-time.Second)
		return put(tx, "auth_security_attempts", "withdrawal:"+u.ID, v)
	}))
	_, err = s.SetWithdrawalPassword(u.ID, securityTestPIN, "135790")
	must(t, err)
	must(t, s.VerifyWithdrawalPassword(u.ID, "135790"))
}

func TestWithdrawalPasswordConcurrentFirstSetup(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	u := register(t, s, "pin_concurrent_user", "")
	results := make(chan string, 2)
	for _, pin := range []string{"246810", "135790"} {
		go func(pin string) {
			_, err := s.SetWithdrawalPassword(u.ID, "", pin)
			if err == nil {
				results <- pin
			} else {
				results <- ""
			}
		}(pin)
	}
	successful := ""
	count := 0
	for i := 0; i < 2; i++ {
		if pin := <-results; pin != "" {
			count++
			successful = pin
		}
	}
	if count != 1 {
		t.Fatal("concurrent first setup overwrote a PIN", count)
	}
	must(t, s.VerifyWithdrawalPassword(u.ID, successful))
	if _, err := s.SetWithdrawalPassword(u.ID, "", "123456"); err == nil {
		t.Fatal("existing PIN reset without verification")
	}
}

func TestAuthSecurityHTTPBoundariesAndCaptchaRateLimit(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 16; i++ {
		w := call("GET", "/api/v1/auth/captcha", "", "")
		if i == 15 {
			if w.Code != http.StatusTooManyRequests {
				t.Fatal("captcha endpoint was not rate limited", w.Code)
			}
			continue
		}
		if w.Code != http.StatusOK || len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("public captcha endpoint failed or issued a session", w.Code)
		}
		var response struct {
			Data CaptchaChallenge `json:"data"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &response))
		if response.Data.ID == "" || !strings.HasPrefix(response.Data.ImageDataURL, "data:image/png;base64,") {
			t.Fatal("HTTP captcha response is incomplete")
		}
		var raw map[string]any
		must(t, json.Unmarshal(w.Body.Bytes(), &raw))
		assertNoSecuritySecrets(t, raw)
		for _, key := range []string{`"answer":`, `"digest":`, `"Digest":`} {
			if bytes.Contains(w.Body.Bytes(), []byte(key)) {
				t.Fatal("HTTP captcha leaked its answer or digest")
			}
		}
	}
	before := registrationCounts(t, s)
	missing := call("POST", "/api/v1/auth/register", "", `{"username":"no_captcha_user","password":"Test-password-12345","phone":"13800138000","wechat":"test_wechat","withdrawal_password":"246810"}`)
	if missing.Code != http.StatusBadRequest || len(missing.Result().Cookies()) != 0 || !strings.Contains(missing.Body.String(), "验证码") {
		t.Fatal("HTTP registration bypassed required captcha", missing.Code, missing.Body.String())
	}
	assertRegistrationUnchanged(t, s, before)
	const setupPath = "/api/v1/auth/withdrawal-password"
	const setupBody = `{"withdrawal_password":"246810","withdrawal_password_confirmation":"246810"}`
	for _, token := range []string{"", "invalid-session-token"} {
		if w := call("POST", setupPath, token, setupBody); w.Code != http.StatusUnauthorized {
			t.Fatal("PIN setup did not require an authenticated session", w.Code)
		}
	}
	u := register(t, s, "http_pin_user", "")
	_, token, err := s.Login(u.Username, testPassword)
	must(t, err)
	wrong := call("POST", setupPath, token, `{"withdrawal_password":"246810","withdrawal_password_confirmation":"135790"}`)
	if wrong.Code != http.StatusBadRequest || storedSecurityUser(t, s, u.ID).WithdrawalPasswordSet {
		t.Fatal("HTTP PIN setup skipped confirmation verification")
	}
	updated := call("POST", setupPath, token, setupBody)
	if updated.Code != http.StatusOK || len(updated.Result().Cookies()) != 0 {
		t.Fatal("authenticated PIN setup failed", updated.Code, updated.Body.String())
	}
	var response struct {
		Data User `json:"data"`
	}
	must(t, json.Unmarshal(updated.Body.Bytes(), &response))
	if !response.Data.WithdrawalPasswordSet || response.Data.ID != u.ID {
		t.Fatal("HTTP setup returned the wrong account or PIN-set state")
	}
	stored := storedSecurityUser(t, s, u.ID)
	var raw map[string]any
	must(t, json.Unmarshal(updated.Body.Bytes(), &raw))
	assertNoSecuritySecrets(t, raw, testPassword, securityTestPIN, stored.Hash, stored.PasswordSalt, stored.WithdrawalHash, stored.WithdrawalSalt)
	must(t, s.VerifyWithdrawalPassword(u.ID, securityTestPIN))
}
