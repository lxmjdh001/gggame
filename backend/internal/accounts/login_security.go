package accounts

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

const loginSecurityTTL = 5 * time.Minute
const loginChallengeCookie = "starlight_login_challenge"
const loginChallengeBucket = "login_security_challenges"
const loginCompletionBucket = "login_security_completions"

var errLoginChallengeInvalid = errors.New("登录验证已失效，请重新输入用户名和密码")
var errLoginSecurityAnswer = errors.New("安全问题答案不正确，请重试")

type SecurityQuestion struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func SecurityQuestions() []SecurityQuestion {
	return []SecurityQuestion{
		{ID: "registered_phone", Label: "请填写注册时的手机号"},
		{ID: "first_school", Label: "您就读的第一所学校叫什么？"},
		{ID: "childhood_nickname", Label: "您小时候的昵称是什么？"},
		{ID: "memorable_place", Label: "您最难忘的地方是哪里？"},
	}
}

func securityQuestionLabel(id string) (string, bool) {
	for _, question := range SecurityQuestions() {
		if question.ID == id {
			return question.Label, true
		}
	}
	return "", false
}

func normalizeSecurityAnswer(answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	if !utf8.ValidString(answer) || utf8.RuneCountInString(answer) < 2 || utf8.RuneCountInString(answer) > 128 || strings.IndexFunc(answer, unicode.IsControl) >= 0 {
		return "", errors.New("安全问题答案须为2–128个字符，不能包含控制字符")
	}
	// Only English letters are case-insensitive; other characters are preserved.
	normalized := []byte(answer)
	for i, value := range normalized {
		if value >= 'A' && value <= 'Z' {
			normalized[i] = value + ('a' - 'A')
		}
	}
	return string(normalized), nil
}

func validateSecurityAnswer(questionID, answer, phone string) (string, error) {
	if _, ok := securityQuestionLabel(questionID); !ok {
		return "", errors.New("请选择有效的安全问题")
	}
	normalized, err := normalizeSecurityAnswer(answer)
	if err != nil {
		return "", err
	}
	if questionID == "registered_phone" && (strings.TrimSpace(phone) == "" || subtle.ConstantTimeCompare([]byte(normalized), []byte(strings.TrimSpace(phone))) != 1) {
		return "", errors.New("安全问题答案须与登记手机号一致；未登记手机号的账户请选择其他问题")
	}
	return normalized, nil
}

type LoginSecurityChallenge struct {
	RequiresSecurity bool      `json:"requires_security"`
	Mode             string    `json:"mode"`
	QuestionID       string    `json:"question_id"`
	Question         string    `json:"question"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type LoginSecurityInput struct {
	Answer             string `json:"answer"`
	QuestionID         string `json:"question_id,omitempty"`
	AnswerConfirmation string `json:"answer_confirmation,omitempty"`
}

// The cookie holds the random token. Only tokenKey(token) is persisted as the
// bucket key; snapshots bind the challenge to the exact credentials it verified.
type loginSecurityPending struct {
	UserID              string
	PasswordHash        string
	PasswordSalt        string
	SecurityQuestionID  string
	SecurityHash        string
	SecuritySalt        string
	SecurityQuestionSet bool
	Phone               string
	Mode                string
	QuestionID          string
	ExpiresAt           time.Time
	Invalidated         bool
}

// A short-lived completion receipt lets cancellation revoke a session whose
// successful verification response is still in flight. Both keys are hashes.
type loginSecurityCompletion struct {
	SessionKey string
	ExpiresAt  time.Time
}

func pruneLoginSecurityCompletions(tx *bolt.Tx, now time.Time) error {
	bucket := tx.Bucket([]byte(loginCompletionBucket))
	if bucket == nil {
		return nil
	}
	cursor := bucket.Cursor()
	for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
		var receipt loginSecurityCompletion
		if json.Unmarshal(raw, &receipt) != nil || !now.Before(receipt.ExpiresAt) {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
	}
	return nil
}

func loginSecurityPolicy(u storedUser) (mode, questionID string, err error) {
	if u.SecurityQuestionSet || u.SecurityQuestionID != "" || u.SecurityHash != "" || u.SecuritySalt != "" {
		if _, ok := securityQuestionLabel(u.SecurityQuestionID); !ok || u.SecurityHash == "" || u.SecuritySalt == "" {
			return "", "", errors.New("账户安全问题配置异常，请联系管理员")
		}
		return "verify", u.SecurityQuestionID, nil
	}
	// Administrators are provisioned by Bootstrap rather than the registration
	// form, so only they need a first-login security-question setup step.
	if u.Role == "admin" {
		return "setup", "", nil
	}
	return "", "", errors.New("账户尚未设置安全问题，请重新注册")
}

func (s *Store) BeginLogin(username, password string) (LoginSecurityChallenge, string, error) {
	var before storedUser
	err := s.db.View(func(tx *bolt.Tx) error {
		id := string(tx.Bucket([]byte("usernames")).Get([]byte(strings.ToLower(strings.TrimSpace(username)))))
		var err error
		before, err = get[storedUser](tx, "users", id)
		return err
	})
	if len(password) > 128 {
		return LoginSecurityChallenge{}, "", errors.New("用户名或密码错误")
	}
	salt := before.PasswordSalt
	if salt == "" {
		salt = "nonexistent-account-salt"
	}
	candidate, hashErr := hash(password, salt)
	if hashErr != nil {
		return LoginSecurityChallenge{}, "", hashErr
	}
	if err != nil || subtle.ConstantTimeCompare([]byte(candidate), []byte(before.Hash)) != 1 {
		return LoginSecurityChallenge{}, "", errors.New("用户名或密码错误")
	}
	var challenge LoginSecurityChallenge
	var token string
	err = s.db.Update(func(tx *bolt.Tx) error {
		current, err := active(tx, before.ID)
		if err != nil {
			return err
		}
		if current.Hash != before.Hash || current.PasswordSalt != before.PasswordSalt {
			return errLoginChallengeInvalid
		}
		mode, questionID, err := loginSecurityPolicy(current)
		if err != nil {
			return err
		}
		bucket, err := tx.CreateBucketIfNotExists([]byte(loginChallengeBucket))
		if err != nil {
			return err
		}
		now := time.Now()
		if err = pruneLoginSecurityCompletions(tx, now); err != nil {
			return err
		}
		cursor, count := bucket.Cursor(), 0
		for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
			var pending loginSecurityPending
			if json.Unmarshal(raw, &pending) != nil || !now.Before(pending.ExpiresAt) {
				if err = cursor.Delete(); err != nil {
					return err
				}
			} else {
				count++
			}
		}
		if count >= 2000 {
			return errors.New("登录验证请求较多，请稍后重试")
		}
		question, _ := securityQuestionLabel(questionID)
		challenge = LoginSecurityChallenge{RequiresSecurity: true, Mode: mode, QuestionID: questionID, Question: question, ExpiresAt: now.Add(loginSecurityTTL)}
		token = randomID()
		pending := loginSecurityPending{UserID: current.ID, PasswordHash: current.Hash, PasswordSalt: current.PasswordSalt, SecurityQuestionID: current.SecurityQuestionID, SecurityHash: current.SecurityHash, SecuritySalt: current.SecuritySalt, SecurityQuestionSet: current.SecurityQuestionSet, Phone: current.Phone, Mode: mode, QuestionID: questionID, ExpiresAt: challenge.ExpiresAt}
		return put(tx, loginChallengeBucket, tokenKey(token), pending)
	})
	if err != nil {
		return LoginSecurityChallenge{}, "", err
	}
	return challenge, token, nil
}

func currentLoginChallenge(tx *bolt.Tx, key string, now time.Time) (loginSecurityPending, storedUser, error) {
	var pending loginSecurityPending
	if tx.Bucket([]byte(loginChallengeBucket)) == nil {
		return pending, storedUser{}, errLoginChallengeInvalid
	}
	pending, err := get[loginSecurityPending](tx, loginChallengeBucket, key)
	if err != nil {
		return pending, storedUser{}, errLoginChallengeInvalid
	}
	current, err := active(tx, pending.UserID)
	if errors.Is(err, ErrForbidden) {
		return pending, current, ErrForbidden
	}
	if err != nil {
		return pending, current, errLoginChallengeInvalid
	}
	mode, questionID, err := loginSecurityPolicy(current)
	if err != nil || pending.Invalidated || !now.Before(pending.ExpiresAt) || mode != pending.Mode || questionID != pending.QuestionID || current.Hash != pending.PasswordHash || current.PasswordSalt != pending.PasswordSalt || current.SecurityQuestionID != pending.SecurityQuestionID || current.SecurityHash != pending.SecurityHash || current.SecuritySalt != pending.SecuritySalt || current.SecurityQuestionSet != pending.SecurityQuestionSet || current.Phone != pending.Phone {
		return pending, current, errLoginChallengeInvalid
	}
	return pending, current, nil
}

func (s *Store) CompleteLoginSecurity(token string, in LoginSecurityInput) (User, string, error) {
	if token == "" || len(token) > 128 {
		return User{}, "", errLoginChallengeInvalid
	}
	key := tokenKey(token)
	var pending loginSecurityPending
	var validationErr error
	// Commit the attempt before validating the answer so failures, concurrency and
	// application restarts cannot reset the account's shared retry budget.
	err := s.db.Update(func(tx *bolt.Tx) error {
		var err error
		pending, _, err = currentLoginChallenge(tx, key, time.Now())
		if err != nil {
			validationErr = err
			if bucket := tx.Bucket([]byte(loginChallengeBucket)); bucket != nil {
				return bucket.Delete([]byte(key))
			}
			return nil
		}
		if _, err = tx.CreateBucketIfNotExists([]byte("auth_security_attempts")); err != nil {
			return err
		}
		attemptKey := "login_security:" + pending.UserID
		attempt, err := get[securityAttempt](tx, "auth_security_attempts", attemptKey)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		now := time.Now()
		if !now.Before(attempt.Reset) {
			attempt = securityAttempt{Reset: now.Add(securityAttemptWindow)}
		}
		if attempt.Count >= 5 {
			validationErr = errSecurityLimited
			return nil
		}
		attempt.Count++
		return put(tx, "auth_security_attempts", attemptKey, attempt)
	})
	if err != nil {
		return User{}, "", err
	}
	if validationErr != nil {
		return User{}, "", validationErr
	}

	answer, err := normalizeSecurityAnswer(in.Answer)
	if err != nil {
		return User{}, "", err
	}
	questionID := strings.TrimSpace(in.QuestionID)
	var nextQuestion, nextHash, nextSalt string
	switch pending.Mode {
	case "verify":
		if questionID != "" && questionID != pending.QuestionID {
			return User{}, "", errLoginSecurityAnswer
		}
		candidate, err := hash(answer, pending.SecuritySalt)
		if err != nil {
			return User{}, "", err
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(pending.SecurityHash)) != 1 {
			return User{}, "", errLoginSecurityAnswer
		}
	case "setup":
		answer, err = validateSecurityAnswer(questionID, in.Answer, pending.Phone)
		if err != nil {
			return User{}, "", err
		}
		confirmation, err := normalizeSecurityAnswer(in.AnswerConfirmation)
		if err != nil || subtle.ConstantTimeCompare([]byte(answer), []byte(confirmation)) != 1 {
			return User{}, "", errors.New("两次安全问题答案不一致")
		}
		nextQuestion, nextSalt = questionID, randomID()
		nextHash, err = hash(answer, nextSalt)
		if err != nil {
			return User{}, "", err
		}
	default:
		return User{}, "", errLoginChallengeInvalid
	}

	var user User
	var session string
	err = s.db.Update(func(tx *bolt.Tx) error {
		verified, current, err := currentLoginChallenge(tx, key, time.Now())
		if err != nil {
			validationErr = err
			if bucket := tx.Bucket([]byte(loginChallengeBucket)); bucket != nil {
				return bucket.Delete([]byte(key))
			}
			return nil
		}
		if verified.UserID != pending.UserID || verified.PasswordHash != pending.PasswordHash || verified.PasswordSalt != pending.PasswordSalt || verified.SecurityQuestionID != pending.SecurityQuestionID || verified.SecurityHash != pending.SecurityHash || verified.SecuritySalt != pending.SecuritySalt || verified.SecurityQuestionSet != pending.SecurityQuestionSet || verified.Phone != pending.Phone || verified.Mode != pending.Mode || verified.QuestionID != pending.QuestionID || !verified.ExpiresAt.Equal(pending.ExpiresAt) {
			validationErr = errLoginChallengeInvalid
			return tx.Bucket([]byte(loginChallengeBucket)).Delete([]byte(key))
		}
		if nextHash != "" {
			current.SecurityQuestionID, current.SecurityHash, current.SecuritySalt = nextQuestion, nextHash, nextSalt
			current.SecurityQuestionSet = true
			if err = put(tx, "users", current.ID, current); err != nil {
				return err
			}
		}
		session, err = newLogin(tx, current.ID)
		if err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists([]byte(loginCompletionBucket)); err != nil {
			return err
		}
		if err = put(tx, loginCompletionBucket, key, loginSecurityCompletion{SessionKey: tokenKey(session), ExpiresAt: time.Now().Add(loginSecurityTTL)}); err != nil {
			return err
		}
		if err = tx.Bucket([]byte(loginChallengeBucket)).Delete([]byte(key)); err != nil {
			return err
		}
		if err = tx.Bucket([]byte("auth_security_attempts")).Delete([]byte("login_security:" + current.ID)); err != nil {
			return err
		}
		user = current.User
		return nil
	})
	if err != nil {
		return User{}, "", err
	}
	if validationErr != nil {
		return User{}, "", validationErr
	}
	return user, session, nil
}

func (s *Store) CancelLoginSecurity(token string) error {
	if token == "" || len(token) > 128 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		key := tokenKey(token)
		if bucket := tx.Bucket([]byte(loginChallengeBucket)); bucket != nil {
			if err := bucket.Delete([]byte(key)); err != nil {
				return err
			}
		}
		if bucket := tx.Bucket([]byte(loginCompletionBucket)); bucket != nil {
			receipt, err := get[loginSecurityCompletion](tx, loginCompletionBucket, key)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			if err == nil && time.Now().Before(receipt.ExpiresAt) {
				if err = tx.Bucket([]byte("logins")).Delete([]byte(receipt.SessionKey)); err != nil {
					return err
				}
			}
			return bucket.Delete([]byte(key))
		}
		return nil
	})
}

// Preserve the reason for a disabled user's pending challenge while making it
// permanently unusable if the account is subsequently enabled again.
func invalidateLoginSecurity(tx *bolt.Tx, id string) error {
	bucket := tx.Bucket([]byte(loginChallengeBucket))
	if bucket == nil {
		return nil
	}
	cursor := bucket.Cursor()
	for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
		var pending loginSecurityPending
		if json.Unmarshal(raw, &pending) != nil || pending.UserID != id {
			continue
		}
		pending.Invalidated = true
		if err := put(tx, loginChallengeBucket, string(key), pending); err != nil {
			return err
		}
	}
	return nil
}

func pendingLoginToken(r *http.Request) string {
	cookie, err := r.Cookie(loginChallengeCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *HTTP) setLoginChallengeCookie(w http.ResponseWriter, token string) {
	age := int(loginSecurityTTL / time.Second)
	if token == "" {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: loginChallengeCookie, Value: token, Path: "/api/v1/auth", HttpOnly: true, Secure: h.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: age})
}

func loginSecurityResult(w http.ResponseWriter, value any, err error) {
	if errors.Is(err, errLoginChallengeInvalid) {
		response(w, http.StatusGone, map[string]any{"error": map[string]string{"message": err.Error(), "code": "login_challenge_expired"}})
		return
	}
	if errors.Is(err, errSecurityLimited) {
		response(w, http.StatusTooManyRequests, map[string]any{"error": map[string]string{"message": err.Error(), "code": "security_locked"}})
		return
	}
	result(w, value, err)
}
