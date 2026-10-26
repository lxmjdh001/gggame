package accounts

import (
	"fmt"
	"reflect"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestTransactionsOnlyRechargeAndWithdrawalBeforePagination(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 1; i <= 25; i++ {
			id := fmt.Sprintf("%020d", i)
			kind := "deposit"
			if i%2 == 0 {
				kind = "withdraw"
			}
			if err := put(tx, "requests", id, WalletRequest{ID: id, UserID: "member", Kind: kind, Amount: 100, Status: "pending"}); err != nil {
				return err
			}
		}
		for _, kind := range []string{"order", "reward", "commission", "admin_adjust", ""} {
			id := "z-" + kind
			if err := put(tx, "requests", id, WalletRequest{ID: id, UserID: "member", Kind: kind, Amount: 999}); err != nil {
				return err
			}
		}
		return put(tx, "requests", "other-private", WalletRequest{ID: "other-private", UserID: "other", Kind: "deposit", Note: "private"})
	}))
	before := markSixCombinedStoredState(t, s)
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		v, err := s.TransactionsPage("member", page)
		must(t, err)
		if v.Total != 25 || v.Page != page || (page < 3 && len(v.Items) != 10) || (page == 3 && len(v.Items) != 5) {
			t.Fatal(v)
		}
		for _, r := range v.Items {
			if (r.Kind != "deposit" && r.Kind != "withdraw") || r.UserID != "member" || seen[r.ID] {
				t.Fatal(r)
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != 25 {
		t.Fatal("truncated records", len(seen))
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("read changed stored history")
	}
}
