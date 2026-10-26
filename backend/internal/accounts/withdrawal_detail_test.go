package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestWithdrawalDetailUsesOriginalHoldAndIsReadOnly(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 10000)
	in := withdrawalInput(t, s, 1000)
	in.PaymentMethod, in.PayoutNetwork, in.PayoutAddress = "okpay", "OKPAY", "00123456789"
	r, err := s.SubmitUSDTWalletRequest("member", in)
	must(t, err)
	// Later activity must never be subtracted from today's balance to fabricate
	// the earlier balance. The original hold is the authoritative snapshot.
	seedWager(t, s, 2500, 0, 2500)
	before := managementFinancialSnapshot(t, s)
	d, err := s.WithdrawalDetail("member", r.ID)
	must(t, err)
	if d.Request.ID != r.ID || d.Request.Withdrawal.Fee != 100 || d.BalanceBefore == nil || d.BalanceAfter == nil || *d.BalanceBefore != 110000 || *d.BalanceAfter != 109000 {
		t.Fatal("wrong original balance snapshot", d)
	}
	if _, err = s.WithdrawalDetail("admin", r.ID); err != ErrNotFound {
		t.Fatal("another user's request disclosed", err)
	}
	if before != managementFinancialSnapshot(t, s) {
		t.Fatal("detail read changed funds or history")
	}
	must(t, s.ReviewRequest("admin", r.ID, "rejected", "核对后退回"))
	d, err = s.WithdrawalDetail("member", r.ID)
	must(t, err)
	if d.Request.Status != "rejected" || *d.BalanceBefore != 110000 || *d.BalanceAfter != 109000 {
		t.Fatal("review rewrote original movement")
	}
}

func TestWithdrawalDetailHTTPPrivacyAndLegacyMissingBalances(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	var memberToken, adminToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if e := put(tx, "requests", "legacy", WalletRequest{ID: "legacy", UserID: "member", Username: "member", Kind: "withdraw", Amount: 1000, Status: "pending"}); e != nil {
			return e
		}
		if e := put(tx, "requests", "deposit", WalletRequest{ID: "deposit", UserID: "member", Kind: "deposit", Amount: 1000}); e != nil {
			return e
		}
		var e error
		memberToken, e = newLogin(tx, "member")
		if e != nil {
			return e
		}
		adminToken, e = newLogin(tx, "admin")
		return e
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	call := func(id, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/wallet/requests/"+id+"/detail", nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		id, token string
		code      int
	}{{"legacy", "", 401}, {"legacy", adminToken, 404}, {"missing", memberToken, 404}, {"deposit", memberToken, 404}, {"legacy", memberToken, 200}} {
		w := call(tc.id, tc.token)
		if w.Code != tc.code || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal(w.Code, w.Body)
		}
	}
	var response struct {
		Data WithdrawalDetail `json:"data"`
	}
	must(t, json.Unmarshal(call("legacy", memberToken).Body.Bytes(), &response))
	if response.Data.BalanceBefore != nil || response.Data.BalanceAfter != nil || response.Data.Request.Withdrawal != nil {
		t.Fatal("legacy missing data was invented")
	}
}
