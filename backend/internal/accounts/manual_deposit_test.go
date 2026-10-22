package accounts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestManualDepositTransactionIDAndReplay(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	input := WalletSubmission{RequestInput: RequestInput{Kind: "deposit", Amount: 12345, Note: "存款凭证", Key: randomID()}, TransactionID: "  Bank/20261020-ABC_01  "}
	for _, id := range []string{"", "  ", "abc\ndef", strings.Repeat("a", 129), strings.Repeat("中", 43)} {
		invalid := input
		invalid.Key = randomID()
		invalid.TransactionID = id
		if _, err := s.SubmitWalletRequest("member", invalid); err == nil {
			t.Fatalf("invalid ID accepted: %q", id)
		}
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	pending, err := s.PendingRequests("admin")
	must(t, err)
	if pending.Total != 0 {
		t.Fatal("invalid ID created request")
	}
	request, err := s.SubmitWalletRequest("member", input)
	must(t, err)
	if request.TransactionID != "Bank/20261020-ABC_01" || request.Status != "pending" || request.Amount != 12345 {
		t.Fatal("missing ID or wrong request", request)
	}
	assertFunds(t, s, "member", 100000, 0, 0)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry, err := s.SubmitWalletRequest("member", input)
			if err != nil || retry.ID != request.ID {
				t.Errorf("unsafe retry: %+v %v", retry, err)
			}
		}()
	}
	wg.Wait()
	changed := input
	changed.TransactionID = "DIFFERENT-ID"
	if _, err := s.SubmitWalletRequest("member", changed); err == nil {
		t.Fatal("accepted changed ID with same request key")
	}
	changed.TransactionID = ""
	if _, err := s.SubmitWalletRequest("member", changed); err == nil {
		t.Fatal("accepted missing ID with same request key")
	}
	pending, err = s.PendingRequests("admin")
	must(t, err)
	if pending.Total != 1 || pending.Items[0].TransactionID != request.TransactionID {
		t.Fatal("repeated request or missing admin proof", pending)
	}
	found, err := s.AdminData("admin", "requests", request.TransactionID, 1)
	must(t, err)
	if found["total"] != 1 {
		t.Fatal("cannot search transaction ID", found)
	}
	mine, err := s.TransactionsPage("member", 1)
	must(t, err)
	if len(mine.Items) != 1 || mine.Items[0].TransactionID != request.TransactionID {
		t.Fatal("member record lost proof", mine)
	}
	other, err := s.TransactionsPage("admin", 1)
	must(t, err)
	if len(other.Items) != 0 {
		t.Fatal("another account can see deposit proof")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	retry, err := s.SubmitWalletRequest("member", input)
	must(t, err)
	if retry.ID != request.ID || retry.TransactionID != request.TransactionID {
		t.Fatal("restart lost proof", retry)
	}
	must(t, s.ReviewRequest("admin", request.ID, "approved", "核实交易后入账"))
	assertFunds(t, s, "member", 112345, 0, 0)
	if err := s.ReviewRequest("admin", request.ID, "approved", "不得重复入账"); err == nil {
		t.Fatal("credited twice")
	}
}

func TestManualDepositTransactionIDHTTP(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var err error; token, err = newLogin(tx, "member"); return err }))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	for _, tc := range []struct {
		id, token string
		status    int
	}{{"", token, 400}, {"bank-ref-20261020", token, 200}, {"bank-ref-20261020", "", 401}} {
		input := WalletSubmission{RequestInput: RequestInput{Kind: "deposit", Amount: 100, Note: "付款凭证", Key: randomID()}, TransactionID: tc.id}
		body, err := json.Marshal(input)
		must(t, err)
		r := httptest.NewRequest("POST", "http://localhost/api/v1/wallet/requests", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost")
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body)
		}
		if tc.status == 200 {
			var response struct {
				Data WalletRequest `json:"data"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &response))
			if response.Data.TransactionID != tc.id {
				t.Fatal("HTTP lost ID", response)
			}
		}
	}
}
