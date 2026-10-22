package accounts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestPendingRequestsSummaryAndReview(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	empty, err := s.PendingRequests("admin")
	must(t, err)
	if empty.Total != 0 || empty.Items == nil || empty.LatestID != "" {
		t.Fatal("invalid empty state")
	}
	if _, err = s.PendingRequests("member"); !errors.Is(err, ErrForbidden) {
		t.Fatal("member can read admin requests")
	}
	var requests []WalletRequest
	for i := 0; i < 27; i++ {
		kind := "deposit"
		if i%2 == 1 {
			kind = "withdraw"
		}
		r, err := s.CreateRequest("member", RequestInput{Kind: kind, Amount: 100, Note: "资金提醒测试", Key: randomID()})
		must(t, err)
		requests = append(requests, r)
	}
	must(t, s.ReviewRequest("admin", requests[26].ID, "approved", "核对充值"))
	must(t, s.ReviewRequest("admin", requests[25].ID, "rejected", "驳回提现"))
	snapshot, err := s.PendingRequests("admin")
	must(t, err)
	if snapshot.Total != 25 || snapshot.DepositCount != 13 || snapshot.WithdrawCount != 12 || snapshot.DepositAmount != 1300 || snapshot.WithdrawAmount != 1200 || len(snapshot.Items) != 5 || snapshot.LatestID != requests[24].ID {
		t.Fatalf("incorrect full count: %+v", snapshot)
	}
	for i, r := range snapshot.Items {
		if r.ID != requests[24-i].ID || r.Status != "pending" {
			t.Fatal("incorrect pending order")
		}
	}
	// Equal total counts must still expose the higher ID of a newly arrived request.
	must(t, s.ReviewRequest("admin", requests[24].ID, "approved", "核对充值"))
	next, err := s.CreateRequest("member", RequestInput{Kind: "deposit", Amount: 200, Note: "新的充值", Key: randomID()})
	must(t, err)
	changed, err := s.PendingRequests("admin")
	must(t, err)
	if changed.Total != snapshot.Total || changed.LatestID <= snapshot.LatestID || changed.LatestID != next.ID {
		t.Fatal("new arrival hidden by unchanged count")
	}
	// Nothing in the reminder may process funds.
	assertFunds(t, s, "member", 99000, 0, 1200)
}

func TestPendingRequestsHTTPAccess(t *testing.T) {
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
	_, err := s.CreateRequest("member", RequestInput{Kind: "deposit", Amount: 2500, Note: "待审核充值", Key: randomID()})
	must(t, err)
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {memberToken, 403}, {adminToken, 200}} {
		req := httptest.NewRequest("GET", "http://localhost/api/v1/admin/requests/pending", nil)
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("expected %d got %d: %s", tc.status, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var response struct {
				Data PendingRequests `json:"data"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &response))
			if response.Data.Total != 1 || response.Data.Items[0].Amount != 2500 {
				t.Fatal("route returned wrong response")
			}
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private notice may be cached")
		}
	}
}
