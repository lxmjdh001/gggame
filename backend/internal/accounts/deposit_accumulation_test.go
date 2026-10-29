package accounts

import (
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestLegacyExplicitDepositIsCountedWithoutRewritingLedger(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	must(t, s.Adjust("admin", "member", 100000, "充值", randomID()))
	must(t, s.Adjust("admin", "member", -80000, "管理扣减", randomID()))
	seedWager(t, s, 10000, 0, 0)
	before := managementFinancialSnapshot(t, s)
	w, e := s.Wagering("member", "member")
	must(t, e)
	if w.Required != 110000 || w.Turnover != 0 {
		t.Fatal("legacy recharge omitted", w)
	}
	if before != managementFinancialSnapshot(t, s) {
		t.Fatal("reading wagering rewrote financial history")
	}
}

func TestAdminDepositThenReviewedDepositDoesNotLoseEarlierCredit(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	must(t, s.AdjustWithKind("admin", "member", 100000, "后台充值", randomID(), "deposit"))
	must(t, s.Adjust("admin", "member", -80000, "管理扣减，非提现", randomID()))
	seedWager(t, s, 0, 0, 5000)
	r, err := s.CreateRequest("member", RequestInput{Kind: "deposit", Amount: 10000, Note: "人工存款核对", Key: randomID()})
	must(t, err)
	must(t, s.ReviewRequest("admin", r.ID, "approved", "核对到账"))
	w, err := s.Wagering("member", "member")
	must(t, err)
	if w.Deposit != 110000 || w.Required != 110000 || w.Turnover != 5000 {
		t.Fatal("earlier admin deposit or existing turnover lost", w)
	}
	must(t, s.AdjustWithKind("admin", "member", 2000, "后台奖励", randomID(), "reward"))
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 112000 || w.Turnover != 5000 {
		t.Fatal("reward did not accumulate", w)
	}
}

func TestRepeatedDepositsPreserveAndAccumulateTurnover(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 6000)
	for i := 0; i < 3; i++ {
		in := RequestInput{Kind: "deposit", Amount: 2000, Note: "连续充值叠加测试", Key: randomID()}
		r, e := s.CreateRequest("member", in)
		must(t, e)
		must(t, s.ReviewRequest("admin", r.ID, "approved", "核对到账"))
		w, e := s.Wagering("member", "member")
		must(t, e)
		if w.Required != 10000+int64(i+1)*2000 || w.Turnover != 6000+int64(i)*1000 {
			t.Fatal("deposit reset accumulated progress", w)
		}
		seedWager(t, s, 0, 0, 1000)
	}
	w, e := s.Wagering("member", "member")
	must(t, e)
	if w.Turnover != 9000 || w.Required != 16000 {
		t.Fatal(w)
	}
}

func TestLegacyUSDTSuffixCollisionReplayAndApproval(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	cfg := enabledUSDTSettings(t, s)
	in := usdtSubmission(cfg, 1)
	in.TransactionID = "Ab12F"
	// Seed already accepted records as persisted by the former five-character
	// version, rather than allowing the new submission endpoint to create them.
	seedLegacy := func(input WalletSubmission) WalletRequest {
		var out WalletRequest
		must(t, s.db.Update(func(tx *bolt.Tx) error {
			amount, e := usdtAmountCents(input.USDTAmount, cfg.RateYuan)
			if e != nil {
				return e
			}
			out = WalletRequest{ID: seq(tx, "requests"), UserID: "member", Kind: "deposit", Amount: amount, Status: "pending", CreatedAt: time.Now(), PaymentMethod: "usdt", USDTAmount: input.USDTAmount, TransactionID: strings.ToLower(input.TransactionID), SettingsVersion: cfg.Version, PaymentNetwork: cfg.Network, PaymentAddress: cfg.Address, RateYuan: cfg.RateYuan}
			if e = put(tx, "requests", out.ID, out); e != nil {
				return e
			}
			if e = tx.Bucket([]byte("payment_transactions")).Put([]byte(usdtRequestTransactionKey(cfg.Network, out.TransactionID, out.ID)), []byte(out.ID)); e != nil {
				return e
			}
			return remember(tx, "request:member", input.Key, out.ID, input)
		}))
		return out
	}
	if _, e := s.SubmitWalletRequest("member", in); e == nil {
		t.Fatal("new suffix request accepted")
	}
	r := seedLegacy(in)
	retry, e := s.SubmitWalletRequest("member", in)
	must(t, e)
	if r.ID != retry.ID || r.TransactionID != "ab12f" {
		t.Fatal("suffix retry changed request")
	}
	other := in
	other.Key = randomID()
	if _, e := s.SubmitWalletRequest("member", other); e == nil {
		t.Fatal("new suffix request accepted")
	}
	r2 := seedLegacy(other)
	if r2.ID == r.ID {
		t.Fatal("suffix used as global transaction identity")
	}
	must(t, s.ReviewRequest("admin", r.ID, "approved", "已人工核对完整转账凭证"))
	must(t, s.ReviewRequest("admin", r2.ID, "approved", "另一笔同后缀转账已核对"))
	if s.ReviewRequest("admin", r.ID, "approved", "重复审核") == nil {
		t.Fatal("double credited")
	}
	w, e := s.Wagering("member", "member")
	must(t, e)
	if w.Deposit != 2*r.Amount {
		t.Fatal("approved suffix deposits did not accumulate", w)
	}
}
