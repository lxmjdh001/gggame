package accounts

import "testing"

func TestUSDTOnlyNewRequestsAndLegacyRetries(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 10000)
	deposit := WalletSubmission{RequestInput: RequestInput{Kind: "deposit", Amount: 100, Note: "旧人工充值", Key: randomID()}, TransactionID: "bank-id"}
	withdraw := withdrawalInput(t, s, 1000)
	for _, in := range []WalletSubmission{deposit, withdraw} {
		if _, e := s.SubmitUSDTWalletRequest("member", in); e == nil {
			t.Fatal("new manual request accepted")
		}
	}
	old, e := s.SubmitWalletRequest("member", deposit)
	must(t, e)
	retry, e := s.SubmitUSDTWalletRequest("member", deposit)
	must(t, e)
	if retry.ID != old.ID {
		t.Fatal("legacy deposit replay lost")
	}
	old, e = s.SubmitWalletRequest("member", withdraw)
	must(t, e)
	retry, e = s.SubmitUSDTWalletRequest("member", withdraw)
	must(t, e)
	if retry.ID != old.ID {
		t.Fatal("legacy withdrawal replay lost")
	}
	withdraw.PaymentMethod = "bypass"
	if _, e = s.SubmitUSDTWalletRequest("member", withdraw); e == nil {
		t.Fatal("changed retry accepted")
	}
	cfg := enabledUSDTSettings(t, s)
	_, e = s.SubmitUSDTWalletRequest("member", usdtSubmission(cfg, 123))
	must(t, e)
	newWithdrawal := withdrawalInput(t, s, 1000)
	newWithdrawal.PaymentMethod = "usdt"
	newWithdrawal.PayoutNetwork = "TRC20"
	newWithdrawal.PayoutAddress = cfg.Address
	_, e = s.SubmitUSDTWalletRequest("member", newWithdrawal)
	must(t, e)
}
