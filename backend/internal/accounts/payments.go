package accounts

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Payment settings are deliberately independent of general site settings. A
// stale general-settings form cannot overwrite a receiving address or rate.
type PaymentSettings struct {
	Version             int64  `json:"version"`
	Enabled             bool   `json:"enabled"`
	Network             string `json:"network,omitempty"`
	Address             string `json:"address,omitempty"`
	RateYuan            string `json:"rate_yuan,omitempty"`
	MinAmountCents      int64  `json:"min_amount_cents,omitempty"`
	MaxAmountCents      int64  `json:"max_amount_cents,omitempty"`
	QRCodeDataURL       string `json:"qr_code_data_url,omitempty"`
	ManualQRCodeDataURL string `json:"manual_qr_code_data_url,omitempty"`
}

// Keep RequestInput unchanged: its JSON is also the digest for historical
// idempotent requests. Passwords are never persisted in requests or key records.
type WalletSubmission struct {
	WithdrawalVersion     int64  `json:"withdrawal_settings_version,omitempty"`
	WithdrawalRateVersion int64  `json:"withdrawal_rate_version,omitempty"`
	PayoutNetwork         string `json:"payout_network,omitempty"`
	PayoutAddress         string `json:"payout_address,omitempty"`
	RequestInput
	PaymentMethod          string `json:"payment_method,omitempty"`
	USDTAmount             string `json:"usdt_amount,omitempty"`
	TransactionID          string `json:"transaction_id,omitempty"`
	SettingsVersion        int64  `json:"settings_version,omitempty"`
	WithdrawalPassword     string `json:"withdrawal_password,omitempty"`
	WithdrawalConfirmation string `json:"withdrawal_password_confirmation,omitempty"`
}

var (
	usdtIntegerPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	usdtRatePattern    = regexp.MustCompile(`^(0|[1-9][0-9]{0,6})(\.[0-9]{1,6})?$`)
	tronAddressPattern = regexp.MustCompile(`^T[1-9A-HJ-NP-Za-km-z]{33}$`)
	ethAddressPattern  = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	transactionPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

func initializePayments(tx *bolt.Tx) error {
	for _, name := range []string{"payment_settings", "payment_transactions"} {
		if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
			return err
		}
	}
	if tx.Bucket([]byte("payment_settings")).Get([]byte("usdt")) == nil {
		return put(tx, "payment_settings", "usdt", PaymentSettings{Version: 1})
	}
	return nil
}

func usdtRateMicros(rate string) (int64, error) {
	if !usdtRatePattern.MatchString(rate) {
		return 0, errors.New("汇率必须为正数，每 USDT 人民币元最多保留 6 位小数")
	}
	parts := strings.SplitN(rate, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	fraction += strings.Repeat("0", 6-len(fraction))
	decimal, _ := strconv.ParseInt(fraction, 10, 64)
	micros := whole*1000000 + decimal
	if micros <= 0 || micros > 1000000*1000000 {
		return 0, errors.New("汇率必须大于 0 且不超过 1,000,000 元 / USDT")
	}
	return micros, nil
}

func validatePaymentSettings(cfg PaymentSettings) error {
	if err := validatePaymentQR(cfg.QRCodeDataURL); err != nil {
		return err
	}
	if err := validatePaymentQR(cfg.ManualQRCodeDataURL); err != nil {
		return err
	}
	if cfg.Version < 1 {
		return errors.New("请刷新收款设置后重试")
	}
	if cfg.Network != "" && cfg.Network != "TRC20" && cfg.Network != "ERC20" {
		return errors.New("仅支持 TRC20 或 ERC20 网络")
	}
	if cfg.Address != "" && !((cfg.Network == "TRC20" && tronAddressPattern.MatchString(cfg.Address)) || (cfg.Network == "ERC20" && ethAddressPattern.MatchString(cfg.Address))) {
		return errors.New("收款地址与所选网络格式不符")
	}
	if cfg.RateYuan != "" {
		if _, err := usdtRateMicros(cfg.RateYuan); err != nil {
			return err
		}
	}
	if cfg.MinAmountCents != 0 || cfg.MaxAmountCents != 0 {
		if cfg.MinAmountCents < 100 || cfg.MaxAmountCents < cfg.MinAmountCents || cfg.MaxAmountCents > 100000000 {
			return errors.New("单笔充值限额需满足 1 元 ≤ 最低金额 ≤ 最高金额 ≤ 1,000,000 元")
		}
	}
	if cfg.Enabled && (cfg.Network == "" || cfg.Address == "" || cfg.RateYuan == "" || cfg.MinAmountCents == 0 || cfg.MaxAmountCents == 0) {
		return errors.New("启用 USDT 前请完整设置网络、收款地址、汇率和限额")
	}
	return nil
}

func (s *Store) PaymentSettings(actor string) (PaymentSettings, error) {
	var out PaymentSettings
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		var err error
		out, err = get[PaymentSettings](tx, "payment_settings", "usdt")
		return err
	})
	return out, err
}

func (s *Store) DepositPaymentSettings(id string) (PaymentSettings, error) {
	var out PaymentSettings
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, err := active(tx, id); err != nil {
			return err
		}
		cfg, err := get[PaymentSettings](tx, "payment_settings", "usdt")
		if err != nil {
			return err
		}
		out = PaymentSettings{Version: cfg.Version}
		// Disabled or incomplete settings never expose a transferable address.
		if cfg.Enabled && validatePaymentSettings(cfg) == nil {
			out = cfg
			out.ManualQRCodeDataURL = "" // Legacy manual configuration is not offered by the USDT-only wallet.
		}
		return nil
	})
	return out, err
}

func (s *Store) SavePaymentSettings(actor string, cfg PaymentSettings) (PaymentSettings, error) {
	var out PaymentSettings
	cfg.Network = strings.ToUpper(strings.TrimSpace(cfg.Network))
	cfg.Address = strings.TrimSpace(cfg.Address)
	cfg.RateYuan = strings.TrimSpace(cfg.RateYuan)
	if err := validatePaymentSettings(cfg); err != nil {
		return out, err
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		old, err := get[PaymentSettings](tx, "payment_settings", "usdt")
		if err != nil {
			return err
		}
		if old.Version != cfg.Version {
			return errors.New("收款设置已更新，请刷新后重新保存")
		}
		if (old.Network != cfg.Network || old.Address != cfg.Address) && old.QRCodeDataURL != "" && old.QRCodeDataURL == cfg.QRCodeDataURL {
			return errors.New("更改收款地址或网络后，请重新上传对应二维码或移除旧二维码")
		}
		cfg.Version++
		if cfg.Version <= old.Version {
			return errors.New("收款设置版本已达上限")
		}
		if err = put(tx, "payment_settings", "usdt", cfg); err != nil {
			return err
		}
		detail, err := json.Marshal(map[string]any{"before": paymentSettingsForAudit(old), "after": paymentSettingsForAudit(cfg)})
		if err != nil {
			return err
		}
		if err = audit(tx, actor, "payment_settings.update", "usdt", string(detail)); err != nil {
			return err
		}
		out = cfg
		return nil
	})
	return out, err
}

// Integer USDT times millionths of a yuan, rounded half up to the nearest cent.
// big.Int prevents quantity/rate multiplication from overflowing before bounds
// are checked. No floating-point or client-supplied yuan amount participates.
func usdtAmountCents(quantity, rate string) (int64, error) {
	if !usdtIntegerPattern.MatchString(quantity) {
		return 0, errors.New("USDT 数量必须为正整数")
	}
	amount, ok := new(big.Int).SetString(quantity, 10)
	if !ok || !amount.IsInt64() {
		return 0, errors.New("USDT 数量超出范围")
	}
	micros, err := usdtRateMicros(rate)
	if err != nil {
		return 0, err
	}
	amount.Mul(amount, big.NewInt(micros))
	amount.Add(amount, big.NewInt(5000))
	amount.Quo(amount, big.NewInt(10000))
	if !amount.IsInt64() || amount.Int64() < 100 || amount.Int64() > 100000000 {
		return 0, errors.New("折合充值金额必须在 1–1,000,000 元之间")
	}
	return amount.Int64(), nil
}

func usdtTransactionID(network, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch network {
	case "TRC20":
		if transactionPattern.MatchString(value) {
			return strings.ToLower(value), nil
		}
	case "ERC20":
		if strings.HasPrefix(value, "0x") && transactionPattern.MatchString(value[2:]) {
			return strings.ToLower(value), nil
		}
	}
	return "", errors.New("请填写完整交易 ID：TRC20 为64位十六进制字符，ERC20 为0x开头的66位交易哈希")
}

// Previously accepted suffix requests remain reviewable; only immutable stored
// requests may use this parser. New submissions require the full hash above.
func storedUSDTTransactionID(network, value string) (string, error) {
	value = strings.TrimSpace(value)
	if (network == "TRC20" || network == "ERC20") && len(value) == 5 {
		for _, c := range value {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return "", errors.New("请填写交易 ID 后5位（十六进制字符）")
			}
		}
		return strings.ToLower(value), nil
	}
	return usdtTransactionID(network, value)
}

// A suffix is only a manual reconciliation hint, never a globally unique hash.
func usdtRequestTransactionKey(network, transactionID, requestID string) string {
	if len(transactionID) == 5 {
		return network + ":suffix:" + requestID
	}
	return usdtTransactionKey(network, transactionID)
}

func usdtTransactionKey(network, transactionID string) string {
	return network + ":" + strings.TrimPrefix(strings.ToLower(transactionID), "0x")
}

// Deposits use USDT; withdrawals also accept the supported wallet accounts.
// Previously accepted manual
// requests remain reviewable and exact retries still resolve their original ID.
func (s *Store) SubmitUSDTWalletRequest(id string, in WalletSubmission) (WalletRequest, error) {
	if in.PaymentMethod == "usdt" || in.Kind == "withdraw" && walletWithdrawalMethod(in.PaymentMethod) {
		return s.SubmitWalletRequest(id, in)
	}
	var accepted bool
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, e := active(tx, id); e != nil {
			return e
		}
		ref, e := idem(tx, "request:"+id, in.Key, in.RequestInput)
		if e != nil {
			return e
		}
		if ref != "" {
			r, e := get[WalletRequest](tx, "requests", ref)
			if e != nil {
				return e
			}
			accepted = r.UserID == id && r.PaymentMethod == ""
		}
		return nil
	})
	if err != nil {
		return WalletRequest{}, err
	}
	if !accepted {
		return WalletRequest{}, errors.New("请选择支持的收款账户类型；充值仅支持USDT")
	}
	return s.SubmitWalletRequest(id, in)
}

func (s *Store) SubmitWalletRequest(id string, in WalletSubmission) (WalletRequest, error) {
	if in.Kind == "withdraw" {
		return s.submitWithdrawal(id, in)
	}
	if in.WithdrawalVersion != 0 || in.WithdrawalRateVersion != 0 || in.PayoutNetwork != "" || in.PayoutAddress != "" {
		return WalletRequest{}, errors.New("存款参数不正确")
	}
	if in.PaymentMethod == "usdt" {
		return s.createUSDTRequest(id, in)
	}
	if in.PaymentMethod != "" || in.USDTAmount != "" || in.SettingsVersion != 0 || in.WithdrawalPassword != "" || in.WithdrawalConfirmation != "" {
		return WalletRequest{}, errors.New("存款参数不正确")
	}
	return s.createRequest(id, in.RequestInput, in.TransactionID, true)
}

func (s *Store) createUSDTRequest(id string, in WalletSubmission) (WalletRequest, error) {
	var out WalletRequest
	if in.Kind != "deposit" || in.Amount != 0 || in.SettingsVersion < 1 || len(in.Note) > 500 || in.WithdrawalPassword != "" || in.WithdrawalConfirmation != "" {
		return out, errors.New("USDT 充值参数不正确，请按当前收款设置提交")
	}
	// The idempotency body excludes withdrawal credentials and uses the original
	// quote/version. Retries must resolve before checking current configuration.
	err := s.db.Update(func(tx *bolt.Tx) error {
		u, err := active(tx, id)
		if err != nil {
			return err
		}
		ref, err := idem(tx, "request:"+id, in.Key, in)
		if err != nil {
			return err
		}
		if ref != "" {
			out, err = get[WalletRequest](tx, "requests", ref)
			return err
		}
		cfg, err := get[PaymentSettings](tx, "payment_settings", "usdt")
		if err != nil {
			return err
		}
		if !cfg.Enabled || validatePaymentSettings(cfg) != nil {
			return errors.New("USDT 充值暂未开放，请联系客服")
		}
		if cfg.Version != in.SettingsVersion {
			return errors.New("USDT 收款信息或汇率已更新，请刷新确认后重新提交")
		}
		amount, err := usdtAmountCents(in.USDTAmount, cfg.RateYuan)
		if err != nil {
			return err
		}
		if amount < cfg.MinAmountCents || amount > cfg.MaxAmountCents {
			return errors.New("折合充值金额不在当前单笔充值限额内")
		}
		transactionID, err := usdtTransactionID(cfg.Network, in.TransactionID)
		if err != nil {
			return err
		}
		index := tx.Bucket([]byte("payment_transactions"))
		transactionKey := usdtTransactionKey(cfg.Network, transactionID)
		if ref := index.Get([]byte(transactionKey)); ref != nil {
			previous, err := get[WalletRequest](tx, "requests", string(ref))
			if err != nil {
				return err
			}
			if previous.Status != "rejected" {
				return errors.New("该网络的交易 ID 已提交，请勿重复申请")
			}
		}
		out = WalletRequest{ID: seq(tx, "requests"), UserID: id, Username: u.Username, Kind: "deposit", Amount: amount, Note: strings.TrimSpace(in.Note), Status: "pending", CreatedAt: time.Now(),
			PaymentMethod: "usdt", USDTAmount: in.USDTAmount, TransactionID: transactionID, SettingsVersion: cfg.Version, PaymentNetwork: cfg.Network, PaymentAddress: cfg.Address, RateYuan: cfg.RateYuan}
		if err = put(tx, "requests", out.ID, out); err != nil {
			return err
		}
		transactionKey = usdtRequestTransactionKey(cfg.Network, transactionID, out.ID)
		if err = index.Put([]byte(transactionKey), []byte(out.ID)); err != nil {
			return err
		}
		return remember(tx, "request:"+id, in.Key, out.ID, in)
	})
	return out, err
}

// Review uses the immutable request snapshot, never today's address or rate.
// The transaction claim and ledger credit are checked in the same Bolt write.
func validateUSDTApproval(tx *bolt.Tx, r WalletRequest) error {
	if r.PaymentMethod != "usdt" || r.Kind != "deposit" {
		return nil
	}
	transactionID, err := storedUSDTTransactionID(r.PaymentNetwork, r.TransactionID)
	if err != nil || r.Kind != "deposit" || r.SettingsVersion < 1 {
		return errors.New("USDT 充值记录不完整，无法入账")
	}
	amount, err := usdtAmountCents(r.USDTAmount, r.RateYuan)
	if err != nil || amount != r.Amount {
		return errors.New("USDT 充值金额与汇率快照不符，无法入账")
	}
	ref := tx.Bucket([]byte("payment_transactions")).Get([]byte(usdtRequestTransactionKey(r.PaymentNetwork, transactionID, r.ID)))
	if string(ref) != r.ID {
		return errors.New("交易 ID 已被其他申请使用或缺少关联，无法重复入账")
	}
	return nil
}
