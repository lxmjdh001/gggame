package accounts

import (
	"reflect"
	"strings"
	"testing"
)

func TestWalletAccountWithdrawalsSnapshotSaveReplayAndApproval(t *testing.T) {
	for _, method := range []string{"okpay", "topay", "kdpay", "fpay", "808"} {
		t.Run(method, func(t *testing.T) {
			s, path := withdrawalFixture(t)
			defer func() { s.Close() }()
			seedWager(t, s, 10000, 0, 10000)
			network := strings.ToUpper(method)
			before := managementFinancialSnapshot(t, s)
			address, err := s.SaveWithdrawalAddress("member", network, WithdrawalAddress{Address: "00123456789"})
			must(t, err)
			if managementFinancialSnapshot(t, s) != before {
				t.Fatal("saving wallet account changed funds")
			}
			in := withdrawalInput(t, s, 10000)
			in.PaymentMethod, in.PayoutNetwork, in.PayoutAddress = method, network, address.Address
			r, err := s.SubmitUSDTWalletRequest("member", in)
			must(t, err)
			if r.PaymentMethod != method || r.Withdrawal.Network != network || r.Withdrawal.Address != "00123456789" || r.Withdrawal.Fee != 1000 || r.Withdrawal.Net != 9000 || r.Withdrawal.USDTAmount != "13.043478" || r.Withdrawal.RateYuan != "6.9" {
				t.Fatal("wrong wallet payout snapshot", r)
			}
			_, err = s.SaveWithdrawalAddress("member", network, WithdrawalAddress{Version: address.Version, Address: "new-wallet-account"})
			must(t, err)
			if _, err = s.SaveWithdrawalAddress("member", network, WithdrawalAddress{Version: address.Version, Address: "stale-account"}); err == nil {
				t.Fatal("stale preference overwrite")
			}
			cfg, err := s.WithdrawalSettings("admin", true)
			must(t, err)
			cfg.FeeBPS, cfg.RateYuan, cfg.USDTEnabled = 2000, "8", false
			_, err = s.SaveWithdrawalSettings("admin", cfg)
			must(t, err)
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			in.WithdrawalPassword, in.WithdrawalConfirmation = "", ""
			retry, err := s.SubmitUSDTWalletRequest("member", in)
			must(t, err)
			if retry.ID != r.ID || !reflect.DeepEqual(retry.Withdrawal, r.Withdrawal) {
				t.Fatal("retry lost original snapshot")
			}
			changed := in
			changed.PayoutAddress = "new-wallet-account"
			if _, err = s.SubmitUSDTWalletRequest("member", changed); err == nil {
				t.Fatal("accepted request redirected")
			}
			next := withdrawalInput(t, s, 1000)
			next.PaymentMethod, next.PayoutNetwork, next.PayoutAddress = method, network, "new-wallet-account"
			if _, err = s.SubmitUSDTWalletRequest("member", next); err == nil {
				t.Fatal("closed withdrawal channel bypassed")
			}
			must(t, s.ReviewRequest("admin", r.ID, "approved", "核实原账号与原汇率"))
			addresses, err := s.WithdrawalAddresses("member")
			must(t, err)
			found := false
			for _, a := range addresses {
				if a.Network == network {
					found = a.Address == "new-wallet-account" && a.Version == 2
				}
			}
			if !found {
				t.Fatal("wallet preference not persisted")
			}
			progress, err := s.Wagering("admin", "member")
			must(t, err)
			if progress.Required != 0 || progress.Turnover != 0 {
				t.Fatal("wallet approval did not consume cutoff")
			}
		})
	}
}

func TestWalletAccountValidationAndPublicRouteRestrictions(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 10000)
	before := managementFinancialSnapshot(t, s)
	for _, tc := range []struct{ method, network, address string }{
		{"unknown", "OKPAY", "001"}, {"okpay", "TOPAY", "001"}, {"usdt", "OKPAY", "001"},
		{"okpay", "TRC20", "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"}, {"okpay", "OKPAY", ""},
		{"okpay", "OKPAY", "a b"}, {"okpay", "OKPAY", "a\nb"}, {"okpay", "OKPAY", strings.Repeat("a", 129)},
	} {
		in := withdrawalInput(t, s, 1000)
		in.PaymentMethod, in.PayoutNetwork, in.PayoutAddress = tc.method, tc.network, tc.address
		if _, err := s.SubmitUSDTWalletRequest("member", in); err == nil {
			t.Fatal("invalid wallet request accepted", tc)
		}
	}
	for _, method := range []string{"okpay", "topay", "kdpay", "fpay", "808"} {
		in := WalletSubmission{RequestInput: RequestInput{Kind: "deposit", Amount: 1000, Note: "unsupported deposit", Key: randomID()}, PaymentMethod: method}
		if _, err := s.SubmitUSDTWalletRequest("member", in); err == nil {
			t.Fatal("wallet deposit unexpectedly enabled")
		}
	}
	for _, value := range []string{"", "a b", "a\tb", strings.Repeat("a", 129)} {
		if _, err := s.SaveWithdrawalAddress("member", "OKPAY", WithdrawalAddress{Address: value}); err == nil {
			t.Fatal("invalid saved account accepted")
		}
	}
	if managementFinancialSnapshot(t, s) != before {
		t.Fatal("rejected request changed funds")
	}
}
