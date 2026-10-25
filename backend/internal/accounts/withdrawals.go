package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

type WithdrawalSettings struct {
	Version     int64  `json:"version"`
	FeeBPS      int64  `json:"fee_bps"`
	USDTEnabled bool   `json:"usdt_enabled"`
	RateYuan    string `json:"rate_yuan"`
	RateVersion int64  `json:"rate_version"`
}
type WithdrawalTerms struct {
	Version     int64  `json:"version"`
	FeeBPS      int64  `json:"fee_bps"`
	Fee         int64  `json:"fee_cents"`
	Net         int64  `json:"net_cents"`
	RateYuan    string `json:"rate_yuan,omitempty"`
	RateVersion int64  `json:"rate_version,omitempty"`
	USDTAmount  string `json:"usdt_amount,omitempty"`
	Network     string `json:"network,omitempty"`
	Address     string `json:"address,omitempty"`
}
type wagerCutoff struct{ Ledger, Orders, Adjustments string }
type wagerState struct {
	Version  int64
	Green    bool
	Consumed wagerCutoff
}
type wagerAdjustment struct {
	UserID       string
	Delta        int64
	OrderThrough string
}
type Wagering struct {
	Revision  string `json:"revision"`
	Version   int64  `json:"version"`
	Deposit   int64  `json:"deposit_cents"`
	Reward    int64  `json:"reward_cents"`
	Required  int64  `json:"required_cents"`
	Turnover  int64  `json:"turnover_cents"`
	Remaining int64  `json:"remaining_cents"`
	Green     bool   `json:"green_channel"`
	Eligible  bool   `json:"eligible"`
	raw       int64
}
type WagerEdit struct {
	Revision string `json:"revision"`
	Version  int64  `json:"version"`
	Turnover int64  `json:"turnover_cents"`
	Green    bool   `json:"green_channel"`
	Note     string `json:"note"`
	Key      string `json:"key"`
}

func initializeWithdrawals(tx *bolt.Tx) error {
	for _, b := range []string{"withdrawal_settings", "wager_states", "wager_adjustments", "withdrawal_wagers", "wager_edits", "withdrawal_addresses"} {
		if _, err := tx.CreateBucketIfNotExists([]byte(b)); err != nil {
			return err
		}
	}
	if tx.Bucket([]byte("withdrawal_settings")).Get([]byte("current")) != nil {
		return nil
	}
	if err := put(tx, "withdrawal_settings", "current", WithdrawalSettings{Version: 1, FeeBPS: 1000, USDTEnabled: true}); err != nil {
		return err
	}
	cfg, err := get[PaymentSettings](tx, "payment_settings", "usdt")
	if err != nil {
		return err
	}
	// One-time requested rate change; retain existing address, network and limits.
	// All already accepted deposits retain their immutable original rate snapshot.
	if cfg.RateYuan != "" && cfg.RateYuan != "6.9" {
		old := cfg
		cfg.RateYuan = "6.9"
		cfg.Version++
		if err = put(tx, "payment_settings", "usdt", cfg); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"before": old, "after": cfg})
		return audit(tx, "system", "withdrawal.rate.initialize", "usdt", string(detail))
	}
	return nil
}
func withdrawalSettings(tx *bolt.Tx) (WithdrawalSettings, error) {
	cfg, err := get[WithdrawalSettings](tx, "withdrawal_settings", "current")
	if err != nil {
		return cfg, err
	}
	pay, err := get[PaymentSettings](tx, "payment_settings", "usdt")
	if err != nil {
		return cfg, err
	}
	cfg.RateYuan = pay.RateYuan
	if cfg.RateYuan == "" {
		cfg.RateYuan = "6.9"
	}
	cfg.RateVersion = pay.Version
	return cfg, nil
}
func (s *Store) WithdrawalSettings(actor string, adminOnly bool) (WithdrawalSettings, error) {
	var out WithdrawalSettings
	err := s.db.View(func(tx *bolt.Tx) error {
		if adminOnly {
			if e := admin(tx, actor); e != nil {
				return e
			}
		} else {
			if _, e := active(tx, actor); e != nil {
				return e
			}
		}
		var e error
		out, e = withdrawalSettings(tx)
		return e
	})
	return out, err
}
func (s *Store) SaveWithdrawalSettings(actor string, cfg WithdrawalSettings) (WithdrawalSettings, error) {
	var out WithdrawalSettings
	cfg.RateYuan = strings.TrimSpace(cfg.RateYuan)
	if cfg.FeeBPS < 0 || cfg.FeeBPS > 9999 {
		return out, errors.New("手续费须在0–99.99%之间")
	}
	if _, err := usdtRateMicros(cfg.RateYuan); err != nil {
		return out, err
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		old, err := withdrawalSettings(tx)
		if err != nil {
			return err
		}
		if old.Version != cfg.Version || old.RateVersion != cfg.RateVersion {
			return errors.New("提现或汇率设置已更新，请刷新后重试")
		}
		cfg.Version++
		if cfg.Version <= old.Version {
			return errors.New("配置版本已达上限")
		}
		pay, err := get[PaymentSettings](tx, "payment_settings", "usdt")
		if err != nil {
			return err
		}
		if pay.RateYuan != cfg.RateYuan {
			pay.RateYuan = cfg.RateYuan
			pay.Version++
			if err = put(tx, "payment_settings", "usdt", pay); err != nil {
				return err
			}
		}
		cfg.RateVersion = pay.Version
		if err = put(tx, "withdrawal_settings", "current", cfg); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"before": old, "after": cfg})
		if err = audit(tx, actor, "withdrawal.settings", "current", string(detail)); err != nil {
			return err
		}
		out = cfg
		return nil
	})
	return out, err
}
func stateForWager(tx *bolt.Tx, id string) (wagerState, error) {
	if tx.Bucket([]byte("wager_states")).Get([]byte(id)) == nil {
		return wagerState{Version: 1}, nil
	}
	return get[wagerState](tx, "wager_states", id)
}
func lastKey(tx *bolt.Tx, b string) string {
	k, _ := tx.Bucket([]byte(b)).Cursor().Last()
	return string(k)
}
func wagerThrough(tx *bolt.Tx) wagerCutoff {
	return wagerCutoff{lastKey(tx, "ledger"), lastKey(tx, "orders"), lastKey(tx, "wager_adjustments")}
}
func inWagerRange(key, after, through string, bounded bool) bool {
	return key > after && (!bounded || key <= through)
}
func wagering(tx *bolt.Tx, id string, cutoff *wagerCutoff) (Wagering, error) {
	state, err := stateForWager(tx, id)
	if err != nil {
		return Wagering{}, err
	}
	out := Wagering{Version: state.Version, Green: state.Green}
	bound := wagerCutoff{}
	if cutoff != nil {
		bound = *cutoff
	}
	err = tx.Bucket([]byte("ledger")).ForEach(func(k, v []byte) error {
		if !inWagerRange(string(k), state.Consumed.Ledger, bound.Ledger, cutoff != nil) {
			return nil
		}
		var l Ledger
		if e := json.Unmarshal(v, &l); e != nil {
			return e
		}
		if l.UserID != id || l.Amount <= 0 {
			return nil
		}
		// Use the same exact legacy-note classification as the member ledger.
		// This includes old admin_adjust entries explicitly labelled 充值/奖励,
		// without rewriting history or treating arbitrary adjustments as deposits.
		switch ledgerForDisplay(l).Kind {
		case "deposit_approved", "admin_deposit":
			out.Deposit += l.Amount
		case "registration_bonus", "promotion_bonus", "deposit_bonus", "reward", "admin_reward":
			out.Reward += l.Amount
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	type wagerOrder struct {
		ID   string
		Cost int64
	}
	orders := []wagerOrder{}
	err = tx.Bucket([]byte("orders")).ForEach(func(k, v []byte) error {
		if !inWagerRange(string(k), state.Consumed.Orders, bound.Orders, cutoff != nil) {
			return nil
		}
		var o Order
		if e := json.Unmarshal(v, &o); e != nil {
			return e
		}
		if o.UserID == id && (o.Status == "pending" || o.Status == "settled") {
			orders = append(orders, wagerOrder{string(k), o.Cost})
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	index := 0
	err = tx.Bucket([]byte("wager_adjustments")).ForEach(func(k, v []byte) error {
		if !inWagerRange(string(k), state.Consumed.Adjustments, bound.Adjustments, cutoff != nil) {
			return nil
		}
		var a wagerAdjustment
		if e := json.Unmarshal(v, &a); e != nil {
			return e
		}
		if a.UserID != id {
			return nil
		}
		for index < len(orders) && orders[index].ID <= a.OrderThrough {
			out.raw += orders[index].Cost
			index++
		}
		out.raw = max(0, out.raw+a.Delta)
		return nil
	})
	if err != nil {
		return out, err
	}
	for ; index < len(orders); index++ {
		out.raw += orders[index].Cost
	}
	out.Revision = fmt.Sprintf("%d:%d:%d:%d", state.Version, out.Deposit, out.Reward, out.raw)
	out.Required = out.Deposit + out.Reward
	out.Turnover = max(0, out.raw)
	out.Remaining = max(0, out.Required-out.Turnover)
	// 打码量 = Required (充值＋奖励); 打码流水 = Turnover (投注＋人工流水调整).
	// Equality qualifies; approval consumes only the submission's saved cutoff.
	out.Eligible = out.Green || out.Remaining == 0
	return out, nil
}
func (s *Store) Wagering(actor, id string) (Wagering, error) {
	var out Wagering
	err := s.db.View(func(tx *bolt.Tx) error {
		if actor != id {
			if e := admin(tx, actor); e != nil {
				return e
			}
		}
		if _, e := get[storedUser](tx, "users", id); e != nil {
			return e
		}
		var e error
		out, e = wagering(tx, id, nil)
		return e
	})
	return out, err
}
func (s *Store) EditWagering(actor, id string, in WagerEdit) (Wagering, error) {
	var out Wagering
	if in.Turnover < 0 || in.Turnover > maxMoney || len(strings.TrimSpace(in.Note)) < 2 || len(in.Note) > 500 {
		return out, errors.New("请填写有效打码流水和调整原因")
	}
	body := struct {
		ID   string
		Edit WagerEdit
	}{id, in}
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		if _, err := get[storedUser](tx, "users", id); err != nil {
			return err
		}
		ref, err := idem(tx, "wager:"+actor, in.Key, body)
		if err != nil {
			return err
		}
		if ref != "" {
			out, err = get[Wagering](tx, "wager_edits", ref)
			return err
		}
		before, err := wagering(tx, id, nil)
		if err != nil {
			return err
		}
		if before.Version != in.Version || before.Revision != in.Revision {
			return errors.New("会员打码记录已更新，请刷新后重新调整")
		}
		state, err := stateForWager(tx, id)
		if err != nil {
			return err
		}
		state.Version++
		state.Green = in.Green
		if err = put(tx, "wager_adjustments", seq(tx, "wager_adjustments"), wagerAdjustment{UserID: id, Delta: in.Turnover - before.raw, OrderThrough: lastKey(tx, "orders")}); err != nil {
			return err
		}
		if err = put(tx, "wager_states", id, state); err != nil {
			return err
		}
		out, err = wagering(tx, id, nil)
		if err != nil {
			return err
		}
		ref = seq(tx, "wager_edits")
		if err = put(tx, "wager_edits", ref, out); err != nil {
			return err
		}
		if err = remember(tx, "wager:"+actor, in.Key, ref, body); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"before": before, "after": out, "note": in.Note})
		return audit(tx, actor, "wager.adjust", id, string(detail))
	})
	return out, err
}
func usdtPayout(cents int64, rate string) (string, error) {
	micros, err := usdtRateMicros(rate)
	if err != nil {
		return "", err
	}
	n := new(big.Int).Mul(big.NewInt(cents), big.NewInt(10000000000))
	n.Quo(n, big.NewInt(micros))
	if !n.IsInt64() || n.Sign() <= 0 {
		return "", errors.New("实际到账金额不足0.000001 USDT")
	}
	v := n.Int64()
	return fmt.Sprintf("%d.%06d", v/1000000, v%1000000), nil
}
func withdrawalReplay(tx *bolt.Tx, id string, in WalletSubmission) (WalletRequest, error) {
	var out WalletRequest
	ref, err := idem(tx, "request:"+id, in.Key, in.RequestInput)
	if err != nil || ref == "" {
		return out, err
	}
	out, err = get[WalletRequest](tx, "requests", ref)
	if err != nil {
		return out, err
	}
	if out.Withdrawal == nil {
		if in.PaymentMethod != "" || in.PayoutNetwork != "" || in.PayoutAddress != "" {
			return WalletRequest{}, errors.New("原提现申请的收款方式不可更改")
		}
		return out, nil
	}
	terms := out.Withdrawal
	if out.PaymentMethod != in.PaymentMethod || terms.Network != in.PayoutNetwork || terms.Address != strings.TrimSpace(in.PayoutAddress) || terms.Version != in.WithdrawalVersion || terms.RateVersion != in.WithdrawalRateVersion {
		return WalletRequest{}, errors.New("提现信息与原申请不一致，请确认原申请")
	}
	return out, nil
}
func (s *Store) submitWithdrawal(id string, in WalletSubmission) (WalletRequest, error) {
	var out WalletRequest
	if in.USDTAmount != "" || in.TransactionID != "" || in.SettingsVersion != 0 || (in.PaymentMethod != "" && in.PaymentMethod != "usdt") {
		return out, errors.New("提现参数不正确")
	}
	// Accepted retries are independent of current PIN, policy or rate changes.
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, e := active(tx, id); e != nil {
			return e
		}
		var e error
		out, e = withdrawalReplay(tx, id, in)
		return e
	})
	if err != nil || out.ID != "" {
		return out, err
	}
	if in.WithdrawalPassword != in.WithdrawalConfirmation {
		return out, errors.New("两次提现密码不一致")
	}
	if err = s.VerifyWithdrawalPassword(id, in.WithdrawalPassword); err != nil {
		return out, err
	}
	if in.Amount < 100 || in.Amount > 100000000 || len(strings.TrimSpace(in.Note)) < 2 || len(in.Note) > 500 {
		return out, errors.New("请输入有效金额（1–1,000,000元）和收款说明")
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		u, e := active(tx, id)
		if e != nil {
			return e
		}
		out, e = withdrawalReplay(tx, id, in)
		if e != nil || out.ID != "" {
			return e
		}
		cfg, e := withdrawalSettings(tx)
		if e != nil {
			return e
		}
		if in.WithdrawalVersion != cfg.Version || in.WithdrawalRateVersion != cfg.RateVersion {
			return errors.New("提现手续费或汇率已更新，请刷新确认后重新提交")
		}
		progress, e := wagering(tx, id, nil)
		if e != nil {
			return e
		}
		if !progress.Eligible {
			return fmt.Errorf("打码流水不足，还需投注%.2f元", float64(progress.Remaining)/100)
		}
		terms := &WithdrawalTerms{Version: cfg.Version, FeeBPS: cfg.FeeBPS, Fee: (in.Amount*cfg.FeeBPS + 5000) / 10000, RateVersion: cfg.RateVersion}
		terms.Net = in.Amount - terms.Fee
		if terms.Net <= 0 {
			return errors.New("扣除手续费后的到账金额须大于零")
		}
		if in.PaymentMethod == "usdt" {
			if !cfg.USDTEnabled {
				return errors.New("USDT提现暂未开放")
			}
			terms.Network = in.PayoutNetwork
			terms.Address = strings.TrimSpace(in.PayoutAddress)
			terms.RateYuan = cfg.RateYuan
			if !((terms.Network == "TRC20" && tronAddressPattern.MatchString(terms.Address)) || (terms.Network == "ERC20" && ethAddressPattern.MatchString(terms.Address))) {
				return errors.New("请填写与TRC20或ERC20网络匹配的完整USDT收款地址")
			}
			terms.USDTAmount, e = usdtPayout(terms.Net, cfg.RateYuan)
			if e != nil {
				return e
			}
		} else if in.PayoutAddress != "" || in.PayoutNetwork != "" {
			return errors.New("人工提现不接受USDT收款参数")
		}
		cutoff := wagerThrough(tx)
		out = WalletRequest{ID: seq(tx, "requests"), UserID: id, Username: u.Username, Kind: "withdraw", Amount: in.Amount, Note: in.Note, Status: "pending", CreatedAt: time.Now(), PaymentMethod: in.PaymentMethod, Withdrawal: terms}
		if e = ledger(tx, &u, -in.Amount, in.Amount, "withdraw_hold", out.ID, in.Note); e != nil {
			return e
		}
		if e = put(tx, "withdrawal_wagers", out.ID, cutoff); e != nil {
			return e
		}
		if e = put(tx, "requests", out.ID, out); e != nil {
			return e
		}
		return remember(tx, "request:"+id, in.Key, out.ID, in.RequestInput)
	})
	return out, err
}
func validateWithdrawalTerms(r WalletRequest) error {
	t := r.Withdrawal
	if t == nil {
		return nil
	}
	if r.Kind != "withdraw" || t.Version < 1 || t.FeeBPS < 0 || t.FeeBPS > 9999 || t.Fee != (r.Amount*t.FeeBPS+5000)/10000 || t.Net != r.Amount-t.Fee || t.Net <= 0 {
		return errors.New("提现费用快照异常，请核实")
	}
	if r.PaymentMethod == "usdt" {
		amount, e := usdtPayout(t.Net, t.RateYuan)
		if e != nil || amount != t.USDTAmount || !((t.Network == "TRC20" && tronAddressPattern.MatchString(t.Address)) || (t.Network == "ERC20" && ethAddressPattern.MatchString(t.Address))) {
			return errors.New("USDT提现快照异常，请核实")
		}
	}
	return nil
}

func approveWithdrawalWager(tx *bolt.Tx, r WalletRequest) error {
	if r.Kind != "withdraw" || r.Withdrawal == nil {
		return nil
	}
	if err := validateWithdrawalTerms(r); err != nil {
		return err
	}
	cutoff, err := get[wagerCutoff](tx, "withdrawal_wagers", r.ID)
	if err != nil {
		return err
	}
	progress, err := wagering(tx, r.UserID, &cutoff)
	if err != nil {
		return err
	}
	if !progress.Eligible {
		return errors.New("申请覆盖的打码流水已因撤销或调整不足，请先核实或驳回；绿色通道可豁免")
	}
	state, err := stateForWager(tx, r.UserID)
	if err != nil {
		return err
	}
	state.Consumed = wagerCutoff{max(state.Consumed.Ledger, cutoff.Ledger), max(state.Consumed.Orders, cutoff.Orders), max(state.Consumed.Adjustments, cutoff.Adjustments)}
	state.Version++
	return put(tx, "wager_states", r.UserID, state)
}
