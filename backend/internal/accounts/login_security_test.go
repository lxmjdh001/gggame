package accounts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func loginSecurityCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func loginSecurityCount(t *testing.T, s *Store) int {
	t.Helper()
	count := 0
	must(t, s.db.View(func(tx *bolt.Tx) error {
		count = tx.Bucket([]byte("logins")).Stats().KeyN
		return nil
	}))
	return count
}

func loginSecurityRegister(t *testing.T, s *Store, username, question, answer string) (User, string) {
	t.Helper()
	in := secureRegistrationInput(t, s, username)
	u, token, err := s.RegisterSecure(in)
	must(t, err)
	// Seed legacy questions explicitly; new registrations no longer collect them.
	normalized, err := validateSecurityAnswer(question, answer, in.Phone)
	must(t, err)
	salt := randomID()
	encoded, err := hash(normalized, salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		stored, err := get[storedUser](tx, "users", u.ID)
		if err != nil {
			return err
		}
		stored.SecurityQuestionSet = true
		stored.SecurityQuestionID, stored.SecurityHash, stored.SecuritySalt = question, encoded, salt
		u = stored.User
		return put(tx, "users", u.ID, stored)
	}))
	if token == "" {
		t.Fatal("registration did not retain automatic sign-in")
	}
	return u, token
}

func loginSecurityBegin(t *testing.T, s *Store, username string) (LoginSecurityChallenge, string) {
	t.Helper()
	before := loginSecurityCount(t, s)
	started := time.Now()
	challenge, token, err := s.BeginLogin(username, testPassword)
	must(t, err)
	if !challenge.RequiresSecurity || token == "" || challenge.ExpiresAt.Before(started.Add(5*time.Minute-time.Second)) || challenge.ExpiresAt.After(time.Now().Add(5*time.Minute+time.Second)) {
		t.Fatal("password step did not create a five-minute security challenge", challenge)
	}
	if loginSecurityCount(t, s) != before {
		t.Fatal("password step issued a formal login session")
	}
	if _, err = s.Authenticate(token); err == nil {
		t.Fatal("challenge token is also an authenticated session")
	}
	assertNoSecuritySecrets(t, challenge, testPassword, securityTestAnswer, strings.ToLower(securityTestAnswer))
	return challenge, token
}

func loginSecurityReject(t *testing.T, s *Store, token string, in LoginSecurityInput) error {
	t.Helper()
	before := loginSecurityCount(t, s)
	u, session, err := s.CompleteLoginSecurity(token, in)
	if err == nil || session != "" || u.ID != "" || loginSecurityCount(t, s) != before {
		t.Fatal("rejected security response issued a user/session", err, u.ID, session)
	}
	return err
}

func TestRegistrationRequiredFieldsAndCredentialRules(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	for _, tc := range []struct {
		name, message string
		change        func(*SecureRegistrationInput)
	}{
		{"underscore", "用户名", func(in *SecureRegistrationInput) { in.Username = "new_user" }},
		{"punctuation", "用户名", func(in *SecureRegistrationInput) { in.Username = "new-user" }},
		{"unicode_username", "用户名", func(in *SecureRegistrationInput) { in.Username = "新用户abc" }},
		{"short_username", "用户名", func(in *SecureRegistrationInput) { in.Username = "abc" }},
		{"lowercase", "大写和小写", func(in *SecureRegistrationInput) { in.Password = "lowercase12345" }},
		{"uppercase", "大写和小写", func(in *SecureRegistrationInput) { in.Password = "UPPERCASE12345" }},
		{"digits", "大写和小写", func(in *SecureRegistrationInput) { in.Password = "123456789012" }},
		{"short_password", "10–128", func(in *SecureRegistrationInput) { in.Password = "Ab12345" }},
		{"long_password", "10–128", func(in *SecureRegistrationInput) { in.Password = "Ab" + strings.Repeat("1", 127) }},
		{"name", "姓名", func(in *SecureRegistrationInput) { in.Name = " \t " }},
		{"wechat", "微信号", func(in *SecureRegistrationInput) { in.Wechat = " \t " }},
		{"phone", "手机号", func(in *SecureRegistrationInput) { in.Phone = "" }},
		{"invite", "邀请码", func(in *SecureRegistrationInput) { in.InviteCode = " \t " }},
		{"unknown_invite", "邀请码", func(in *SecureRegistrationInput) { in.InviteCode = "INVALIDCODE" }},
		{"pin", "提现密码", func(in *SecureRegistrationInput) { in.WithdrawalPassword = "Ab1234" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := secureRegistrationInput(t, s, "NewMember123")
			tc.change(&in)
			before := registrationCounts(t, s)
			_, token, err := s.RegisterSecure(in)
			if err == nil || token != "" || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %s rejection, got %v", tc.message, err)
			}
			assertRegistrationUnchanged(t, s, before)
			if err = s.consumeCaptcha(in.CaptchaID, in.CaptchaAnswer); err == nil {
				t.Fatal("rejected registration left reusable CAPTCHA")
			}
		})
	}
	in := secureRegistrationInput(t, s, "NewMember123")
	in.Name = " 张三 "
	in.Wechat = " Wechat123 "
	u, token, err := s.RegisterSecure(in)
	must(t, err)
	if u.Name != "张三" || u.Wechat != "Wechat123" || token == "" || u.SecurityQuestionSet {
		t.Fatal("required profile or direct sign-in incorrect")
	}
	stored := storedSecurityUser(t, s, u.ID)
	if stored.SecurityHash != "" || stored.SecuritySalt != "" || stored.SecurityQuestionID != "" {
		t.Fatal("removed security fields still populated")
	}
	_, err = s.Authenticate(token)
	must(t, err)
}

func TestLoginSecurityRegisteredQuestionsHashAnswersAndRequireEveryLogin(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	if _, token, err := s.BeginLogin("missing_account", "incorrect-password"); err == nil || token != "" || loginSecurityCount(t, s) != 0 {
		t.Fatal("invalid password step issued authentication state")
	}
	var firstSalt, firstHash string
	for i, question := range []string{"first_school", "childhood_nickname", "memorable_place", "registered_phone"} {
		answer := "  OrChId ACADEMY  "
		if question == "registered_phone" {
			answer = " 13800138000 "
		}
		u, originalSession := loginSecurityRegister(t, s, fmt.Sprintf("securityquestion%d", i), question, answer)
		stored := storedSecurityUser(t, s, u.ID)
		if !stored.SecurityQuestionSet || stored.SecurityQuestionID != question || stored.SecurityHash == "" || stored.SecuritySalt == "" || stored.SecurityHash == answer || stored.SecuritySalt == stored.PasswordSalt || stored.SecuritySalt == stored.WithdrawalSalt {
			t.Fatal("registration did not store independent private security credentials")
		}
		wantHash, err := hash(strings.ToLower(strings.TrimSpace(answer)), stored.SecuritySalt)
		must(t, err)
		if stored.SecurityHash != wantHash {
			t.Fatal("answer did not use normalized salted password hashing")
		}
		if i == 0 {
			firstSalt, firstHash = stored.SecuritySalt, stored.SecurityHash
		} else if i == 1 && (firstSalt == stored.SecuritySalt || firstHash == stored.SecurityHash) {
			t.Fatal("two users sharing an answer reused a salt or hash")
		}
		challenge, pending := loginSecurityBegin(t, s, u.Username)
		if challenge.Mode != "verify" || challenge.QuestionID != question || challenge.Question == "" || strings.Contains(challenge.Question, u.Phone) {
			t.Fatal("login did not ask the stored question without disclosing its answer", challenge)
		}
		if _, err = s.Authenticate(originalSession); err != nil {
			t.Fatal("starting a challenge invalidated an existing session", err)
		}
		verified, session, err := s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: strings.ToUpper(strings.TrimSpace(answer))})
		must(t, err)
		if session == "" || verified.ID != u.ID {
			t.Fatal("correct answer did not complete login")
		}
		_, err = s.Authenticate(session)
		must(t, err)
		loginSecurityReject(t, s, pending, LoginSecurityInput{Answer: answer})
		again, fresh := loginSecurityBegin(t, s, u.Username)
		if fresh == pending || again.Mode != "verify" || again.QuestionID != question {
			t.Fatal("subsequent login skipped or reused the security challenge")
		}
		must(t, s.CancelLoginSecurity(fresh))
		account, err := s.Account(u.ID)
		must(t, err)
		adminData, err := s.AdminData("admin", "users", u.Username, 1)
		must(t, err)
		secrets := []string{stored.SecurityHash, stored.SecuritySalt, testPassword}
		if question != "registered_phone" {
			secrets = append(secrets, answer, strings.TrimSpace(answer), strings.ToLower(strings.TrimSpace(answer)))
		}
		for _, public := range []any{u, challenge, verified, account, adminData} {
			assertNoSecuritySecrets(t, public, secrets...)
		}
		must(t, s.db.View(func(tx *bolt.Tx) error {
			for _, bucket := range []string{"audit", "keys", "login_security_challenges"} {
				if err := tx.Bucket([]byte(bucket)).ForEach(func(_, raw []byte) error {
					if bytes.Contains(raw, []byte(strings.ToLower(strings.TrimSpace(answer)))) || bytes.Contains(raw, []byte(strings.TrimSpace(answer))) {
						t.Fatal("answer persisted in an audit, idempotency key or challenge", bucket)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	challenge, pending := loginSecurityBegin(t, s, "securityquestion0")
	if challenge.Mode != "verify" || challenge.QuestionID != "first_school" {
		t.Fatal("restart lost configured security question")
	}
	_, _, err = s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: "orchid academy"})
	must(t, err)
}

func TestLegacyChallengeMembersNeverFallbackOrEnroll(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	_, _, _, call := loginSecurityHTTP(t, s, false)
	for _, tc := range []struct{ username, phone string }{{"legacy_with_phone", "13800138000"}, {"legacy_no_phone", ""}} {
		legacy := register(t, s, tc.username, "")
		must(t, s.db.Update(func(tx *bolt.Tx) error {
			u, err := get[storedUser](tx, "users", legacy.ID)
			if err != nil {
				return err
			}
			u.Phone = tc.phone
			return put(tx, "users", u.ID, u)
		}))
		beforeUser := storedSecurityUser(t, s, legacy.ID)
		beforeSessions := loginSecurityCount(t, s)
		challenge, pending, err := s.BeginLogin(legacy.Username, testPassword)
		if err == nil || err.Error() != "账户尚未设置安全问题，请重新注册" || pending != "" || challenge != (LoginSecurityChallenge{}) {
			t.Fatal("unconfigured member received phone fallback, setup or a challenge", tc.username, challenge, err)
		}
		got := call("POST", "/api/v1/auth/login", map[string]string{"username": legacy.Username, "password": testPassword}, "same")
		if got.status != 200 || loginSecurityCookie(got.cookies, "starlight_session") == nil {
			t.Fatal("password login still required legacy question", got.status, string(got.body))
		}
		if loginSecurityCount(t, s) != beforeSessions+1 || storedSecurityUser(t, s, legacy.ID) != beforeUser {
			t.Fatal("direct login modified legacy profile")
		}

		// An old or fabricated pre-change challenge cannot reintroduce either
		// phone verification or enrollment through the completion endpoint.
		for _, mode := range []string{"verify", "setup"} {
			oldToken := randomID()
			oldChallenge := loginSecurityPending{UserID: legacy.ID, PasswordHash: beforeUser.Hash, PasswordSalt: beforeUser.PasswordSalt, Phone: tc.phone, Mode: mode, ExpiresAt: time.Now().Add(5 * time.Minute)}
			input := LoginSecurityInput{QuestionID: securityTestQuestion, Answer: securityTestAnswer, AnswerConfirmation: securityTestAnswer}
			if mode == "verify" {
				oldChallenge.QuestionID = "registered_phone"
				input = LoginSecurityInput{Answer: "13800138000"}
			}
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				if _, err := tx.CreateBucketIfNotExists([]byte("login_security_challenges")); err != nil {
					return err
				}
				return put(tx, "login_security_challenges", tokenKey(oldToken), oldChallenge)
			}))
			loginSecurityReject(t, s, oldToken, input)
			if storedSecurityUser(t, s, legacy.ID) != beforeUser {
				t.Fatal("old challenge migrated an unconfigured member", tc.username, mode)
			}
		}
		must(t, s.db.View(func(tx *bolt.Tx) error {
			if bucket := tx.Bucket([]byte("login_security_challenges")); bucket != nil && bucket.Stats().KeyN != 0 {
				t.Fatal("rejected old member left a pending login challenge")
			}
			return nil
		}))
		if account := call("GET", "/api/v1/account", nil, ""); account.status != 200 {
			t.Fatal("direct member login failed HTTP account access")
		}
	}
}

func TestLoginSecurityUnconfiguredAdministratorSetup(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	// The fixture administrator intentionally has no phone. Give it a normal
	// login password, then exercise first-login enrollment instead of bypassing.
	salt := randomID()
	encoded, err := hash(testPassword, salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		admin, err := get[storedUser](tx, "users", "admin")
		if err != nil {
			return err
		}
		admin.Hash, admin.PasswordSalt = encoded, salt
		return put(tx, "users", admin.ID, admin)
	}))
	challenge, pending := loginSecurityBegin(t, s, "admin")
	if challenge.Mode != "setup" {
		t.Fatal("legacy administrator without phone bypassed question setup")
	}
	for _, invalid := range []LoginSecurityInput{
		{QuestionID: "first_school", Answer: "Orchid Academy"},
		{QuestionID: "first_school", Answer: "Orchid Academy", AnswerConfirmation: "Different Academy"},
		{QuestionID: "registered_phone", Answer: "13800138000", AnswerConfirmation: "13800138000"},
		{QuestionID: "invented_question", Answer: "Orchid Academy", AnswerConfirmation: "Orchid Academy"},
	} {
		loginSecurityReject(t, s, pending, invalid)
		if storedSecurityUser(t, s, "admin").SecurityQuestionSet {
			t.Fatal("failed setup persisted partial security credentials")
		}
		_, pending = loginSecurityBegin(t, s, "admin")
	}
	admin, token, err := s.CompleteLoginSecurity(pending, LoginSecurityInput{QuestionID: "first_school", Answer: "  OrChId Academy ", AnswerConfirmation: "orchid academy"})
	must(t, err)
	if admin.ID != "admin" || admin.Role != "admin" || !admin.SecurityQuestionSet || token == "" {
		t.Fatal("successful setup did not atomically save the question and sign in")
	}
	challenge, pending = loginSecurityBegin(t, s, "admin")
	if challenge.Mode != "verify" || challenge.QuestionID != "first_school" {
		t.Fatal("next administrator login repeated enrollment instead of verification")
	}
	_, _, err = s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: "ORCHID ACADEMY"})
	must(t, err)
}

func TestLoginSecurityCancelExpiryAndCredentialChangesInvalidateChallenges(t *testing.T) {
	for _, scenario := range []string{"cancel", "expired", "disabled", "password", "security_hash", "security_question", "security_state", "phone"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _ := fundsFixture(t)
			defer s.Close()
			u, oldSession := loginSecurityRegister(t, s, "challengechanges", securityTestQuestion, securityTestAnswer)
			_, token := loginSecurityBegin(t, s, u.Username)
			switch scenario {
			case "cancel":
				must(t, s.CancelLoginSecurity(token))
				must(t, s.CancelLoginSecurity(token))
				if _, err := s.Authenticate(oldSession); err != nil {
					t.Fatal("cancel invalidated a previously active session", err)
				}
			case "expired":
				must(t, s.db.Update(func(tx *bolt.Tx) error {
					pending, err := get[loginSecurityPending](tx, "login_security_challenges", tokenKey(token))
					if err != nil {
						return err
					}
					pending.ExpiresAt = time.Now().Add(-time.Second)
					return put(tx, "login_security_challenges", tokenKey(token), pending)
				}))
			case "disabled":
				must(t, s.SetStatus("admin", u.ID, "disabled"))
			case "password":
				must(t, s.ChangePassword(u.ID, testPassword, "changed-password-12345"))
			default:
				must(t, s.db.Update(func(tx *bolt.Tx) error {
					current, err := get[storedUser](tx, "users", u.ID)
					if err != nil {
						return err
					}
					switch scenario {
					case "security_hash":
						current.SecurityHash = strings.Repeat("0", len(current.SecurityHash))
					case "security_question":
						current.SecurityQuestionID = "memorable_place"
					case "security_state":
						current.SecurityQuestionSet = false
					case "phone":
						current.Phone = "13900139000"
					}
					return put(tx, "users", u.ID, current)
				}))
			}
			loginSecurityReject(t, s, token, LoginSecurityInput{Answer: securityTestAnswer})
			loginSecurityReject(t, s, token, LoginSecurityInput{Answer: securityTestAnswer})
		})
	}
}

func TestLoginSecurityConcurrentSingleUseAndNoPrematureSession(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	u, oldSession := loginSecurityRegister(t, s, "securityconcurrent", securityTestQuestion, securityTestAnswer)
	_, pending := loginSecurityBegin(t, s, u.Username)
	before := loginSecurityCount(t, s)
	type result struct {
		user    User
		session string
		err     error
	}
	results := make(chan result, 8)
	var ready sync.WaitGroup
	ready.Add(8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			ready.Done()
			<-start
			user, session, err := s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: securityTestAnswer})
			results <- result{user, session, err}
		}()
	}
	ready.Wait()
	close(start)
	successes := 0
	for i := 0; i < 8; i++ {
		got := <-results
		if got.err == nil {
			successes++
			if got.user.ID != u.ID || got.session == "" {
				t.Fatal("successful verification has no user/session")
			}
		} else if got.user.ID != "" || got.session != "" {
			t.Fatal("concurrent rejected verification issued credentials")
		}
	}
	if successes != 1 || loginSecurityCount(t, s) != before+1 {
		t.Fatal("one challenge issued multiple sessions", successes)
	}
	_, err := s.Authenticate(oldSession)
	must(t, err)
	loginSecurityReject(t, s, pending, LoginSecurityInput{Answer: securityTestAnswer})
}

func TestLoginSecurityCancelAfterCompletionRevokesOnlyIssuedSession(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	u, originalSession := loginSecurityRegister(t, s, "securitycancelrace", securityTestQuestion, securityTestAnswer)
	_, pending := loginSecurityBegin(t, s, u.Username)
	before := loginSecurityCount(t, s)
	verified, issuedSession, err := s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: securityTestAnswer})
	must(t, err)
	if verified.ID != u.ID || issuedSession == "" || loginSecurityCount(t, s) != before+1 {
		t.Fatal("security completion did not issue exactly one session")
	}
	_, err = s.Authenticate(issuedSession)
	must(t, err)
	// Model a cancel request arriving just after verification committed but
	// before its response reached the browser. The completion receipt lets that
	// original challenge revoke only the session it just created.
	must(t, s.CancelLoginSecurity(pending))
	must(t, s.CancelLoginSecurity(pending))
	if _, err = s.Authenticate(issuedSession); err == nil {
		t.Fatal("late cancel left the just-issued session usable")
	}
	if loginSecurityCount(t, s) != before {
		t.Fatal("late cancel removed unrelated sessions or retained the new one")
	}
	_, err = s.Authenticate(originalSession)
	must(t, err)
	loginSecurityReject(t, s, pending, LoginSecurityInput{Answer: securityTestAnswer})
}

func TestLoginSecurityAttemptLimitSurvivesNewChallengesAndRestart(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	u, _ := loginSecurityRegister(t, s, "securitylimited", securityTestQuestion, securityTestAnswer)
	for i := 0; i < 5; i++ {
		_, pending := loginSecurityBegin(t, s, u.Username)
		loginSecurityReject(t, s, pending, LoginSecurityInput{Answer: "incorrect academy"})
	}
	assertLocked := func() {
		t.Helper()
		before := loginSecurityCount(t, s)
		_, pending, err := s.BeginLogin(u.Username, testPassword)
		if err == nil {
			loginSecurityReject(t, s, pending, LoginSecurityInput{Answer: securityTestAnswer})
		} else if pending != "" {
			t.Fatal("locked password step returned a usable challenge")
		}
		if loginSecurityCount(t, s) != before {
			t.Fatal("changing challenge bypassed the account limit")
		}
	}
	assertLocked()
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	assertLocked()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		attempt, err := get[securityAttempt](tx, "auth_security_attempts", "login_security:"+u.ID)
		if err != nil {
			return err
		}
		if attempt.Count < 5 || !attempt.Reset.After(time.Now()) {
			t.Fatal("login security limit was not durably recorded")
		}
		attempt.Reset = time.Now().Add(-time.Second)
		return put(tx, "auth_security_attempts", "login_security:"+u.ID, attempt)
	}))
	_, pending := loginSecurityBegin(t, s, u.Username)
	_, session, err := s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: securityTestAnswer})
	must(t, err)
	if session == "" {
		t.Fatal("expired verification limit prevented recovery")
	}
	// Successful verification clears the counter: four more errors still permit
	// a correct fifth attempt, even when each error uses a different challenge.
	for i := 0; i < 4; i++ {
		_, pending = loginSecurityBegin(t, s, u.Username)
		loginSecurityReject(t, s, pending, LoginSecurityInput{Answer: "incorrect academy"})
	}
	_, pending = loginSecurityBegin(t, s, u.Username)
	_, session, err = s.CompleteLoginSecurity(pending, LoginSecurityInput{Answer: securityTestAnswer})
	must(t, err)
	if session == "" {
		t.Fatal("successful verification did not reset the account limit")
	}
}

type loginSecurityHTTPResult struct {
	status  int
	body    []byte
	cookies []*http.Cookie
}

func loginSecurityHTTP(t *testing.T, s *Store, secure bool) (*HTTP, *http.Client, string, func(string, string, any, string) loginSecurityHTTPResult) {
	t.Helper()
	h := &HTTP{Store: s, SecureCookie: secure}
	mux := http.NewServeMux()
	h.Bind(mux)
	var server *httptest.Server
	if secure {
		server = httptest.NewTLSServer(h.Protect(mux))
	} else {
		server = httptest.NewServer(h.Protect(mux))
	}
	t.Cleanup(server.Close)
	client := server.Client()
	var err error
	client.Jar, err = cookiejar.New(nil)
	must(t, err)
	call := func(method, path string, body any, origin string) loginSecurityHTTPResult {
		t.Helper()
		raw, err := json.Marshal(body)
		must(t, err)
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		must(t, err)
		req.Header.Set("Content-Type", "application/json")
		if origin == "same" {
			origin = server.URL
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		response, err := client.Do(req)
		must(t, err)
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		must(t, err)
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("authentication response may be cached")
		}
		return loginSecurityHTTPResult{response.StatusCode, data, response.Cookies()}
	}
	return h, client, server.URL, call
}

func TestDirectLoginHTTPExistingSecurityAccountAndCSRF(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	u, oldSession := loginSecurityRegister(t, s, "securityhttpuser", securityTestQuestion, securityTestAnswer)
	_, _, _, call := loginSecurityHTTP(t, s, true)
	login := map[string]string{"username": u.Username, "password": testPassword}
	if got := call("POST", "/api/v1/auth/login", login, "https://attacker.test"); got.status != 403 {
		t.Fatal("cross-origin login accepted")
	}
	before := loginSecurityCount(t, s)
	got := call("POST", "/api/v1/auth/login", login, "same")
	cookie := loginSecurityCookie(got.cookies, "starlight_session")
	cleared := loginSecurityCookie(got.cookies, "starlight_login_challenge")
	if got.status != 200 || cookie == nil || cookie.Value == "" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || loginSecurityCount(t, s) != before+1 || cleared == nil || cleared.MaxAge >= 0 {
		t.Fatal("direct protected session missing", got.status, string(got.body))
	}
	var body struct {
		Data User `json:"data"`
	}
	must(t, json.Unmarshal(got.body, &body))
	if body.Data.ID != u.ID || bytes.Contains(got.body, []byte("requires_security")) {
		t.Fatal("login returned challenge instead of user")
	}
	if account := call("GET", "/api/v1/account", nil, ""); account.status != 200 {
		t.Fatal("direct session could not access account")
	}
	_, err := s.Authenticate(oldSession)
	must(t, err)
	if wrong := call("POST", "/api/v1/auth/login", map[string]string{"username": u.Username, "password": "wrong-password"}, "same"); wrong.status != 400 || loginSecurityCookie(wrong.cookies, "starlight_session") != nil {
		t.Fatal("wrong password accepted")
	}
	must(t, s.SetStatus("admin", u.ID, "disabled"))
	if disabled := call("POST", "/api/v1/auth/login", login, "same"); disabled.status != 403 || loginSecurityCookie(disabled.cookies, "starlight_session") != nil {
		t.Fatal("disabled account authenticated")
	}
}

func TestLoginSecurityHTTPExpiryDisabledChangedAndLockStatuses(t *testing.T) {
	for _, scenario := range []struct {
		name string
		code int
	}{{"expired", 410}, {"disabled", 403}, {"changed", 410}, {"locked", 429}} {
		t.Run(scenario.name, func(t *testing.T) {
			s, _, _ := fundsFixture(t)
			defer s.Close()
			u, _ := loginSecurityRegister(t, s, "securityhttpstatus", securityTestQuestion, securityTestAnswer)
			_, client, base, call := loginSecurityHTTP(t, s, false)
			_, pendingToken := loginSecurityBegin(t, s, u.Username)
			pending := &http.Cookie{Name: "starlight_login_challenge", Value: pendingToken, Path: "/api/v1/auth"}
			authURL, err := url.Parse(base + "/api/v1/auth/login/verify")
			must(t, err)
			client.Jar.SetCookies(authURL, []*http.Cookie{pending})

			// Sec-Fetch-Site is an independent CSRF guard, even if an attacker
			// supplies an apparently matching Origin header.
			req, err := http.NewRequest("POST", base+"/api/v1/auth/login/verify", strings.NewReader(`{"answer":"Primary School"}`))
			must(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", base)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			res, err := client.Do(req)
			must(t, err)
			res.Body.Close()
			if res.StatusCode != 403 {
				t.Fatal("cross-site fetch bypassed challenge CSRF protection")
			}
			switch scenario.name {
			case "disabled":
				must(t, s.SetStatus("admin", u.ID, "disabled"))
			case "expired":
				must(t, s.db.Update(func(tx *bolt.Tx) error {
					p, err := get[loginSecurityPending](tx, "login_security_challenges", tokenKey(pending.Value))
					if err != nil {
						return err
					}
					p.ExpiresAt = time.Now().Add(-time.Second)
					return put(tx, "login_security_challenges", tokenKey(pending.Value), p)
				}))
			case "changed":
				must(t, s.db.Update(func(tx *bolt.Tx) error {
					stored, err := get[storedUser](tx, "users", u.ID)
					if err != nil {
						return err
					}
					stored.SecuritySalt = randomID()
					return put(tx, "users", u.ID, stored)
				}))
			case "locked":
				must(t, s.db.Update(func(tx *bolt.Tx) error {
					if _, err := tx.CreateBucketIfNotExists([]byte("auth_security_attempts")); err != nil {
						return err
					}
					return put(tx, "auth_security_attempts", "login_security:"+u.ID, securityAttempt{Count: 5, Reset: time.Now().Add(15 * time.Minute)})
				}))
			}
			before := loginSecurityCount(t, s)
			got := call("POST", "/api/v1/auth/login/verify", map[string]string{"answer": securityTestAnswer}, "same")
			if got.status != scenario.code || loginSecurityCookie(got.cookies, "starlight_session") != nil || loginSecurityCount(t, s) != before {
				t.Fatalf("%s: want %d without authentication, got %d %s", scenario.name, scenario.code, got.status, got.body)
			}
			if got := call("GET", "/api/v1/account", nil, ""); got.status != 401 {
				t.Fatal("failed verification authorized account access")
			}
		})
	}
}
