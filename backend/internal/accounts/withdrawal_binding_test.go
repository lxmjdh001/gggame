package accounts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWithdrawalBindingRequiresMatchingConfirmationAndCurrentPIN(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	in := WithdrawalAddressInput{Address: "00123456789", Confirmation: "00123456789", Password: "483927"}
	financial := managementFinancialSnapshot(t, s)
	bad := in
	bad.Confirmation = "different"
	if _, err := s.SaveVerifiedWithdrawalAddress("member", "OKPAY", bad); err == nil {
		t.Fatal("mismatch accepted")
	}
	for _, pin := range []string{"", "000000"} {
		bad = in
		bad.Password = pin
		if _, err := s.SaveVerifiedWithdrawalAddress("member", "OKPAY", bad); err == nil {
			t.Fatal("missing or wrong PIN accepted")
		}
	}
	result, err := s.SaveVerifiedWithdrawalAddress("member", "OKPAY", in)
	must(t, err)
	if result.Version != 1 || result.Address != in.Address {
		t.Fatal(result)
	}
	retry, err := s.SaveVerifiedWithdrawalAddress("member", "OKPAY", in)
	must(t, err)
	if retry != result {
		t.Fatal("lost-response replay did not preserve preference")
	}
	bad = in
	bad.Address = "changed-account"
	bad.Confirmation = bad.Address
	if _, err = s.SaveVerifiedWithdrawalAddress("member", "OKPAY", bad); err == nil {
		t.Fatal("stale different edit accepted")
	}
	bad.Version = result.Version
	_, err = s.SaveVerifiedWithdrawalAddress("member", "OKPAY", bad)
	must(t, err)
	if financial != managementFinancialSnapshot(t, s) {
		t.Fatal("binding modified financial history or credentials")
	}
	before := managementStored(t, s, "member")
	_, err = s.SetWithdrawalPassword("member", "483927", "135790")
	must(t, err)
	if _, err = s.saveWithdrawalAddress("member", "TOPAY", WithdrawalAddress{Address: "new-account"}, &before); err == nil {
		t.Fatal("credential change after verification was ignored")
	}
	bad.Password = "483927"
	bad.Version = 2
	bad.Address = "another-account"
	bad.Confirmation = bad.Address
	if _, err = s.SaveVerifiedWithdrawalAddress("member", "OKPAY", bad); err == nil {
		t.Fatal("old PIN still bound accounts")
	}
	bad.Password = "135790"
	_, err = s.SaveVerifiedWithdrawalAddress("member", "OKPAY", bad)
	must(t, err)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	addresses, err := s.WithdrawalAddresses("member")
	must(t, err)
	raw, err := json.Marshal(addresses)
	must(t, err)
	if !strings.Contains(string(raw), "another-account") {
		t.Fatal("binding did not survive restart")
	}
	for _, secret := range []string{"135790", "483927", "withdrawal_password", "address_confirmation", "Password", "Confirmation"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("saved destination leaked binding input")
		}
	}
}

func TestWithdrawalBindingSharesPersistentPINRateLimit(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	in := WithdrawalAddressInput{Address: "account-123", Confirmation: "account-123", Password: "000000"}
	for i := 0; i < 5; i++ {
		if _, err := s.SaveVerifiedWithdrawalAddress("member", "FPAY", in); err == nil {
			t.Fatal("wrong PIN accepted")
		}
	}
	in.Password = "483927"
	if _, err := s.SaveVerifiedWithdrawalAddress("member", "FPAY", in); err != errSecurityLimited {
		t.Fatal("binding bypassed PIN limiter", err)
	}
	if err := s.VerifyWithdrawalPassword("member", "483927"); err != errSecurityLimited {
		t.Fatal("binding and withdrawals used different PIN limiters", err)
	}
}
