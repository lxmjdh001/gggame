package accounts

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func wagerValue(value int64) *int64 { return &value }

func samePublicWager(a, b Wagering) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func currentWager(t *testing.T, s *Store) Wagering {
	t.Helper()
	w, err := s.Wagering("admin", "member")
	must(t, err)
	return w
}

func editWagerValues(t *testing.T, s *Store, required, turnover *int64) Wagering {
	t.Helper()
	w := currentWager(t, s)
	before := markSixCombinedStoredState(t, s)
	next, err := s.EditWagering("admin", "member", WagerEdit{Version: w.Version, Revision: w.Revision, Required: required, Turnover: turnover, Green: w.Green, Note: "核实后调整打码", Key: randomID()})
	must(t, err)
	after := markSixCombinedStoredState(t, s)
	for bucket, contents := range before {
		switch bucket {
		case "wager_states", "wager_adjustments", "wager_edits", "keys", "audit":
			continue
		}
		if !reflect.DeepEqual(contents, after[bucket]) {
			t.Fatal("wager edit changed original data", bucket)
		}
	}
	return next
}

func TestWagerRequiredEditResetAndFutureAccumulation(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	seedWager(t, s, 50000, 2000, 20000)
	w := editWagerValues(t, s, wagerValue(60000), nil)
	if w.Required != 60000 || w.Turnover != 20000 || w.Deposit != 50000 || w.Reward != 2000 {
		t.Fatal(w)
	}
	seedWager(t, s, 10000, 500, 3000)
	w = currentWager(t, s)
	if w.Required != 70500 || w.Turnover != 23000 {
		t.Fatal("new recharge/reward must add to adjusted target", w)
	}
	w = editWagerValues(t, s, nil, wagerValue(75000))
	if w.Required != 70500 || w.Turnover != 75000 || !w.Eligible {
		t.Fatal(w)
	}
	w = editWagerValues(t, s, nil, wagerValue(0))
	if w.Required != 0 || w.Turnover != 0 || w.Deposit != 60000 || w.Reward != 2500 {
		t.Fatal("explicit zero did not clear both, or rewrote credits", w)
	}
	// Merely having zero turnover does not waive a new or manually set target.
	w = editWagerValues(t, s, wagerValue(7000), nil)
	if w.Required != 7000 || w.Turnover != 0 || w.Eligible {
		t.Fatal("independent target edit at zero turnover rejected or auto-cleared", w)
	}
	w = editWagerValues(t, s, nil, wagerValue(0))
	if w.Required != 0 || w.Turnover != 0 {
		t.Fatal("explicit zero must work even at already-zero turnover", w)
	}
	seedWager(t, s, 2000, 300, 900)
	w = currentWager(t, s)
	if w.Required != 2300 || w.Turnover != 900 || w.Eligible {
		t.Fatal("reset swallowed new activity", w)
	}
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	w = currentWager(t, s)
	if w.Required != 2300 || w.Turnover != 900 {
		t.Fatal("restart lost reset or new activity", w)
	}
}

func TestWagerRequiredWithdrawalCutoffsPreserveLaterEditsAndCredits(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "required-only", true: "clear-both"}[reset], func(t *testing.T) {
			s, path := withdrawalFixture(t)
			defer func() { s.Close() }()
			seedWager(t, s, 60000, 0, 60000)
			if !reset {
				editWagerValues(t, s, wagerValue(50000), nil)
			}
			r, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 10000))
			must(t, err)
			seedWager(t, s, 20000, 1000, 500)
			if reset {
				editWagerValues(t, s, nil, wagerValue(0))
			} else {
				editWagerValues(t, s, wagerValue(80000), nil)
			}
			seedWager(t, s, 10000, 300, 800)
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			must(t, s.ReviewRequest("admin", r.ID, "approved", "仅核实本次申请覆盖的记录"))
			w := currentWager(t, s)
			wantRequired, wantTurnover := int64(40300), int64(1300)
			if reset {
				wantRequired, wantTurnover = 10300, 800
			}
			if w.Required != wantRequired || w.Turnover != wantTurnover || w.Deposit != 30000 || w.Reward != 1300 {
				t.Fatal("approval consumed later activity or replayed adjustment at wrong position", w)
			}
			seedWager(t, s, 700, 0, 25)
			w = currentWager(t, s)
			if w.Required != wantRequired+700 || w.Turnover != wantTurnover+25 {
				t.Fatal("later credits stopped accumulating", w)
			}
		})
	}
}

func TestWagerRequiredOverlappingWithdrawalsAndRejectedWithdrawal(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "overlapping-approvals", true: "rejected"}[reject], func(t *testing.T) {
			s, _ := withdrawalFixture(t)
			defer s.Close()
			seedWager(t, s, 50000, 0, 50000)
			first, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 1000))
			must(t, err)
			seedWager(t, s, 10000, 0, 10000)
			second, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 1000))
			must(t, err)
			editWagerValues(t, s, nil, wagerValue(0))
			seedWager(t, s, 20000, 1000, 5000)
			decision := "approved"
			if reject {
				decision = "rejected"
			}
			for _, r := range []WalletRequest{first, second} {
				must(t, s.ReviewRequest("admin", r.ID, decision, "核实后处理申请"))
				w := currentWager(t, s)
				if w.Required != 21000 || w.Turnover != 5000 {
					t.Fatal("overlap/rejection cleared later records or revived cleared values", w)
				}
			}
		})
	}
}

func TestWagerRequiredResetDoesNotBypassOldWithdrawalRecheck(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	order := seedWager(t, s, 10000, 0, 10000)
	r, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 1000))
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		o, e := get[Order](tx, "orders", order)
		if e != nil {
			return e
		}
		o.Status = "refunded"
		return put(tx, "orders", o.ID, o)
	}))
	editWagerValues(t, s, nil, wagerValue(0))
	if err = s.ReviewRequest("admin", r.ID, "approved", "撤销旧投注后不能绕过原审核"); err == nil {
		t.Fatal("later reset bypassed saved-cutoff eligibility")
	}
	w := currentWager(t, s)
	if w.Required != 0 || w.Turnover != 0 {
		t.Fatal(w)
	}
}

func TestWagerRequiredValidationAuthorizationCASAndReplay(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 4000)
	w := currentWager(t, s)
	base := WagerEdit{Version: w.Version, Revision: w.Revision, Required: wagerValue(5000), Note: "核实门槛", Key: randomID()}
	for _, in := range []WagerEdit{
		{Required: wagerValue(-1)}, {Required: wagerValue(maxMoney + 1)}, {Turnover: wagerValue(-1)}, {Turnover: wagerValue(maxMoney + 1)},
		{Required: wagerValue(1), Turnover: wagerValue(0)},
	} {
		in.Version = w.Version
		in.Revision = w.Revision
		in.Note = "不能接受无效数值"
		in.Key = randomID()
		before := markSixCombinedStoredState(t, s)
		if _, err := s.EditWagering("admin", "member", in); err == nil {
			t.Fatal("invalid edit accepted", in)
		}
		if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("invalid edit wrote data")
		}
	}
	if _, err := s.EditWagering("member", "member", base); err != ErrForbidden {
		t.Fatal("member changed own requirement", err)
	}
	seedWager(t, s, 100, 0, 0)
	if _, err := s.EditWagering("admin", "member", base); err == nil {
		t.Fatal("stale revision accepted")
	}
	w = currentWager(t, s)
	base.Version = w.Version
	base.Revision = w.Revision
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := base
			in.Key = randomID()
			_, err := s.EditWagering("admin", "member", in)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("CAS did not serialize edits", success)
	}
	w = currentWager(t, s)
	base.Version = w.Version
	base.Revision = w.Revision
	base.Required = wagerValue(6000)
	base.Key = randomID()
	first, err := s.EditWagering("admin", "member", base)
	must(t, err)
	before := markSixCombinedStoredState(t, s)
	retry, err := s.EditWagering("admin", "member", base)
	must(t, err)
	if !samePublicWager(first, retry) || !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("retry reapplied edit")
	}
	base.Required = wagerValue(7000)
	if _, err = s.EditWagering("admin", "member", base); err == nil {
		t.Fatal("same key accepted changed requirement")
	}
}

func TestWagerRequiredLegacyEditReceiptCompatibility(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 4000)
	w := currentWager(t, s)
	// Field order and omitted required_cents preserve receipts produced by the
	// deployed editor, whose turnover was always a concrete number (including 0).
	for _, turnover := range []int64{0, 4000} {
		in := WagerEdit{Version: w.Version, Revision: "1:10000:0:4000", Turnover: wagerValue(turnover), Note: "旧版调整", Key: randomID()}
		type oldEdit struct {
			Revision string `json:"revision"`
			Version  int64  `json:"version"`
			Turnover int64  `json:"turnover_cents"`
			Green    bool   `json:"green_channel"`
			Note     string `json:"note"`
			Key      string `json:"key"`
		}
		oldBody := struct {
			ID   string
			Edit oldEdit
		}{"member", oldEdit{in.Revision, in.Version, turnover, false, in.Note, in.Key}}
		newBody := struct {
			ID   string
			Edit WagerEdit
		}{"member", in}
		oldJSON, _ := json.Marshal(oldBody)
		newJSON, _ := json.Marshal(newBody)
		if string(oldJSON) != string(newJSON) {
			t.Fatal("legacy digest shape changed", string(oldJSON), string(newJSON))
		}
		must(t, s.db.Update(func(tx *bolt.Tx) error {
			ref := seq(tx, "wager_edits")
			if err := put(tx, "wager_edits", ref, w); err != nil {
				return err
			}
			return remember(tx, "wager:admin", in.Key, ref, oldBody)
		}))
		before := markSixCombinedStoredState(t, s)
		retry, err := s.EditWagering("admin", "member", in)
		must(t, err)
		if !samePublicWager(retry, w) || !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("old accepted receipt was reapplied")
		}
	}
}
