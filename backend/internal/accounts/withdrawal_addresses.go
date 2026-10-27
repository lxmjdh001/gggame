package accounts

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

// Saved destinations are private preferences, not mutable withdrawal terms.
// Accepted requests always retain the address/network in their own snapshot.
type WithdrawalAddress struct {
	Version int64  `json:"version"`
	Network string `json:"network"`
	Address string `json:"address"`
}

// Secrets and confirmation are accepted input only, never saved preferences.
type WithdrawalAddressInput struct {
	Version      int64  `json:"version"`
	Network      string `json:"network,omitempty"`
	Address      string `json:"address"`
	Confirmation string `json:"address_confirmation"`
	Password     string `json:"withdrawal_password"`
}

func (s *Store) SaveVerifiedWithdrawalAddress(id, network string, in WithdrawalAddressInput) (WithdrawalAddress, error) {
	if strings.TrimSpace(in.Address) == "" || strings.TrimSpace(in.Address) != strings.TrimSpace(in.Confirmation) {
		return WithdrawalAddress{}, errors.New("两次收款地址或账号不一致，请重新确认")
	}
	var before storedUser
	err := s.db.View(func(tx *bolt.Tx) error { var e error; before, e = active(tx, id); return e })
	if err != nil {
		return WithdrawalAddress{}, err
	}
	if err = s.VerifyWithdrawalPassword(id, in.Password); err != nil {
		return WithdrawalAddress{}, err
	}
	return s.saveWithdrawalAddress(id, network, WithdrawalAddress{Version: in.Version, Network: in.Network, Address: in.Address}, &before)
}

func validWithdrawalAddress(network, address string) bool {
	switch network {
	case "TRC20":
		return tronAddressPattern.MatchString(address)
	case "ERC20":
		return ethAddressPattern.MatchString(address)
	case "OKPAY", "TOPAY", "KDPAY", "FPAY", "808":
		return utf8.ValidString(address) && utf8.RuneCountInString(address) >= 1 && utf8.RuneCountInString(address) <= 128 && !strings.ContainsFunc(address, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	}
	return false
}

func walletWithdrawalMethod(method string) bool {
	return method == "okpay" || method == "topay" || method == "kdpay" || method == "fpay" || method == "808"
}

func validWithdrawalDestination(method, network, address string) bool {
	return validWithdrawalAddress(network, address) && (method == "usdt" && (network == "TRC20" || network == "ERC20") || walletWithdrawalMethod(method) && network == strings.ToUpper(method))
}

func (s *Store) WithdrawalAddresses(id string) ([]WithdrawalAddress, error) {
	out := []WithdrawalAddress{}
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, err := active(tx, id); err != nil {
			return err
		}
		for _, network := range []string{"TRC20", "ERC20", "OKPAY", "TOPAY", "KDPAY", "FPAY", "808"} {
			value := WithdrawalAddress{Network: network}
			if raw := tx.Bucket([]byte("withdrawal_addresses")).Get([]byte(id + ":" + network)); raw != nil {
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
			}
			out = append(out, value)
		}
		return nil
	})
	return out, err
}

func (s *Store) SaveWithdrawalAddress(id, network string, in WithdrawalAddress) (WithdrawalAddress, error) {
	return s.saveWithdrawalAddress(id, network, in, nil)
}

func (s *Store) saveWithdrawalAddress(id, network string, in WithdrawalAddress, verified *storedUser) (WithdrawalAddress, error) {
	var out WithdrawalAddress
	network = strings.ToUpper(strings.TrimSpace(network))
	in.Address = strings.TrimSpace(in.Address)
	if in.Version < 0 || in.Network != "" && in.Network != network || !validWithdrawalAddress(network, in.Address) {
		return out, errors.New("请填写有效的收款账号；USDT须填写与所选网络匹配的完整地址，其他钱包账号为1–128位且不能含空白或控制字符")
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		current, err := active(tx, id)
		if err != nil {
			return err
		}
		if verified != nil && (current.WithdrawalHash != verified.WithdrawalHash || current.WithdrawalSalt != verified.WithdrawalSalt || !current.WithdrawalPasswordSet) {
			return errors.New("提款密码已更新，请重新输入后保存")
		}
		b := tx.Bucket([]byte("withdrawal_addresses"))
		key := id + ":" + network
		old := WithdrawalAddress{Network: network}
		if raw := b.Get([]byte(key)); raw != nil {
			if err := json.Unmarshal(raw, &old); err != nil {
				return err
			}
		}
		// Exact replay after a lost response is safe; a stale different address
		// must never overwrite a newer edit from another device.
		if old.Address == in.Address && in.Version <= old.Version {
			out = old
			return nil
		}
		if old.Version != in.Version {
			return errors.New("提现地址已更新，请重新读取后再更改")
		}
		out = WithdrawalAddress{Version: old.Version + 1, Network: network, Address: in.Address}
		if out.Version <= old.Version {
			return errors.New("提现地址版本已达上限")
		}
		if err := put(tx, "withdrawal_addresses", key, out); err != nil {
			return err
		}
		// No address or other personal data in the general operation log.
		detail, _ := json.Marshal(map[string]any{"network": network, "version": out.Version, "replaced": old.Address != ""})
		return audit(tx, id, "withdrawal_address.save", id, string(detail))
	})
	return out, err
}
