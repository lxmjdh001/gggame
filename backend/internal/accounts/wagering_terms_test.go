package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestWagerAmountAndTurnoverRemainSeparateAcrossCycles(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	seedWager(t, s, 50000, 2000, 20000)
	r, err := s.CreateRequest("member", RequestInput{Kind: "deposit", Amount: 10000, Note: "再次充值叠加", Key: randomID()})
	must(t, err)
	must(t, s.ReviewRequest("admin", r.ID, "approved", "核实充值到账"))
	w, err := s.Wagering("member", "member")
	must(t, err)
	if w.Deposit != 60000 || w.Reward != 2000 || w.Required != 62000 || w.Turnover != 20000 {
		t.Fatal("充值与奖励应累加为打码量，不能覆盖打码流水", w)
	}
	beforeUser := managementStored(t, s, "member")
	var beforeLedger []Ledger
	var beforeOrders []Order
	must(t, s.db.View(func(tx *bolt.Tx) error {
		var e error
		beforeLedger, e = list[Ledger](tx, "ledger")
		if e != nil {
			return e
		}
		beforeOrders, e = list[Order](tx, "orders")
		return e
	}))
	edit := WagerEdit{Version: w.Version, Revision: w.Revision, Turnover: 71000, Note: "核实后调整打码流水", Key: randomID()}
	w, err = s.EditWagering("admin", "member", edit)
	must(t, err)
	if w.Required != 62000 || w.Deposit != 60000 || w.Reward != 2000 || w.Turnover != 71000 || !w.Eligible {
		t.Fatal("人工只能调整流水，不能修改充值奖励对应的打码量", w)
	}
	if !reflect.DeepEqual(beforeUser, managementStored(t, s, "member")) {
		t.Fatal("流水调整改变会员或余额")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		ledger, e := list[Ledger](tx, "ledger")
		if e != nil {
			return e
		}
		orders, e := list[Order](tx, "orders")
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(beforeLedger, ledger) || !reflect.DeepEqual(beforeOrders, orders) {
			t.Fatal("流水调整重写了原始账变或投注")
		}
		return nil
	}))
	seedWager(t, s, 10000, 0, 3000)
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 72000 || w.Turnover != 74000 {
		t.Fatal("后续充值与投注没有分别累加", w)
	}
	withdraw, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 10000))
	must(t, err)
	pending, err := s.Wagering("member", "member")
	must(t, err)
	if pending.Required != w.Required || pending.Turnover != w.Turnover {
		t.Fatal("提交申请提前清零打码")
	}
	must(t, s.ReviewRequest("admin", withdraw.ID, "rejected", "测试驳回保持累计"))
	rejected, err := s.Wagering("member", "member")
	must(t, err)
	if rejected.Required != w.Required || rejected.Turnover != w.Turnover {
		t.Fatal("提现驳回清零了打码")
	}
	withdraw, err = s.SubmitWalletRequest("member", withdrawalInput(t, s, 10000))
	must(t, err)
	must(t, s.ReviewRequest("admin", withdraw.ID, "approved", "核实提现完成"))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 0 || w.Turnover != 0 || w.Deposit != 0 || w.Reward != 0 {
		t.Fatal("提现成功后打码量与打码流水未清零，或重启后重新计入旧记录", w)
	}
	seedWager(t, s, 10000, 500, 1000)
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 10500 || w.Turnover != 1000 {
		t.Fatal("新一轮累加了已清零的历史记录", w)
	}
}

func TestWagerEditCannotSetRequiredAmountOverHTTP(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 50000, 1000, 20000)
	w, err := s.Wagering("admin", "member")
	must(t, err)
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var e error; token, e = newLogin(tx, "admin"); return e }))
	body, err := json.Marshal(map[string]any{"version": w.Version, "revision": w.Revision, "turnover_cents": 52000, "required_cents": 0, "note": "不能修改打码量", "key": randomID()})
	must(t, err)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	r := httptest.NewRequest("POST", "http://localhost/api/v1/admin/users/member/wagering", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
	response := httptest.NewRecorder()
	h.Protect(mux).ServeHTTP(response, r)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("required_cents must be rejected: %d %s", response.Code, response.Body)
	}
	after, err := s.Wagering("admin", "member")
	must(t, err)
	if after.Required != w.Required || after.Turnover != w.Turnover || after.Revision != w.Revision {
		t.Fatal("rejected request changed wagering", after)
	}
}

func TestWagerEqualityAndSubmissionCutoffPreserveLaterActivity(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	seedWager(t, s, 60000, 0, 59999)
	w, err := s.Wagering("member", "member")
	must(t, err)
	if w.Eligible || w.Remaining != 1 {
		t.Fatal("打码流水差一分时不应达标", w)
	}
	in := withdrawalInput(t, s, 10000)
	if _, err = s.SubmitWalletRequest("member", in); err == nil {
		t.Fatal("打码流水尚未达到打码量时允许了提现")
	}
	seedWager(t, s, 0, 0, 1)
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 60000 || w.Turnover != 60000 || !w.Eligible || w.Remaining != 0 {
		t.Fatal("打码量600元、打码流水600元时应允许提现", w)
	}
	withdraw, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	// New recharge/reward and bets belong to the next cycle. The new full
	// total is deliberately ineligible, but cannot invalidate this request.
	seedWager(t, s, 20000, 1000, 500)
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 81000 || w.Turnover != 60500 || w.Eligible {
		t.Fatal("申请后新增活动没有继续累计", w)
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	must(t, s.ReviewRequest("admin", withdraw.ID, "approved", "仅按提交时覆盖的记录审核"))
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Deposit != 20000 || w.Reward != 1000 || w.Required != 21000 || w.Turnover != 500 || w.Eligible {
		t.Fatal("审核清除了提交后的记录，或没有清除本次申请覆盖的记录", w)
	}
	if _, err = s.SubmitWalletRequest("member", withdrawalInput(t, s, 1000)); err == nil {
		t.Fatal("下一轮流水未达标时允许了新提现")
	}
	seedWager(t, s, 0, 0, 20500)
	next, err := s.SubmitWalletRequest("member", withdrawalInput(t, s, 1000))
	must(t, err)
	must(t, s.ReviewRequest("admin", next.ID, "approved", "新一轮相等也可提现"))
	w, err = s.Wagering("member", "member")
	must(t, err)
	if w.Required != 0 || w.Turnover != 0 {
		t.Fatal("无新增活动时两项应归零", w)
	}
}
