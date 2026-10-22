package accounts

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

const captchaTTL = 5 * time.Minute
const securityAttemptWindow = 15 * time.Minute

var registrationUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9]{4,32}$`)
var uppercasePasswordPattern = regexp.MustCompile(`[A-Z]`)
var lowercasePasswordPattern = regexp.MustCompile(`[a-z]`)

var withdrawalPasswordPattern = regexp.MustCompile(`^[0-9]{6}$`)
var errSecurityLimited = errors.New("验证次数过多，请15分钟后再试")

type SecureRegistrationInput struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	InviteCode         string `json:"invite_code"`
	Phone              string `json:"phone"`
	Name               string `json:"name"`
	Wechat             string `json:"wechat"`
	WithdrawalPassword string `json:"withdrawal_password"`
	CaptchaID          string `json:"captcha_id"`
	CaptchaAnswer      string `json:"captcha_answer"`
	SecurityQuestionID string `json:"security_question_id"`
	SecurityAnswer     string `json:"security_answer"`
}
type CaptchaChallenge struct {
	ID           string    `json:"id"`
	ImageDataURL string    `json:"image_data_url"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type captchaRecord struct {
	Digest    string
	ExpiresAt time.Time
}
type registrationSecurity struct {
	Wechat, Hash, Salt                             string
	SecurityQuestionID, SecurityHash, SecuritySalt string
}
type securityAttempt struct {
	Count int
	Reset time.Time
}

func captchaDigest(id, answer string) string {
	digest := sha256.Sum256([]byte(id + ":" + strings.ToUpper(strings.TrimSpace(answer))))
	return hex.EncodeToString(digest[:])
}

func (s *Store) NewCaptcha() (CaptchaChallenge, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	answer := make([]byte, 5)
	for i := range answer {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return CaptchaChallenge{}, err
		}
		answer[i] = alphabet[index.Int64()]
	}
	data, err := renderCaptcha(string(answer))
	if err != nil {
		return CaptchaChallenge{}, err
	}
	now := time.Now()
	challenge := CaptchaChallenge{ID: randomID(), ImageDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), ExpiresAt: now.Add(captchaTTL)}
	err = s.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("auth_captchas"))
		if err != nil {
			return err
		}
		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			entry, err := get[captchaRecord](tx, "auth_captchas", string(key))
			if err != nil || !now.Before(entry.ExpiresAt) {
				if err = cursor.Delete(); err != nil {
					return err
				}
			}
		}
		if bucket.Stats().KeyN >= 2000 {
			return errors.New("验证码请求较多，请稍后再试")
		}
		return put(tx, "auth_captchas", challenge.ID, captchaRecord{Digest: captchaDigest(challenge.ID, string(answer)), ExpiresAt: challenge.ExpiresAt})
	})
	return challenge, err
}

// Consume in a separate transaction: invalid answers and later registration
// failures must not roll back the one-use challenge.
func (s *Store) consumeCaptcha(id, answer string) error {
	invalid := errors.New("验证码错误或已过期，请刷新后重试（不区分大小写）")
	if id == "" || len(id) > 128 {
		return invalid
	}
	var valid bool
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("auth_captchas"))
		if bucket == nil {
			return nil
		}
		record, err := get[captchaRecord](tx, "auth_captchas", id)
		if err != nil {
			return nil
		}
		if err = bucket.Delete([]byte(id)); err != nil {
			return err
		}
		valid = time.Now().Before(record.ExpiresAt) && len(answer) <= 12 && subtle.ConstantTimeCompare([]byte(record.Digest), []byte(captchaDigest(id, answer))) == 1
		return nil
	})
	if err != nil {
		return err
	}
	if !valid {
		return invalid
	}
	return nil
}

func (s *Store) RegisterSecure(in SecureRegistrationInput) (User, string, error) {
	if err := s.consumeCaptcha(in.CaptchaID, in.CaptchaAnswer); err != nil {
		return User{}, "", err
	}
	if !withdrawalPasswordPattern.MatchString(in.WithdrawalPassword) {
		return User{}, "", errors.New("提现密码须为6位数字")
	}
	wechat := strings.TrimSpace(in.Wechat)
	if utf8.RuneCountInString(wechat) > 64 || strings.IndexFunc(wechat, unicode.IsControl) >= 0 {
		return User{}, "", errors.New("微信号最多64个字符，不能包含控制字符")
	}
	if !phonePattern.MatchString(strings.TrimSpace(in.Phone)) {
		return User{}, "", errors.New("请输入11位手机号，或带+国家区号的手机号码")
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Name)) > 40 || strings.IndexFunc(strings.TrimSpace(in.Name), unicode.IsControl) >= 0 {
		return User{}, "", errors.New("姓名最多40个字符，不能包含控制字符")
	}
	if !registrationUsernamePattern.MatchString(strings.TrimSpace(in.Username)) {
		return User{}, "", errors.New("用户名须为4–32位字母或数字，不支持下划线")
	}
	if !passwordValid(in.Password) || !uppercasePasswordPattern.MatchString(in.Password) || !lowercasePasswordPattern.MatchString(in.Password) {
		return User{}, "", errors.New("登录密码须为10–128位，且同时包含大写和小写字母")
	}
	if strings.TrimSpace(in.Name) == "" {
		return User{}, "", errors.New("请填写姓名")
	}
	if strings.TrimSpace(in.Wechat) == "" {
		return User{}, "", errors.New("请填写微信号")
	}
	if strings.TrimSpace(in.InviteCode) == "" {
		return User{}, "", errors.New("注册需要邀请码，请填写有效邀请码")
	}
	salt := randomID()
	pinHash, err := hash(in.WithdrawalPassword, salt)
	if err != nil {
		return User{}, "", err
	}
	return s.register(strings.TrimSpace(in.Username), in.Password, in.InviteCode, in.Phone, in.Name, &registrationSecurity{Wechat: wechat, Hash: pinHash, Salt: salt})
}

// Reserve attempts before checking a secret, committing the count even on a
// rejected guess. The persistent per-account limit also covers concurrent calls.
func (s *Store) reserveSecurityAttempt(scope, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("auth_security_attempts")); err != nil {
			return err
		}
		key := scope + ":" + id
		record, err := get[securityAttempt](tx, "auth_security_attempts", key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		now := time.Now()
		if !now.Before(record.Reset) {
			record = securityAttempt{Reset: now.Add(securityAttemptWindow)}
		}
		if record.Count >= 5 {
			return errSecurityLimited
		}
		record.Count++
		return put(tx, "auth_security_attempts", key, record)
	})
}

func (s *Store) VerifyWithdrawalPassword(id, pin string) error {
	var before storedUser
	err := s.db.View(func(tx *bolt.Tx) error { var err error; before, err = active(tx, id); return err })
	if err != nil {
		return err
	}
	if !before.WithdrawalPasswordSet || before.WithdrawalHash == "" || before.WithdrawalSalt == "" {
		return errors.New("请先设置提现密码")
	}
	if err = s.reserveSecurityAttempt("withdrawal", id); err != nil {
		return err
	}
	if !withdrawalPasswordPattern.MatchString(pin) {
		return errors.New("提现密码不正确")
	}
	candidate, err := hash(pin, before.WithdrawalSalt)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(before.WithdrawalHash)) != 1 {
		return errors.New("提现密码不正确")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		current, err := active(tx, id)
		if err != nil {
			return err
		}
		if current.WithdrawalHash != before.WithdrawalHash || current.WithdrawalSalt != before.WithdrawalSalt {
			return errors.New("提现密码已更新，请重试")
		}
		return tx.Bucket([]byte("auth_security_attempts")).Delete([]byte("withdrawal:" + id))
	})
}

// Initial setup needs an authenticated account; later changes verify the
// existing withdrawal PIN using the same persistent limiter as withdrawals.
func (s *Store) SetWithdrawalPassword(id, currentPIN, pin string) (User, error) {
	if !withdrawalPasswordPattern.MatchString(pin) {
		return User{}, errors.New("提现密码须为6位数字")
	}
	var before storedUser
	err := s.db.View(func(tx *bolt.Tx) error { var err error; before, err = active(tx, id); return err })
	if err != nil {
		return User{}, err
	}
	if before.WithdrawalPasswordSet {
		if err = s.VerifyWithdrawalPassword(id, currentPIN); err != nil {
			return User{}, err
		}
	} else {
		if err = s.reserveSecurityAttempt("withdrawal_setup", id); err != nil {
			return User{}, err
		}
		if currentPIN != "" {
			return User{}, errors.New("首次设置请直接填写两次新提现密码")
		}
	}
	salt := randomID()
	pinHash, err := hash(pin, salt)
	if err != nil {
		return User{}, err
	}
	var result User
	err = s.db.Update(func(tx *bolt.Tx) error {
		current, err := active(tx, id)
		if err != nil {
			return err
		}
		if current.Hash != before.Hash || current.PasswordSalt != before.PasswordSalt || current.WithdrawalPasswordSet != before.WithdrawalPasswordSet || current.WithdrawalHash != before.WithdrawalHash || current.WithdrawalSalt != before.WithdrawalSalt {
			return errors.New("账户或提现密码已更新，请重试")
		}
		current.WithdrawalHash, current.WithdrawalSalt, current.WithdrawalPasswordSet = pinHash, salt, true
		if err = put(tx, "users", id, current); err != nil {
			return err
		}
		bucket := tx.Bucket([]byte("auth_security_attempts"))
		if err = bucket.Delete([]byte("withdrawal_setup:" + id)); err != nil {
			return err
		}
		if err = bucket.Delete([]byte("withdrawal:" + id)); err != nil {
			return err
		}
		if err = audit(tx, id, "withdrawal_password.set", id, "设置或更新提现密码"); err != nil {
			return err
		}
		result = current.User
		return nil
	})
	return result, err
}

// A small bitmap font keeps the challenge raster-only without external font or
// image dependencies. Answers are never included in image metadata or responses.
var captchaGlyphs = map[byte][7]byte{
	'2': {14, 17, 1, 2, 4, 8, 31}, '3': {30, 1, 1, 14, 1, 1, 30}, '4': {2, 6, 10, 18, 31, 2, 2}, '5': {31, 16, 16, 30, 1, 1, 30}, '6': {14, 16, 16, 30, 17, 17, 14}, '7': {31, 1, 2, 4, 8, 8, 8}, '8': {14, 17, 17, 14, 17, 17, 14}, '9': {14, 17, 17, 15, 1, 1, 14},
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30}, 'C': {14, 17, 16, 16, 16, 17, 14}, 'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16}, 'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'J': {7, 2, 2, 2, 18, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31}, 'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 25, 25, 21, 19, 19, 17}, 'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17}, 'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'U': {17, 17, 17, 17, 17, 17, 14}, 'V': {17, 17, 17, 17, 17, 10, 4}, 'W': {17, 17, 17, 21, 21, 27, 17}, 'X': {17, 17, 10, 4, 10, 17, 17}, 'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31},
}

func renderCaptcha(answer string) ([]byte, error) {
	noise := make([]byte, 512)
	if _, err := rand.Read(noise); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, 190, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 190; x++ {
			img.SetRGBA(x, y, color.RGBA{244, 237, 255, 255})
		}
	}
	for i := 0; i < 180; i++ {
		img.SetRGBA(int(noise[i])%190, int(noise[i+180])%64, color.RGBA{158, 137, 186, 255})
	}
	for i := 0; i < 3; i++ {
		y, slope := int(noise[360+i])%44+10, int(noise[364+i])%7-3
		for x := 0; x < 190; x++ {
			img.SetRGBA(x, y+(x-95)*slope/30, color.RGBA{190, 169, 214, 255})
		}
	}
	for i := range answer {
		glyph := captchaGlyphs[answer[i]]
		startX, startY := 15+i*34+int(noise[380+i])%5, 12+int(noise[390+i])%9
		ink := color.RGBA{uint8(66 + noise[400+i]%40), uint8(35 + noise[410+i]%40), uint8(100 + noise[420+i]%45), 255}
		for row, bits := range glyph {
			for col := 0; col < 5; col++ {
				if bits&(1<<uint(4-col)) == 0 {
					continue
				}
				for dy := 0; dy < 4; dy++ {
					for dx := 0; dx < 4; dx++ {
						img.SetRGBA(startX+col*4+dx+(row-3)/2, startY+row*4+dy, ink)
					}
				}
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
