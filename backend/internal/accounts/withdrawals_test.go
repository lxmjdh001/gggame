package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func withdrawalFixture(t *testing.T) (*Store, string) {
	t.Helper()
	s, path, _ := fundsFixture(t)
	salt := randomID()
	encoded, err := hash("483927", salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "member")
		if e != nil {
			return e
		}
		u.WithdrawalHash = encoded
		u.WithdrawalSalt = salt
		u.WithdrawalPasswordSet = true
		return put(tx, "users", u.ID, u)
	}))
	return s, path
}
func withdrawalInput(t *testing.T, s *Store, amount int64) WalletSubmission {
	t.Helper()
	cfg, err := s.WithdrawalSettings("member", false)
	must(t, err)
	return WalletSubmission{RequestInput: RequestInput{Kind: "withdraw", Amount: amount, Note: "收款核对信息", Key: randomID()}, WithdrawalPassword: "483927", WithdrawalConfirmation: "483927", WithdrawalVersion: cfg.Version, WithdrawalRateVersion: cfg.RateVersion}
}
func seedWager(t *testing.T, s *Store, deposit, reward, stake int64) string {
	t.Helper()
	var id string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "member")
		if e != nil {
			return e
		}
		if deposit > 0 {
			if e = ledger(tx, &u, deposit, 0, "deposit_approved", "fixture", "充值"); e != nil {
				return e
			}
		}
		if reward > 0 {
			if e = ledger(tx, &u, reward, 0, "registration_bonus", "fixture", "奖励"); e != nil {
				return e
			}
		}
		if stake > 0 {
			id = seq(tx, "orders")
			return put(tx, "orders", id, Order{ID: id, UserID: "member", Cost: stake, Status: "settled"})
		}
		return nil
	}))
	return id
}
func TestWithdrawalGateFeeResetAndNewActivity(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 1000, 5000)
	input := withdrawalInput(t, s, 10000)
	if _, err := s.SubmitWalletRequest("member", input); err == nil {
		t.Fatal("insufficient turnover accepted")
	}
	assertFunds(t, s, "member", 111000, 0, 0)
	before, err := s.Wagering("admin", "member")
	must(t, err)
	if before.Required != 11000 || before.Remaining != 6000 {
		t.Fatal(before)
	}
	edit := WagerEdit{Version: before.Version, Revision: before.Revision, Turnover: 11000, Note: "核实后调整打码", Key: randomID()}
	after, err := s.EditWagering("admin", "member", edit)
	must(t, err)
	if !after.Eligible {
		t.Fatal(after)
	}
	same, err := s.EditWagering("admin", "member", edit)
	must(t, err)
	if same.Turnover != 11000 {
		t.Fatal("duplicate admin edit", same)
	}
	r, err := s.SubmitWalletRequest("member", input)
	must(t, err)
	if r.Withdrawal.Fee != 1000 || r.Withdrawal.Net != 9000 {
		t.Fatal(r)
	}
	assertFunds(t, s, "member", 101000, 0, 10000)
	// Later deposits, bets and adjustments survive this request's approval.
	seedWager(t, s, 2000, 500, 2500)
	cfg, err := s.WithdrawalSettings("admin", true)
	must(t, err)
	cfg.FeeBPS = 2000
	cfg.RateYuan = "8"
	_, err = s.SaveWithdrawalSettings("admin", cfg)
	must(t, err)
	input.WithdrawalPassword = ""
	input.WithdrawalConfirmation = ""
	retry, err := s.SubmitWalletRequest("member", input)
	must(t, err)
	if retry.ID != r.ID || retry.Withdrawal.Fee != 1000 {
		t.Fatal("snapshot changed", retry)
	}
	must(t, s.ReviewRequest("admin", r.ID, "approved", "已核实出款90元"))
	assertFunds(t, s, "member", 103500, 0, 0)
	after, err = s.Wagering("admin", "member")
	must(t, err)
	if after.Required != 2500 || after.Turnover != 2500 {
		t.Fatal("lost activity during review", after)
	}
	if err = s.ReviewRequest("admin", r.ID, "approved", "重复审核"); err == nil {
		t.Fatal("double approval")
	}
	next := withdrawalInput(t, s, 1000)
	r2, err := s.SubmitWalletRequest("member", next)
	must(t, err)
	if r2.Withdrawal.Fee != 200 {
		t.Fatal("new fee not applied")
	}
	must(t, s.ReviewRequest("admin", r2.ID, "rejected", "驳回全额退还"))
	assertFunds(t, s, "member", 103500, 0, 0)
	after, err = s.Wagering("admin", "member")
	must(t, err)
	if after.Required != 2500 {
		t.Fatal("rejection reset turnover", after)
	}
}
func TestUSDTWithdrawalSnapshotConcurrentReplayAndRestart(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	seedWager(t, s, 10000, 0, 10000)
	in := withdrawalInput(t, s, 10000)
	in.PaymentMethod = "usdt"
	in.PayoutNetwork = "TRC20"
	in.PayoutAddress = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.SubmitWalletRequest("member", in)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for v := range ids {
		if id != "" && v != id {
			t.Fatal("multiple withdrawals")
		}
		id = v
	}
	if id == "" {
		t.Fatal("no request")
	}
	assertFunds(t, s, "member", 100000, 0, 10000)
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if r.Withdrawal.USDTAmount != "13.043478" || r.Withdrawal.RateYuan != "6.9" {
		t.Fatal(r.Withdrawal)
	}
	changed := in
	changed.PayoutNetwork = "ERC20"
	changed.PayoutAddress = "0x" + strings.Repeat("a", 40)
	if _, err = s.SubmitWalletRequest("member", changed); err == nil {
		t.Fatal("changed replay destination accepted")
	}
	cfg, err := s.WithdrawalSettings("admin", true)
	must(t, err)
	cfg.FeeBPS = 0
	cfg.USDTEnabled = false
	cfg.RateYuan = "9"
	_, err = s.SaveWithdrawalSettings("admin", cfg)
	must(t, err)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	in.WithdrawalPassword = ""
	in.WithdrawalConfirmation = ""
	retry, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if retry.Withdrawal.USDTAmount != "13.043478" || retry.ID != r.ID {
		t.Fatal("restart/rate change altered request")
	}
	must(t, s.ReviewRequest("admin", r.ID, "approved", "已向原地址出款"))
	w, err := s.Wagering("admin", "member")
	must(t, err)
	if w.Required != 0 || w.Turnover != 0 {
		t.Fatal("not reset", w)
	}
}
func TestWithdrawalRefundedBetsGreenChannelAndStaleEdits(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	order := seedWager(t, s, 10000, 0, 10000)
	in := withdrawalInput(t, s, 1000)
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		o, e := get[Order](tx, "orders", order)
		if e != nil {
			return e
		}
		o.Status = "refunded"
		return put(tx, "orders", o.ID, o)
	}))
	if err = s.ReviewRequest("admin", r.ID, "approved", "不能使用撤销打码"); err == nil {
		t.Fatal("cancelled turnover accepted at approval")
	}
	w, err := s.Wagering("admin", "member")
	must(t, err)
	if w.Turnover != 0 || w.Eligible {
		t.Fatal(w)
	}
	stale := WagerEdit{Version: w.Version, Revision: w.Revision, Turnover: 10000, Note: "调整", Key: randomID()}
	seedWager(t, s, 0, 0, 100)
	if _, err = s.EditWagering("admin", "member", stale); err == nil {
		t.Fatal("stale edit overwrote new wager")
	}
	w, err = s.Wagering("admin", "member")
	must(t, err)
	edit := WagerEdit{Version: w.Version, Revision: w.Revision, Turnover: 0, Green: true, Note: "开通绿色通道", Key: randomID()}
	if _, err = s.EditWagering("member", "member", edit); err != ErrForbidden {
		t.Fatal("member edited turnover", err)
	}
	_, err = s.EditWagering("admin", "member", edit)
	must(t, err)
	must(t, s.ReviewRequest("admin", r.ID, "approved", "绿色通道核实出款"))
	w, err = s.Wagering("admin", "member")
	must(t, err)
	if !w.Green || !w.Eligible {
		t.Fatal(w)
	}
	next := withdrawalInput(t, s, 1000)
	out, err := s.SubmitWalletRequest("member", next)
	must(t, err)
	if out.Withdrawal.Fee != 100 {
		t.Fatal("green channel waived fee")
	}
}
func TestWithdrawalSettingsAuthorizationAndHTTP(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	cfg, err := s.WithdrawalSettings("admin", true)
	must(t, err)
	if _, err = s.SaveWithdrawalSettings("member", cfg); err != ErrForbidden {
		t.Fatal(err)
	}
	stale := cfg
	cfg.FeeBPS = 9999
	_, err = s.SaveWithdrawalSettings("admin", cfg)
	must(t, err)
	if _, err = s.SaveWithdrawalSettings("admin", stale); err == nil {
		t.Fatal("stale settings accepted")
	}
	cfg.FeeBPS = 10000
	if _, err = s.SaveWithdrawalSettings("admin", cfg); err == nil {
		t.Fatal("100% fee accepted")
	}
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var e error; token, e = newLogin(tx, "member"); return e }))
	for _, tc := range []struct {
		path, token string
		status      int
	}{{"/wallet/withdrawal-settings", "", 401}, {"/wallet/withdrawal-settings", token, 200}, {"/admin/withdrawal-settings", token, 403}, {"/admin/users/admin/wagering", token, 403}} {
		r := httptest.NewRequest("GET", "http://localhost/api/v1"+tc.path, nil)
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
		if tc.status == 200 {
			var v struct {
				Data WithdrawalSettings `json:"data"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &v))
			if v.Data.FeeBPS != 9999 {
				t.Fatal(v)
			}
		}
	}
}

func TestWithdrawalOverlappingApprovalsAndERC20Rounding(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 1000, 0, 1000)
	a, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 105))
	must(t, err)
	if a.Withdrawal.Fee != 11 || a.Withdrawal.Net != 94 {
		t.Fatal("fee must round to nearest cent", a.Withdrawal)
	}
	seedWager(t, s, 2000, 0, 2000)
	input := withdrawalInput(t, s, 1000)
	input.PaymentMethod = "usdt"
	input.PayoutNetwork = "ERC20"
	input.PayoutAddress = "0x" + strings.Repeat("a", 40)
	b, err := s.SubmitWalletRequest("member", input)
	must(t, err)
	seedWager(t, s, 3000, 0, 3000)
	must(t, s.ReviewRequest("admin", b.ID, "approved", "先审核后申请的提现"))
	must(t, s.ReviewRequest("admin", a.ID, "approved", "后审核较早申请"))
	w, err := s.Wagering("admin", "member")
	must(t, err)
	if w.Required != 3000 || w.Turnover != 3000 {
		t.Fatal("older approval erased new cycle", w)
	}
	cfg, err := s.WithdrawalSettings("admin", true)
	must(t, err)
	cfg.USDTEnabled = false
	_, err = s.SaveWithdrawalSettings("admin", cfg)
	must(t, err)
	next := withdrawalInput(t, s, 1000)
	next.PaymentMethod = "usdt"
	next.PayoutNetwork = input.PayoutNetwork
	next.PayoutAddress = input.PayoutAddress
	if _, err = s.SubmitWalletRequest("member", next); err == nil {
		t.Fatal("disabled USDT channel accepted")
	}
}

func TestWithdrawalRateMigrationOncePreservesDepositSnapshots(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	cfg := enabledUSDTSettings(t, s)
	old, err := s.SubmitWalletRequest("member", usdtSubmission(cfg, 55))
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte("withdrawal_settings")).Delete([]byte("current")) }))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	pay, err := s.PaymentSettings("admin")
	must(t, err)
	if pay.RateYuan != "6.9" || pay.Address != cfg.Address || pay.Network != cfg.Network || pay.Version != cfg.Version+1 {
		t.Fatal("migration changed receiving settings", pay)
	}
	must(t, s.ReviewRequest("admin", old.ID, "approved", "历史充值按原汇率入账"))
	assertFunds(t, s, "member", 100000+old.Amount, 0, 0)
	pay.RateYuan = "8.1"
	pay, err = s.SavePaymentSettings("admin", pay)
	must(t, err)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	next, err := s.WithdrawalSettings("member", false)
	must(t, err)
	if next.RateYuan != "8.1" {
		t.Fatal("restart overwrote admin rate")
	}
}

func TestWageringCompleteHistoryAndAdjustmentAfterCancellation(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 21000, 0, 0)
	var pending string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 205; i++ {
			id := seq(tx, "orders")
			if e := put(tx, "orders", id, Order{ID: id, UserID: "member", Cost: 100, Status: "settled"}); e != nil {
				return e
			}
		}
		pending = seq(tx, "orders")
		if e := put(tx, "orders", pending, Order{ID: pending, UserID: "member", Cost: 500, Status: "pending"}); e != nil {
			return e
		}
		for _, o := range []Order{{UserID: "member", Cost: 99999, Status: "refunded"}, {UserID: "admin", Cost: 99999, Status: "settled"}} {
			o.ID = seq(tx, "orders")
			if e := put(tx, "orders", o.ID, o); e != nil {
				return e
			}
		}
		return nil
	}))
	w, e := s.Wagering("admin", "member")
	must(t, e)
	if w.Turnover != 21000 || !w.Eligible {
		t.Fatal("limited history or foreign/refunded wager counted", w)
	}
	_, e = s.EditWagering("admin", "member", WagerEdit{Version: w.Version, Revision: w.Revision, Turnover: 0, Note: "人工扣除打码", Key: randomID()})
	must(t, e)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		o, e := get[Order](tx, "orders", pending)
		if e != nil {
			return e
		}
		o.Status = "refunded"
		return put(tx, "orders", o.ID, o)
	}))
	seedWager(t, s, 0, 0, 100)
	w, e = s.Wagering("admin", "member")
	must(t, e)
	if w.Turnover != 100 {
		t.Fatal("negative adjustment carried a phantom debt", w)
	}
}
