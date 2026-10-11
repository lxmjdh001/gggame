package accounts

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"regexp"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("请先登录")
var ErrForbidden = errors.New("账户不可用或无权限")
var ErrNotFound = errors.New("记录不存在")

const maxMoney int64 = 100000000000

var buckets = []string{"users", "usernames", "invites", "logins", "ledger", "requests", "settings", "orders", "commissions", "audit", "keys", "market_quotes", "pending_markets", "settlement_plans", "prepared_payouts"}

type Store struct{ db *bolt.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, e := tx.CreateBucketIfNotExists([]byte(name)); e != nil {
				return e
			}
		}
		if e := initializeIssueStakes(tx); e != nil {
			return e
		}
		if e := seedPromotions(tx); e != nil {
			return e
		}
		for _, g := range games.All() {
			if tx.Bucket([]byte("market_quotes")).Get([]byte(g.ID)) == nil {
				if e := put(tx, "market_quotes", g.ID, defaultQuotes(g)); e != nil {
					return e
				}
			} else {
				quotes, e := get[[]Quote](tx, "market_quotes", g.ID)
				if e != nil {
					return e
				}
				seen := map[string]bool{}
				for _, q := range quotes {
					seen[q.MarketID] = true
				}
				added := []string{}
				for _, q := range defaultQuotes(g) {
					if !seen[q.MarketID] {
						quotes = append(quotes, q)
						added = append(added, q.MarketID)
					}
				}
				if len(added) > 0 {
					if e := put(tx, "market_quotes", g.ID, quotes); e != nil {
						return e
					}
					if e := audit(tx, "system", "odds.initialize", g.ID, "新增玩法赔率："+strings.Join(added, ",")); e != nil {
						return e
					}
				}
			}
		}
		if tx.Bucket([]byte("settings")).Get([]byte("global")) == nil {
			return put(tx, "settings", "global", defaults())
		}
		cfg, e := get[Settings](tx, "settings", "global")
		if e != nil {
			return e
		}
		changed := false
		amountChanged := false
		issueLimitChanged := false
		for i := range cfg.Plays {
			if cfg.Plays[i].UserIssueLimitCents == 0 {
				cfg.Plays[i].UserIssueLimitCents = defaultUserIssueLimit
				issueLimitChanged = true
			}
			if cfg.Plays[i].MaxStakeCents == 0 {
				cfg.Plays[i].MaxStakeCents = max(int64(100000), cfg.Plays[i].UnitCents)
				amountChanged = true
			}
			if cfg.Plays[i].CloseSeconds != games.ClosedWindowSeconds {
				cfg.Plays[i].CloseSeconds = games.ClosedWindowSeconds
				changed = true
			}
		}
		if changed || amountChanged || issueLimitChanged {
			if e = put(tx, "settings", "global", cfg); e != nil {
				return e
			}
			if changed {
				if e = audit(tx, "system", "timing.update", "global", "整分前10秒封盘、后05秒开奖；封盘窗口固定15秒"); e != nil {
					return e
				}
			}
			if issueLimitChanged {
				if e = audit(tx, "system", "user_issue_limit.initialize", "global", "单用户单场次单期投注上限初始化为20000元"); e != nil {
					return e
				}
			}
			if amountChanged {
				return audit(tx, "system", "stake_limit.initialize", "global", "快捷金额投注：初始每注上限1000元，起投更高时以上述起投额为上限")
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func tokenKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func put(tx *bolt.Tx, bucket, key string, v any) error {
	data, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return tx.Bucket([]byte(bucket)).Put([]byte(key), data)
}
func get[T any](tx *bolt.Tx, bucket, key string) (T, error) {
	var v T
	raw := tx.Bucket([]byte(bucket)).Get([]byte(key))
	if raw == nil {
		return v, ErrNotFound
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}
func list[T any](tx *bolt.Tx, bucket string) ([]T, error) {
	items := []T{}
	c := tx.Bucket([]byte(bucket)).Cursor()
	for k, v := c.Last(); k != nil; k, v = c.Prev() {
		var item T
		if err := json.Unmarshal(v, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
func seq(tx *bolt.Tx, b string) string {
	n, e := tx.Bucket([]byte(b)).NextSequence()
	if e != nil {
		panic(e)
	}
	return fmt.Sprintf("%020d", n)
}
func defaults() Settings {
	v := Settings{Registration: true, Announcement: "欢迎来到星彩娱乐。账户与开奖记录已同步至服务器。", Plays: []PlaySetting{}}
	for _, g := range games.All() {
		v.Plays = append(v.Plays, PlaySetting{SessionID: g.ID, Enabled: true, UnitCents: 200, MaxMultiplier: 99, CloseSeconds: games.ClosedWindowSeconds, MaxStakeCents: 100000, UserIssueLimitCents: defaultUserIssueLimit})
	}
	return v
}
func hash(password, salt string) (string, error) {
	b, err := pbkdf2.Key(sha256.New, password, []byte(salt), 600000, 32)
	return hex.EncodeToString(b), err
}
func passwordValid(p string) bool { return len(p) >= 10 && len(p) <= 128 }

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{4,32}$`)

func (s *Store) Bootstrap(username, password string) (bool, error) {
	exists := false
	err := s.db.View(func(tx *bolt.Tx) error {
		users, e := list[storedUser](tx, "users")
		for _, u := range users {
			if u.Role == "admin" {
				exists = true
			}
		}
		return e
	})
	if err != nil || exists {
		return false, err
	}
	if !usernamePattern.MatchString(username) || !passwordValid(password) {
		return false, errors.New("管理员用户名需4–32位字母数字下划线，密码需10–128位")
	}
	salt := randomID()
	h, err := hash(password, salt)
	if err != nil {
		return false, err
	}
	err = s.db.Update(func(tx *bolt.Tx) error { _, e := createUser(tx, username, h, salt, "", "admin"); return e })
	return err == nil, err
}
func createUser(tx *bolt.Tx, username, h, salt, invite, role string) (User, error) {
	if tx.Bucket([]byte("usernames")).Get([]byte(strings.ToLower(username))) != nil {
		return User{}, errors.New("用户名已存在")
	}
	inviter := ""
	if invite != "" {
		inviter = string(tx.Bucket([]byte("invites")).Get([]byte(strings.ToUpper(invite))))
		if inviter == "" {
			return User{}, errors.New("邀请码不存在")
		}
		u, e := get[storedUser](tx, "users", inviter)
		if e != nil || u.Status != "active" {
			return User{}, errors.New("邀请人账户不可用")
		}
	}
	u := storedUser{User: User{ID: randomID(), Username: username, Role: role, Status: "active", InviteCode: strings.ToUpper(randomID()[:12]), InviterID: inviter, CreatedAt: time.Now()}, Hash: h, PasswordSalt: salt}
	if e := put(tx, "users", u.ID, u); e != nil {
		return User{}, e
	}
	if e := tx.Bucket([]byte("usernames")).Put([]byte(strings.ToLower(username)), []byte(u.ID)); e != nil {
		return User{}, e
	}
	if e := tx.Bucket([]byte("invites")).Put([]byte(u.InviteCode), []byte(u.ID)); e != nil {
		return User{}, e
	}
	return u.User, nil
}
func newLogin(tx *bolt.Tx, id string) (string, error) {
	// Prune expired sessions whenever a new session is issued.
	c := tx.Bucket([]byte("logins")).Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var l Login
		if json.Unmarshal(v, &l) == nil && time.Now().After(l.Expires) {
			if e := c.Delete(); e != nil {
				return "", e
			}
		}
	}
	token := randomID()
	return token, put(tx, "logins", tokenKey(token), Login{id, time.Now().Add(7 * 24 * time.Hour)})
}
func (s *Store) Register(username, password, invite string) (User, string, error) {
	invite = strings.TrimSpace(invite)
	if !usernamePattern.MatchString(username) || !passwordValid(password) {
		return User{}, "", errors.New("用户名需4–32位字母数字下划线，密码需10–128位")
	}
	salt := randomID()
	h, e := hash(password, salt)
	if e != nil {
		return User{}, "", e
	}
	var u User
	var token string
	e = s.db.Update(func(tx *bolt.Tx) error {
		cfg, err := get[Settings](tx, "settings", "global")
		if err != nil {
			return err
		}
		if !cfg.Registration {
			return errors.New("注册暂未开放")
		}
		if cfg.InviteRequired && invite == "" {
			return errors.New("注册需要邀请码，请填写有效邀请码")
		}
		u, err = createUser(tx, username, h, salt, invite, "user")
		if err != nil {
			return err
		}
		if cfg.RegistrationBonusCents > 0 {
			stored, e := get[storedUser](tx, "users", u.ID)
			if e != nil {
				return e
			}
			if e = ledger(tx, &stored, cfg.RegistrationBonusCents, 0, "registration_bonus", u.ID, "注册赠送（开发测试）"); e != nil {
				return e
			}
			u = stored.User
		}
		token, err = newLogin(tx, u.ID)
		return err
	})
	return u, token, e
}
func (s *Store) Login(username, password string) (User, string, error) {
	var u storedUser
	err := s.db.View(func(tx *bolt.Tx) error {
		id := string(tx.Bucket([]byte("usernames")).Get([]byte(strings.ToLower(username))))
		var e error
		u, e = get[storedUser](tx, "users", id)
		return e
	})
	if len(password) > 128 {
		return User{}, "", errors.New("用户名或密码错误")
	}
	salt := u.PasswordSalt
	if salt == "" {
		salt = "nonexistent-account-salt"
	}
	h, e := hash(password, salt)
	if e != nil {
		return User{}, "", e
	}
	if err != nil || subtle.ConstantTimeCompare([]byte(h), []byte(u.Hash)) != 1 {
		return User{}, "", errors.New("用户名或密码错误")
	}
	var token string
	err = s.db.Update(func(tx *bolt.Tx) error {
		current, e := active(tx, u.ID)
		if e != nil {
			return e
		}
		if current.Hash != u.Hash {
			return ErrUnauthorized
		}
		u = current
		token, e = newLogin(tx, u.ID)
		return e
	})
	return u.User, token, err
}
func active(tx *bolt.Tx, id string) (storedUser, error) {
	u, e := get[storedUser](tx, "users", id)
	if e != nil {
		return u, ErrUnauthorized
	}
	if u.Status != "active" {
		return u, ErrForbidden
	}
	return u, nil
}
func admin(tx *bolt.Tx, id string) error {
	u, e := active(tx, id)
	if e != nil {
		return e
	}
	if u.Role != "admin" {
		return ErrForbidden
	}
	return nil
}
func (s *Store) Authenticate(token string) (User, error) {
	var u storedUser
	err := s.db.View(func(tx *bolt.Tx) error {
		l, e := get[Login](tx, "logins", tokenKey(token))
		if e != nil || time.Now().After(l.Expires) {
			return ErrUnauthorized
		}
		u, e = active(tx, l.UserID)
		return e
	})
	return u.User, err
}
func (s *Store) Logout(token string) error {
	return s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte("logins")).Delete([]byte(tokenKey(token))) })
}
func revoke(tx *bolt.Tx, id string) error {
	c := tx.Bucket([]byte("logins")).Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var l Login
		if json.Unmarshal(v, &l) == nil && l.UserID == id {
			if e := c.Delete(); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) ChangePassword(id, old, next string) error {
	if !passwordValid(next) {
		return errors.New("新密码需10–128位")
	}
	var before storedUser
	err := s.db.View(func(tx *bolt.Tx) error { var e error; before, e = active(tx, id); return e })
	if err != nil {
		return err
	}
	h, err := hash(old, before.PasswordSalt)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(h), []byte(before.Hash)) != 1 {
		return errors.New("原密码不正确")
	}
	salt := randomID()
	h, err = hash(next, salt)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		if u.Hash != before.Hash {
			return errors.New("密码已变更，请重新登录")
		}
		u.Hash = h
		u.PasswordSalt = salt
		if e = put(tx, "users", id, u); e != nil {
			return e
		}
		return revoke(tx, id)
	})
}
func audit(tx *bolt.Tx, actor, action, target, detail string) error {
	a := Audit{seq(tx, "audit"), actor, action, target, detail, time.Now()}
	return put(tx, "audit", a.ID, a)
}
func ledger(tx *bolt.Tx, u *storedUser, amount, frozen int64, kind, reference, note string) error {
	if u.Balance+amount < 0 || u.Frozen+frozen < 0 {
		return errors.New("可用余额不足")
	}
	if u.Balance+amount > maxMoney || u.Frozen+frozen > maxMoney {
		return errors.New("余额超出限额")
	}
	u.Balance += amount
	u.Frozen += frozen
	l := Ledger{seq(tx, "ledger"), u.ID, kind, amount, u.Balance, u.Frozen, reference, note, time.Now()}
	if e := put(tx, "ledger", l.ID, l); e != nil {
		return e
	}
	return put(tx, "users", u.ID, u)
}
func (s *Store) Settings() (Settings, error) {
	var v Settings
	e := s.db.View(func(tx *bolt.Tx) error { var e error; v, e = get[Settings](tx, "settings", "global"); return e })
	return v, e
}
func (s *Store) SaveSettings(actor string, cfg Settings) error {
	if cfg.RegistrationBonusCents < 0 || cfg.RegistrationBonusCents > 100000000 {
		return errors.New("注册赠送金额需在0至100万元之间")
	}
	totalRate := int64(0)
	for _, rate := range cfg.CommissionBPS {
		if rate < 0 || rate > 10000 {
			return errors.New("佣金比例无效")
		}
		totalRate += rate
	}
	if totalRate > 10000 || len(cfg.Announcement) > 2000 || len(cfg.Plays) != len(games.All()) {
		return errors.New("设置参数不正确")
	}
	seen := map[string]bool{}
	for _, p := range cfg.Plays {
		if p.UserIssueLimitCents < p.UnitCents || p.UserIssueLimitCents > 100000000 {
			return errors.New("单用户单期上限必须不低于起投金额，且不超过100万元")
		}
		if p.MaxStakeCents < p.UnitCents || p.MaxStakeCents > 100000000 {
			return errors.New("单笔投注上限必须不低于起投金额，且不超过100万元")
		}
		_, ok := games.Find(p.SessionID)
		if !ok || seen[p.SessionID] || p.UnitCents < 1 || p.UnitCents > 1000000 || p.MaxMultiplier < 1 || p.MaxMultiplier > 999 || p.CloseSeconds != games.ClosedWindowSeconds {
			return errors.New("玩法参数不正确")
		}
		seen[p.SessionID] = true
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		old, e := get[Settings](tx, "settings", "global")
		if e != nil {
			return e
		}
		detail, _ := json.Marshal(map[string]any{"before": old, "after": cfg})
		if e = put(tx, "settings", "global", cfg); e != nil {
			return e
		}
		return audit(tx, actor, "settings.update", "global", string(detail))
	})
}
func (s *Store) SetStatus(actor, id, status string) error {
	if status != "active" && status != "disabled" {
		return errors.New("无效状态")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		u, e := get[storedUser](tx, "users", id)
		if e != nil {
			return e
		}
		if u.Role == "admin" {
			return errors.New("不能在用户列表停用管理员")
		}
		u.Status = status
		if e = put(tx, "users", id, u); e != nil {
			return e
		}
		if status == "disabled" {
			if e = revoke(tx, id); e != nil {
				return e
			}
		}
		return audit(tx, actor, "user.status", id, status)
	})
}
func validKey(key string) bool { return len(key) >= 16 && len(key) <= 100 }
func idem(tx *bolt.Tx, scope, key string, body any) (string, error) {
	if !validKey(key) {
		return "", errors.New("缺少有效的幂等请求编号")
	}
	raw := tx.Bucket([]byte("keys")).Get([]byte(scope + key))
	if raw == nil {
		return "", nil
	}
	var v struct {
		Digest string
		ID     string
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return "", e
	}
	b, _ := json.Marshal(body)
	if v.Digest != tokenKey(string(b)) {
		return "", errors.New("请求编号已用于其他操作")
	}
	return v.ID, nil
}
func remember(tx *bolt.Tx, scope, key, id string, body any) error {
	b, _ := json.Marshal(body)
	return put(tx, "keys", scope+key, map[string]string{"Digest": tokenKey(string(b)), "ID": id})
}
func (s *Store) Adjust(actor, id string, amount int64, note, key string) error {
	if amount == 0 || amount > 100000000 || amount < -100000000 || len(strings.TrimSpace(note)) < 2 || len(note) > 500 {
		return errors.New("调整金额或原因不正确")
	}
	body := []any{id, amount, note}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		ref, e := idem(tx, "adjust:"+actor, key, body)
		if e != nil {
			return e
		}
		if ref != "" {
			return nil
		}
		u, e := get[storedUser](tx, "users", id)
		if e != nil {
			return e
		}
		ref = randomID()
		if e = ledger(tx, &u, amount, 0, "admin_adjust", ref, note); e != nil {
			return e
		}
		if e = remember(tx, "adjust:"+actor, key, ref, body); e != nil {
			return e
		}
		return audit(tx, actor, "balance.adjust", id, fmt.Sprintf("%d分；%s", amount, note))
	})
}
func (s *Store) CreateRequest(id string, in RequestInput) (WalletRequest, error) {
	var out WalletRequest
	if (in.Kind != "deposit" && in.Kind != "withdraw") || in.Amount < 100 || in.Amount > 100000000 || len(strings.TrimSpace(in.Note)) < 2 || len(in.Note) > 500 {
		return out, errors.New("请输入有效金额（1–1,000,000元）和说明")
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		ref, e := idem(tx, "request:"+id, in.Key, in)
		if e != nil {
			return e
		}
		if ref != "" {
			out, e = get[WalletRequest](tx, "requests", ref)
			return e
		}
		out = WalletRequest{ID: seq(tx, "requests"), UserID: id, Username: u.Username, Kind: in.Kind, Amount: in.Amount, Note: in.Note, Status: "pending", CreatedAt: time.Now()}
		if in.Kind == "withdraw" {
			if e = ledger(tx, &u, -in.Amount, in.Amount, "withdraw_hold", out.ID, in.Note); e != nil {
				return e
			}
		}
		if e = put(tx, "requests", out.ID, out); e != nil {
			return e
		}
		return remember(tx, "request:"+id, in.Key, out.ID, in)
	})
	return out, err
}
func (s *Store) ReviewRequest(actor, id, decision, note string) error {
	if (decision != "approved" && decision != "rejected") || len(strings.TrimSpace(note)) < 2 || len(note) > 500 {
		return errors.New("请填写审核说明")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		r, e := get[WalletRequest](tx, "requests", id)
		if e != nil {
			return e
		}
		if r.Status != "pending" {
			return errors.New("该申请已审核")
		}
		u, e := get[storedUser](tx, "users", r.UserID)
		if e != nil {
			return e
		}
		if decision == "approved" && u.Status != "active" {
			return errors.New("账户已停用，请先恢复或驳回申请")
		}
		amount, frozen := int64(0), int64(0)
		if r.Kind == "deposit" && decision == "approved" {
			amount = r.Amount
		}
		if r.Kind == "withdraw" {
			frozen = -r.Amount
			if decision == "rejected" {
				amount = r.Amount
			}
		}
		if amount != 0 || frozen != 0 {
			if e = ledger(tx, &u, amount, frozen, r.Kind+"_"+decision, id, note); e != nil {
				return e
			}
		}
		r.Status = decision
		r.ReviewNote = note
		if e = put(tx, "requests", id, r); e != nil {
			return e
		}
		return audit(tx, actor, "wallet."+decision, id, note)
	})
}
func validateNumbers(g games.Session, n []int) bool {
	count, low, high, unique := 5, 0, 9, false
	switch g.GameID {
	case "ssc":
		unique = true
	case "k3":
		count = 3
		low = 1
		high = 6
	case "pk10":
		count = 10
		low = 1
		high = 10
		unique = true
	case "lhc":
		count = 6
		low = 1
		high = 49
		unique = true
	case "pc28":
		count = 1
		high = 27
	}
	if len(n) != count {
		return false
	}
	seen := map[int]bool{}
	for _, v := range n {
		if v < low || v > high || (unique && seen[v]) {
			return false
		}
		seen[v] = true
	}
	return true
}
func (s *Store) PlaceOrder(id string, in OrderInput, now time.Time) (Order, error) {
	var out Order
	g, ok := games.Find(in.SessionID)
	if !ok || !validateNumbers(g, in.Numbers) {
		return out, errors.New("场次或所选号码不正确")
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		// Production callers sample time after acquiring the write transaction.
		if now.IsZero() {
			now = time.Now()
		}
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		ref, e := idem(tx, "order:"+id, in.Key, in)
		if e != nil {
			return e
		}
		if ref != "" {
			out, e = get[Order](tx, "orders", ref)
			return e
		}
		cfg, e := get[Settings](tx, "settings", "global")
		if e != nil {
			return e
		}
		var p PlaySetting
		for _, v := range cfg.Plays {
			if v.SessionID == g.ID {
				p = v
			}
		}
		if in.UnitCents != p.UnitCents {
			return errors.New("单注金额已变更，请刷新后重新确认")
		}
		if !p.Enabled {
			return errors.New("该玩法已暂停")
		}
		due := g.Next(now)
		if in.Issue != due.Format(model.IssueLayout) || !g.BettingOpen(now) {
			return errors.New("本期已封盘或期号已更新，请刷新")
		}
		cost, e := orderStake(p, in.AmountCents, in.Multiplier)
		if e != nil {
			return e
		}
		if e = reserveIssueStake(tx, id, g.ID, in.Issue, cost, p.UserIssueLimitCents); e != nil {
			return e
		}
		out = Order{ID: seq(tx, "orders"), UserID: id, Username: u.Username, SessionID: g.ID, SessionName: g.Name, GameName: g.GameName, Issue: in.Issue, Numbers: in.Numbers, Multiplier: in.Multiplier, Cost: cost, Status: "pending", InviterID: u.InviterID, CommissionBPS: cfg.CommissionBPS, CreatedAt: now}
		parent := u.InviterID
		for level := 0; level < 3 && parent != ""; level++ {
			up, err := get[storedUser](tx, "users", parent)
			if err != nil {
				return err
			}
			out.Ancestors = append(out.Ancestors, up.ID)
			parent = up.InviterID
		}
		if e = ledger(tx, &u, -out.Cost, 0, "order", out.ID, g.GameName+g.Name); e != nil {
			return e
		}
		if e = put(tx, "orders", out.ID, out); e != nil {
			return e
		}
		return remember(tx, "order:"+id, in.Key, out.ID, in)
	})
	return out, err
}

// Settlement is reviewed by an administrator. The caller must verify a scheduled source result exists.
func (s *Store) SettleOrder(actor, id, decision, note string, payout int64, resultAvailable bool) error {
	if (decision != "settled" && decision != "refunded") || payout < 0 || payout > 100000000 || len(strings.TrimSpace(note)) < 2 || len(note) > 500 {
		return errors.New("结算参数或说明不正确")
	}
	if decision == "refunded" && payout != 0 {
		return errors.New("撤销订单不可设置派彩金额")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		o, e := get[Order](tx, "orders", id)
		if e != nil {
			return e
		}
		if o.Status != "pending" {
			return errors.New("订单已处理，请勿重复结算")
		}
		if o.MarketID != "" && decision == "settled" {
			return errors.New("该订单使用锁定赔率自动结算，不能手填派彩")
		}
		if decision == "settled" && !resultAvailable {
			return errors.New("尚无定时开奖结果，暂不可结算")
		}
		u, e := get[storedUser](tx, "users", o.UserID)
		if e != nil {
			return e
		}
		amount := payout
		if decision == "refunded" {
			if e = releaseIssueStake(tx, o); e != nil {
				return e
			}
			amount = o.Cost
		}
		if amount > 0 {
			if e = ledger(tx, &u, amount, 0, "order_"+decision, id, note); e != nil {
				return e
			}
		}
		o.Status = decision
		o.Payout = payout
		o.Note = note
		if e = put(tx, "orders", id, o); e != nil {
			return e
		}
		if decision == "settled" {
			if e = createCommissions(tx, o); e != nil {
				return e
			}
		}
		if o.MarketID != "" {
			if e = tx.Bucket([]byte("pending_markets")).Delete([]byte(id)); e != nil {
				return e
			}
		}

		return audit(tx, actor, "order."+decision, id, fmt.Sprintf("派彩%d分；%s", payout, note))
	})
}
func (s *Store) Order(id string) (Order, error) {
	var o Order
	e := s.db.View(func(tx *bolt.Tx) error { var e error; o, e = get[Order](tx, "orders", id); return e })
	return o, e
}
func (s *Store) Claim(id string) (int64, error) {
	var amount int64
	err := s.db.Update(func(tx *bolt.Tx) error {
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		items, e := list[Commission](tx, "commissions")
		if e != nil {
			return e
		}
		for _, c := range items {
			if c.UserID == id && c.Status == "available" {
				amount += c.Amount
				c.Status = "claimed"
				if e = put(tx, "commissions", c.ID, c); e != nil {
					return e
				}
			}
		}
		if amount == 0 {
			return errors.New("暂无可领取佣金")
		}
		return ledger(tx, &u, amount, 0, "commission", randomID(), "三级邀请佣金领取")
	})
	return amount, err
}
