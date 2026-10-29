package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func enabledUSDTSettings(t *testing.T, s *Store) PaymentSettings {
	t.Helper()
	cfg, err := s.PaymentSettings("admin")
	must(t, err)
	cfg.Enabled = true
	cfg.Network = "TRC20"
	cfg.Address = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	cfg.RateYuan = "7.005"
	cfg.MinAmountCents = 100
	cfg.MaxAmountCents = 100000000
	cfg, err = s.SavePaymentSettings("admin", cfg)
	must(t, err)
	return cfg
}

func usdtSubmission(cfg PaymentSettings, n int) WalletSubmission {
	transactionID := fmt.Sprintf("%064x", n)
	if cfg.Network == "ERC20" {
		transactionID = "0x" + transactionID
	}
	return WalletSubmission{RequestInput: RequestInput{Kind: "deposit", Note: "核对链上转账", Key: randomID()}, PaymentMethod: "usdt", USDTAmount: "3", TransactionID: transactionID, SettingsVersion: cfg.Version}
}

func sameWalletRequest(t *testing.T, a, b WalletRequest) bool {
	t.Helper()
	x, err := json.Marshal(a)
	must(t, err)
	y, err := json.Marshal(b)
	must(t, err)
	return bytes.Equal(x, y)
}

func TestUSDTPaymentSettingsVersionPrivacyAndIsolation(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	cfg, err := s.PaymentSettings("admin")
	must(t, err)
	if cfg != (PaymentSettings{Version: 1}) {
		t.Fatalf("new database has configured payment defaults: %+v", cfg)
	}
	if _, err = s.PaymentSettings("member"); !errors.Is(err, ErrForbidden) {
		t.Fatal("member read private payment settings", err)
	}
	public, err := s.DepositPaymentSettings("member")
	must(t, err)
	raw, err := json.Marshal(public)
	must(t, err)
	if string(raw) != `{"version":1,"enabled":false}` {
		t.Fatal("unconfigured public response exposes transfer data", string(raw))
	}
	invalid := cfg
	invalid.Enabled = true
	if _, err = s.SavePaymentSettings("admin", invalid); err == nil {
		t.Fatal("enabled incomplete settings")
	}
	if _, err = s.SavePaymentSettings("member", cfg); !errors.Is(err, ErrForbidden) {
		t.Fatal("member edited payment settings", err)
	}
	legacy, err := s.Settings()
	must(t, err)
	cfg = enabledUSDTSettings(t, s)
	if cfg.Version != 2 {
		t.Fatal("version not advanced")
	}
	if _, err = s.SavePaymentSettings("admin", PaymentSettings{Version: 1}); err == nil {
		t.Fatal("stale payment form overwrote settings")
	}
	must(t, s.SaveSettings("admin", legacy))
	got, err := s.PaymentSettings("admin")
	must(t, err)
	if got != cfg {
		t.Fatal("general site settings overwrote payment settings")
	}
	public, err = s.DepositPaymentSettings("member")
	must(t, err)
	if public != cfg {
		t.Fatal("enabled payment settings missing")
	}
	cfg.Enabled = false
	cfg, err = s.SavePaymentSettings("admin", cfg)
	must(t, err)
	public, err = s.DepositPaymentSettings("member")
	must(t, err)
	if public != (PaymentSettings{Version: cfg.Version}) {
		t.Fatal("disabled payment settings expose a receiving address")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	got, err = s.PaymentSettings("admin")
	must(t, err)
	if got != cfg {
		t.Fatal("reopen replaced configured settings")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		rows, e := list[Audit](tx, "audit")
		if e != nil {
			return e
		}
		count := 0
		for _, row := range rows {
			if row.Action == "payment_settings.update" {
				count++
				if !strings.Contains(row.Detail, `"before"`) || !strings.Contains(row.Detail, `"after"`) {
					t.Fatal("payment audit missing before/after")
				}
			}
		}
		if count != 2 {
			t.Fatalf("expected 2 successful setting audits, got %d", count)
		}
		return nil
	}))
}

func TestUSDTSettingsAndFixedPrecisionValidation(t *testing.T) {
	for _, tc := range []struct {
		quantity, rate string
		want           int64
	}{
		{"1", "1.005", 101}, {"1", "1.004999", 100}, {"100", "7.231234", 72312},
		{"3", "7.005", 2102}, {"1000000", "0.000001", 100}, {"1000000", "1", 100000000},
		{"1", "1000000", 100000000}, {"1", "0.995", 100},
		{"1", "0.994999", 0}, {"1000001", "1", 0}, {"9223372036854775807", "1000000", 0},
		{"9223372036854775808", "1", 0}, {"1.5", "7", 0}, {"01", "7", 0}, {"0", "7", 0},
		{"-1", "7", 0}, {"1e3", "7", 0}, {" 1", "7", 0}, {"1", "NaN", 0},
		{"1", "0", 0}, {"1", "-7", 0}, {"1", "7e0", 0}, {"1", "7.0000001", 0}, {"1", "1000000.000001", 0},
	} {
		t.Run(tc.quantity+"_"+tc.rate, func(t *testing.T) {
			got, err := usdtAmountCents(tc.quantity, tc.rate)
			if tc.want == 0 && err == nil || tc.want != 0 && (err != nil || got != tc.want) {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
	s, _, _ := fundsFixture(t)
	defer s.Close()
	cfg := enabledUSDTSettings(t, s)
	for _, mutate := range []func(*PaymentSettings){
		func(c *PaymentSettings) { c.Network = "BEP20" },
		func(c *PaymentSettings) { c.Address = "" },
		func(c *PaymentSettings) { c.Address = "0x" + strings.Repeat("a", 40) },
		func(c *PaymentSettings) { c.Address = "T" + strings.Repeat("0", 33) },
		func(c *PaymentSettings) { c.RateYuan = "" },
		func(c *PaymentSettings) { c.RateYuan = "0" },
		func(c *PaymentSettings) { c.MinAmountCents = 99 },
		func(c *PaymentSettings) { c.MinAmountCents = 200; c.MaxAmountCents = 100 },
		func(c *PaymentSettings) { c.MaxAmountCents = 100000001 },
	} {
		bad := cfg
		mutate(&bad)
		if _, err := s.SavePaymentSettings("admin", bad); err == nil {
			t.Fatalf("saved invalid settings: %+v", bad)
		}
	}
	unchanged, err := s.PaymentSettings("admin")
	must(t, err)
	if unchanged != cfg {
		t.Fatal("failed setting validation wrote changes")
	}
}

func TestUSDTRequestSnapshotsRetryAndManualCredit(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	cfg := enabledUSDTSettings(t, s)
	in := usdtSubmission(cfg, 0xabcdef)
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if r.Amount != 2102 || r.PaymentNetwork != "TRC20" || r.PaymentAddress != cfg.Address || r.RateYuan != cfg.RateYuan || r.USDTAmount != "3" || r.TransactionID != in.TransactionID || r.SettingsVersion != cfg.Version || r.Kind != "deposit" || r.Status != "pending" {
		t.Fatalf("incorrect immutable snapshot: %+v", r)
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	pending, err := s.PendingRequests("admin")
	must(t, err)
	if pending.DepositCount != 1 || pending.DepositAmount != 2102 || pending.Items[0].TransactionID != r.TransactionID {
		t.Fatal("USDT missing from manual funds queue")
	}
	rows, err := s.AdminData("admin", "requests", r.TransactionID, 1)
	must(t, err)
	if len(rows["items"].([]any)) != 1 {
		t.Fatal("admin cannot search full transaction ID")
	}
	changed := in
	changed.USDTAmount = "4"
	if _, err = s.SubmitWalletRequest("member", changed); err == nil {
		t.Fatal("reused idempotency key accepted a different amount")
	}
	duplicate := in
	duplicate.Key = randomID()
	duplicate.TransactionID = strings.ToUpper(duplicate.TransactionID)
	if _, err = s.SubmitWalletRequest("member", duplicate); err == nil {
		t.Fatal("upper-case transaction hash evaded duplicate check")
	}
	cfg.Network, cfg.Address, cfg.RateYuan = "ERC20", "0x"+strings.Repeat("a", 40), "9.999999"
	cfg, err = s.SavePaymentSettings("admin", cfg)
	must(t, err)
	if _, err = s.SubmitWalletRequest("member", usdtSubmission(PaymentSettings{Version: cfg.Version - 1}, 2)); err == nil {
		t.Fatal("stale rate quote accepted")
	}
	cfg.Enabled = false
	_, err = s.SavePaymentSettings("admin", cfg)
	must(t, err)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	retry, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if !sameWalletRequest(t, retry, r) {
		t.Fatal("retry after settings change/restart changed snapshot", retry, r)
	}
	if _, err = s.SubmitWalletRequest("member", usdtSubmission(cfg, 3)); err == nil {
		t.Fatal("created deposit while disabled")
	}
	if err = s.ReviewRequest("member", r.ID, "approved", "不是管理员"); !errors.Is(err, ErrForbidden) {
		t.Fatal("member approved own request", err)
	}
	must(t, s.ReviewRequest("admin", r.ID, "approved", "人工核对链上网络地址数量与交易确认"))
	if err = s.ReviewRequest("admin", r.ID, "approved", "重复审核"); err == nil {
		t.Fatal("credited twice")
	}
	assertFunds(t, s, "member", 102102, 0, 0)
	account, err := s.Account("member")
	must(t, err)
	ledger := account["ledger"].([]Ledger)
	if len(ledger) != 1 || ledger[0].Amount != 2102 || ledger[0].Reference != r.ID || ledger[0].Kind != "deposit_approved" {
		t.Fatal("wrong deposit ledger", ledger)
	}
}

func TestUSDTConcurrentIdempotencyDuplicateAndReview(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	cfg := enabledUSDTSettings(t, s)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return put(tx, "users", "other", storedUser{User: User{ID: "other", Username: "other", Role: "member", Status: "active"}})
	}))
	in := usdtSubmission(cfg, 10)
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.SubmitWalletRequest("member", in)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && got != id {
			t.Fatal("idempotent concurrent requests created multiple rows")
		}
		id = got
	}
	if id == "" {
		t.Fatal("no successful requests")
	}
	duplicate := in
	duplicate.Key = randomID()
	if _, err := s.SubmitWalletRequest("other", duplicate); err == nil {
		t.Fatal("different account claimed pending transaction")
	}
	var approved atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.ReviewRequest("admin", id, "approved", "核对入账") == nil {
				approved.Add(1)
			}
		}()
	}
	wg.Wait()
	if approved.Load() != 1 {
		t.Fatalf("successful reviews: %d", approved.Load())
	}
	assertFunds(t, s, "member", 102102, 0, 0)
	if _, err := s.SubmitWalletRequest("other", duplicate); err == nil {
		t.Fatal("different account reused approved transaction")
	}
	var successes atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := "member"
			if i%2 == 1 {
				user = "other"
			}
			if _, err := s.SubmitWalletRequest(user, usdtSubmission(cfg, 20)); err == nil {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("same hash with separate keys/accounts accepted %d times", successes.Load())
	}
}

func TestUSDTRejectedCorrectionAndNetworkScopedTransactions(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	cfg := enabledUSDTSettings(t, s)
	in := usdtSubmission(cfg, 12)
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	must(t, s.ReviewRequest("admin", r.ID, "rejected", "数量填错，重新申请"))
	corrected := in
	corrected.Key, corrected.USDTAmount = randomID(), "4"
	r2, err := s.SubmitWalletRequest("member", corrected)
	must(t, err)
	if r.ID == r2.ID || r2.Amount != 2802 {
		t.Fatal("correction did not create a new request")
	}
	retry, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if retry.ID != r.ID || retry.Status != "rejected" {
		t.Fatal("old key did not retain rejected request")
	}
	if err = s.ReviewRequest("admin", r.ID, "approved", "旧申请不可通过"); err == nil {
		t.Fatal("approved rejected request")
	}
	must(t, s.ReviewRequest("admin", r2.ID, "approved", "核对更正申请"))
	cfg.Network, cfg.Address = "ERC20", "0x"+strings.Repeat("a", 40)
	cfg, err = s.SavePaymentSettings("admin", cfg)
	must(t, err)
	onOtherNetwork, err := s.SubmitWalletRequest("member", usdtSubmission(cfg, 12))
	must(t, err)
	if onOtherNetwork.TransactionID != "0x"+in.TransactionID || onOtherNetwork.PaymentNetwork != "ERC20" {
		t.Fatal("incorrect ERC20 full hash")
	}
	must(t, s.ReviewRequest("admin", onOtherNetwork.ID, "approved", "核对另一网络的独立交易"))
	assertFunds(t, s, "member", 104904, 0, 0)
}

func TestUSDTInvalidRequestsDoNotWriteAndApprovalChecksClaim(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	cfg := enabledUSDTSettings(t, s)
	for _, mutate := range []func(*WalletSubmission){
		func(in *WalletSubmission) { in.Amount = 999999 },
		func(in *WalletSubmission) { in.Kind = "withdraw" },
		func(in *WalletSubmission) { in.PaymentMethod = "unknown" },
		func(in *WalletSubmission) { in.PaymentMethod = "" },
		func(in *WalletSubmission) { in.USDTAmount = "1.1" },
		func(in *WalletSubmission) { in.USDTAmount = "9999999999999999999" },
		func(in *WalletSubmission) { in.SettingsVersion-- },
		func(in *WalletSubmission) { in.Key = "short" },
		func(in *WalletSubmission) { in.TransactionID = "abcdz" },
		func(in *WalletSubmission) { in.TransactionID = "abcde" },
		func(in *WalletSubmission) { in.TransactionID = strings.Repeat("a", 63) },
		func(in *WalletSubmission) { in.TransactionID = strings.Repeat("a", 65) },
		func(in *WalletSubmission) { in.TransactionID = "0x" + in.TransactionID },
		func(in *WalletSubmission) { in.TransactionID = strings.Repeat("z", 64) },
		func(in *WalletSubmission) { in.Note = strings.Repeat("a", 501) },
		func(in *WalletSubmission) { in.WithdrawalPassword = "123456" },
	} {
		in := usdtSubmission(cfg, 50)
		mutate(&in)
		if _, err := s.SubmitWalletRequest("member", in); err == nil {
			t.Fatalf("invalid submission accepted: %+v", in)
		}
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, bucket := range []string{"requests", "payment_transactions", "keys", "ledger"} {
			if tx.Bucket([]byte(bucket)).Stats().KeyN != 0 {
				t.Fatal("invalid submission left persistent state", bucket)
			}
		}
		return nil
	}))
	cfg.MinAmountCents, cfg.MaxAmountCents = 1000, 2000
	cfg, err := s.SavePaymentSettings("admin", cfg)
	must(t, err)
	for _, quantity := range []string{"1", "3"} {
		in := usdtSubmission(cfg, 50)
		in.USDTAmount = quantity
		if _, err = s.SubmitWalletRequest("member", in); err == nil {
			t.Fatal("configured minimum/maximum bypassed")
		}
	}
	in := usdtSubmission(cfg, 50)
	in.USDTAmount, in.Note = "2", ""
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("payment_transactions")).Put([]byte(usdtTransactionKey(r.PaymentNetwork, r.TransactionID)), []byte("another-request"))
	}))
	if err = s.ReviewRequest("admin", r.ID, "approved", "必须验证交易唯一关联"); err == nil {
		t.Fatal("credited request without its unique transaction claim")
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}

func TestUSDTPaymentHTTPContractAndLegacyRequests(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var adminToken, memberToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var err error
		adminToken, err = newLogin(tx, "admin")
		if err != nil {
			return err
		}
		memberToken, err = newLogin(tx, "member")
		return err
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		must(t, err)
		r := httptest.NewRequest(method, "http://localhost/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("payment response may be cached")
		}
		return w
	}
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{"wallet/payment-settings", "", 401}, {"wallet/payment-settings", memberToken, 200},
		{"admin/payment-settings", memberToken, 403}, {"admin/payment-settings", adminToken, 200},
	} {
		if w := request("GET", tc.path, tc.token, nil); w.Code != tc.status {
			t.Fatalf("GET %s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	cfg := enabledUSDTSettings(t, s)
	cfg.RateYuan = "7.25"
	if w := request("PUT", "admin/payment-settings", memberToken, cfg); w.Code != 403 {
		t.Fatal("member changed payment settings", w.Body.String())
	}
	w := request("PUT", "admin/payment-settings", adminToken, cfg)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var saved struct {
		Data PaymentSettings `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &saved))
	if saved.Data.Version != cfg.Version+1 {
		t.Fatal("PUT did not return the new configuration")
	}
	in := usdtSubmission(saved.Data, 123)
	w = request("POST", "wallet/requests", memberToken, in)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		Data WalletRequest `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &created))
	if created.Data.Amount != 2175 || created.Data.PaymentMethod != "usdt" || created.Data.PaymentAddress != cfg.Address {
		t.Fatal("HTTP did not return snapshot", w.Body.String())
	}
	forged := map[string]any{"kind": "deposit", "payment_method": "usdt", "usdt_amount": "3", "transaction_id": strings.Repeat("b", 64), "settings_version": saved.Data.Version, "rate_yuan": "99", "key": randomID()}
	if w = request("POST", "wallet/requests", memberToken, forged); w.Code != 400 {
		t.Fatal("accepted client rate override", w.Body.String())
	}
	legacy := RequestInput{Kind: "deposit", Amount: 1000, Note: "原有人工充值", Key: randomID()}
	r, err := s.CreateRequest("member", legacy)
	must(t, err)
	w = request("POST", "wallet/requests", memberToken, legacy)
	if w.Code != 200 {
		t.Fatal("legacy idempotency broken by extended input", w.Body.String())
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &created))
	if created.Data.ID != r.ID {
		t.Fatal("legacy retry created new request")
	}
	withdrawal := RequestInput{Kind: "withdraw", Amount: 100, Note: "旧用户须设置提现密码", Key: randomID()}
	if w = request("POST", "wallet/requests", memberToken, withdrawal); w.Code != 400 {
		t.Fatal("HTTP withdrawal allowed without configured password", w.Body.String())
	}
	// Internal historical call remains compatible with existing settlement tests.
	r, err = s.CreateRequest("member", withdrawal)
	must(t, err)
	if w = request("POST", "wallet/requests", memberToken, withdrawal); w.Code != 200 {
		t.Fatal("accepted withdrawal retry unnecessarily rechecked PIN", w.Body.String())
	}
	assertFunds(t, s, "member", 99900, 0, 100)
	must(t, s.ReviewRequest("admin", r.ID, "rejected", "核验后退回冻结"))
	assertFunds(t, s, "member", 100000, 0, 0)
}

func TestUSDTOldDatabaseMigrationPreservesLegacyWallet(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	in := RequestInput{Kind: "deposit", Amount: 500, Note: "旧库充值", Key: randomID()}
	r, err := s.CreateRequest("member", in)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte("payment_settings")); err != nil {
			return err
		}
		return tx.DeleteBucket([]byte("payment_transactions"))
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	cfg, err := s.PaymentSettings("admin")
	must(t, err)
	if cfg != (PaymentSettings{Version: 1}) {
		t.Fatal("migration enabled or configured payments")
	}
	retry, err := s.SubmitWalletRequest("member", WalletSubmission{RequestInput: in})
	must(t, err)
	if !sameWalletRequest(t, retry, r) {
		t.Fatal("migration changed old wallet request")
	}
	must(t, s.ReviewRequest("admin", r.ID, "approved", "核对旧申请入账"))
	assertFunds(t, s, "member", 100500, 0, 0)
}

func TestUSDTWalletWithdrawalPINAndSafeReplay(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	const pin = "483927"
	salt := randomID()
	encoded, err := hash(pin, salt)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "member")
		if err != nil {
			return err
		}
		u.WithdrawalPasswordSet = true
		u.WithdrawalHash, u.WithdrawalSalt = encoded, salt
		return put(tx, "users", u.ID, u)
	}))
	cfg, err := s.WithdrawalSettings("member", false)
	must(t, err)
	in := WalletSubmission{RequestInput: RequestInput{Kind: "withdraw", Amount: 3000, Note: "提现到账资料", Key: randomID()}, WithdrawalPassword: "000000", WithdrawalConfirmation: "000000", WithdrawalVersion: cfg.Version, WithdrawalRateVersion: cfg.RateVersion}
	if _, err = s.SubmitWalletRequest("member", in); err == nil {
		t.Fatal("incorrect withdrawal PIN accepted")
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	in.WithdrawalPassword = pin
	in.WithdrawalConfirmation = ""
	if _, err = s.SubmitWalletRequest("member", in); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	in.WithdrawalConfirmation = "000000"
	if _, err = s.SubmitWalletRequest("member", in); err == nil {
		t.Fatal("mismatched confirmation accepted")
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	in.WithdrawalConfirmation = pin
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	assertFunds(t, s, "member", 97000, 0, 3000)
	// Lock future withdrawals after this request was accepted. A lost-response
	// retry must still return its original request without another PIN attempt.
	for i := 0; i < 5; i++ {
		if err = s.VerifyWithdrawalPassword("member", "000000"); err == nil {
			t.Fatal("incorrect PIN unexpectedly accepted")
		}
	}
	in.WithdrawalPassword = ""
	retry, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if !sameWalletRequest(t, retry, r) {
		t.Fatal("locked-account retry did not return the original request")
	}
	changed := in
	changed.Amount++
	if _, err = s.SubmitWalletRequest("member", changed); err == nil {
		t.Fatal("retry changed withdrawal amount without PIN")
	}
	in.Key, in.WithdrawalPassword = randomID(), pin
	if _, err = s.SubmitWalletRequest("member", in); err == nil {
		t.Fatal("a new request bypassed the withdrawal PIN lock")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, name := range []string{"requests", "keys", "ledger", "audit"} {
			if err := tx.Bucket([]byte(name)).ForEach(func(_, value []byte) error {
				if bytes.Contains(value, []byte(pin)) || bytes.Contains(value, []byte("withdrawal_password")) {
					t.Fatal("wallet storage persisted withdrawal credentials", name)
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}))
	assertFunds(t, s, "member", 97000, 0, 3000)
	must(t, s.ReviewRequest("admin", r.ID, "approved", "核对提现完成"))
	assertFunds(t, s, "member", 97000, 0, 0)
}
