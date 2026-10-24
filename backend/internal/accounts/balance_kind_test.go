package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestBalanceKindsPersistReplayAndAccumulateWagering(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	before, err := s.Wagering("member", "member")
	must(t, err)
	depositKey, rewardKey := randomID(), randomID()
	must(t, s.AdjustWithKind("admin", "member", 40800, "人工核对款项", depositKey, "deposit"))
	must(t, s.AdjustWithKind("admin", "member", 88800, "活动赠送", rewardKey, "reward"))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.AdjustWithKind("admin", "member", 40800, "人工核对款项", depositKey, "deposit")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	if err := s.AdjustWithKind("admin", "member", 40800, "人工核对款项", depositKey, "reward"); err == nil {
		t.Fatal("replay with a different kind must fail")
	}
	assertFunds(t, s, "member", 229600, 0, 0)
	page, err := s.LedgerPage("member", 1)
	must(t, err)
	kinds := map[string]int64{}
	for _, row := range page.Items {
		kinds[row.Kind] += row.Amount
	}
	if page.Total != 2 || kinds["admin_deposit"] != 40800 || kinds["admin_reward"] != 88800 {
		t.Fatalf("incorrect classified ledger: %+v", page.Items)
	}
	after, err := s.Wagering("member", "member")
	must(t, err)
	if after.Deposit != before.Deposit+40800 || after.Reward != before.Reward+88800 || after.Required != before.Required+129600 || after.Turnover != before.Turnover {
		t.Fatal("classified deposits and rewards must accumulate without resetting turnover", after)
	}
	must(t, s.AdjustWithKind("admin", "member", -800, "奖励金额更正", randomID(), "reward"))
	assertFunds(t, s, "member", 228800, 0, 0)
	financial := managementFinancialSnapshot(t, s)
	for _, tc := range []struct {
		actor, kind string
		amount      int64
	}{
		{"member", "deposit", 100}, {"admin", "other", 100}, {"admin", "", 100},
		{"admin", "reward", 0}, {"admin", "deposit", -228801}, {"admin", "reward", 100000001},
	} {
		if err := s.AdjustWithKind(tc.actor, "member", tc.amount, "验证无效请求", randomID(), tc.kind); err == nil {
			t.Fatalf("invalid adjustment accepted: %+v", tc)
		}
	}
	if managementFinancialSnapshot(t, s) != financial {
		t.Fatal("rejected request changed financial records")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		rows, err := list[Audit](tx, "audit")
		if err != nil {
			return err
		}
		var adjustments int
		for _, row := range rows {
			if row.Action == "balance.adjust" {
				adjustments++
				if !strings.Contains(row.Detail, "admin_deposit") && !strings.Contains(row.Detail, "admin_reward") {
					t.Fatal("audit is missing adjustment kind")
				}
			}
		}
		if adjustments != 3 {
			t.Fatalf("duplicate or missing adjustment audit: %d", adjustments)
		}
		return nil
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	page, err = s.LedgerPage("member", 1)
	must(t, err)
	if page.Total != 3 || page.Items[0].Kind != "admin_reward" || page.Items[0].Amount != -800 {
		t.Fatal("classification did not survive reopen")
	}
	assertFunds(t, s, "member", 228800, 0, 0)
}

func TestLegacyAdjustmentKindsAreDisplayOnly(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	for _, note := range []string{" 充值 ", "奖励", "奖励发放失败后更正"} {
		must(t, s.Adjust("admin", "member", 100, note, randomID()))
	}
	before := managementFinancialSnapshot(t, s)
	page, err := s.LedgerPage("member", 1)
	must(t, err)
	for _, row := range page.Items {
		want := map[string]string{" 充值 ": "admin_deposit", "奖励": "admin_reward", "奖励发放失败后更正": "admin_adjust"}[row.Note]
		if row.Kind != want {
			t.Fatalf("ambiguous legacy note misclassified: %+v", row)
		}
	}
	account, err := s.Account("member")
	must(t, err)
	adminRows, err := s.AdminData("admin", "ledger", "", 1)
	must(t, err)
	for _, value := range []any{account["ledger"], adminRows} {
		raw, err := json.Marshal(value)
		must(t, err)
		for _, kind := range []string{"admin_deposit", "admin_reward", "admin_adjust"} {
			if !strings.Contains(string(raw), `"kind":"`+kind+`"`) {
				t.Fatalf("missing display kind %s", kind)
			}
		}
	}
	if before != managementFinancialSnapshot(t, s) {
		t.Fatal("display reads rewrote stored financial records")
	}
}

func TestBalanceKindsHTTPAndLegacyCompatibility(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var adminToken, memberToken string
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		var err error
		adminToken, err = newLogin(tx, "admin")
		if err != nil {
			return err
		}
		memberToken, err = newLogin(tx, "member")
		return err
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	key := randomID()
	for _, tc := range []struct {
		token, origin, kind, key string
		code                     int
	}{
		{"", "", "deposit", randomID(), 401},
		{memberToken, "", "deposit", randomID(), 403},
		{adminToken, "https://other.example", "deposit", randomID(), 403},
		{adminToken, "", "unknown", randomID(), 400},
		{adminToken, "", "deposit", key, 200},
		{adminToken, "", "deposit", key, 200},
		{adminToken, "", "reward", key, 400},
		{adminToken, "", "reward", randomID(), 200},
		{adminToken, "", "", randomID(), 200},
	} {
		body := map[string]any{"amount_cents": 100, "note": "分类入账", "key": tc.key}
		if tc.kind != "" {
			body["kind"] = tc.kind
		}
		raw, err := json.Marshal(body)
		must(t, err)
		r := httptest.NewRequest("POST", "http://localhost/api/v1/admin/users/member/balance", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("kind %q: %d %s, want %d", tc.kind, w.Code, w.Body, tc.code)
		}
	}
	assertFunds(t, s, "member", 100300, 0, 0)
	page, err := s.LedgerPage("member", 1)
	must(t, err)
	if page.Total != 3 {
		t.Fatal("HTTP idempotency or rejection failed")
	}
}
