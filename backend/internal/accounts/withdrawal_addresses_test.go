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

func TestSavedWithdrawalAddressesIsolationVersionsAndRestart(t *testing.T) {
	s, path := withdrawalFixture(t)
	defer func() { s.Close() }()
	beforeUser := managementStored(t, s, "member")
	financial := managementFinancialSnapshot(t, s)
	empty, err := s.WithdrawalAddresses("member")
	must(t, err)
	if len(empty) != 7 || empty[0].Version != 0 || empty[0].Address != "" {
		t.Fatal(empty)
	}
	trc := "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	erc := "0x" + strings.Repeat("a", 40)
	first, err := s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Address: " " + trc + " "})
	must(t, err)
	if first.Version != 1 || first.Address != trc {
		t.Fatal(first)
	}
	replay, err := s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Address: trc})
	must(t, err)
	if replay != first {
		t.Fatal("lost-response retry changed record")
	}
	_, err = s.SaveWithdrawalAddress("member", "ERC20", WithdrawalAddress{Address: erc})
	must(t, err)
	second, err := s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Version: 1, Address: "T" + strings.Repeat("A", 33)})
	must(t, err)
	if second.Version != 2 {
		t.Fatal(second)
	}
	if _, err = s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Version: 1, Address: trc}); err == nil {
		t.Fatal("stale edit overwrote new destination")
	}
	for _, bad := range []WithdrawalAddress{{Network: "ERC20", Address: trc}, {Version: -1, Address: trc}, {Address: erc}, {Address: "T" + strings.Repeat("0", 33)}} {
		if _, err = s.SaveWithdrawalAddress("member", "TRC20", bad); err == nil {
			t.Fatal("invalid destination accepted", bad)
		}
	}
	if _, err = s.SaveWithdrawalAddress("member", "BEP20", WithdrawalAddress{Address: erc}); err == nil {
		t.Fatal("unsupported network")
	}
	other, err := s.WithdrawalAddresses("admin")
	must(t, err)
	if other[0].Address != "" || other[1].Address != "" {
		t.Fatal("another account received member addresses")
	}
	if financial != managementFinancialSnapshot(t, s) || !reflect.DeepEqual(beforeUser, managementStored(t, s, "member")) {
		t.Fatal("saved address changed funds, transactions or credentials")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	got, err := s.WithdrawalAddresses("member")
	must(t, err)
	if got[0] != second || got[1].Address != erc {
		t.Fatal("addresses not preserved across restart", got)
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		rows, e := list[Audit](tx, "audit")
		if e != nil {
			return e
		}
		count := 0
		for _, row := range rows {
			if row.Action == "withdrawal_address.save" {
				count++
				if strings.Contains(row.Detail, trc) || strings.Contains(row.Detail, erc) {
					t.Fatal("audit leaked saved destination")
				}
			}
		}
		if count != 3 {
			t.Fatal("missing or duplicate address audit", count)
		}
		return nil
	}))
}

func TestSavedAddressChangesNeverChangeAcceptedWithdrawal(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	seedWager(t, s, 10000, 0, 10000)
	a, err := s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Address: "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"})
	must(t, err)
	in := withdrawalInput(t, s, 1000)
	in.PaymentMethod = "usdt"
	in.PayoutNetwork = a.Network
	in.PayoutAddress = a.Address
	r, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	_, err = s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Version: a.Version, Address: "T" + strings.Repeat("B", 33)})
	must(t, err)
	retry, err := s.SubmitWalletRequest("member", in)
	must(t, err)
	if retry.ID != r.ID || retry.Withdrawal.Address != a.Address {
		t.Fatal("saved edit redirected accepted withdrawal")
	}
	in.PayoutAddress = "T" + strings.Repeat("B", 33)
	if _, err = s.SubmitWalletRequest("member", in); err == nil {
		t.Fatal("replay redirected accepted withdrawal")
	}
}

func TestSavedWithdrawalAddressHTTPAuthenticationAndOwnership(t *testing.T) {
	s, _ := withdrawalFixture(t)
	defer s.Close()
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var e error; token, e = newLogin(tx, "member"); return e }))
	request := func(method, path, credential, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost/api/v1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if credential != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: credential})
		}
		w := httptest.NewRecorder()
		h.Protect(mux).ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"GET", "PUT"} {
		path := "/wallet/withdrawal-addresses"
		if method == "PUT" {
			path += "/TRC20"
		}
		if w := request(method, path, "", "{}", ""); w.Code != 401 {
			t.Fatal(w.Code, w.Body)
		}
	}
	body := `{"version":0,"address":"T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"}`
	if w := request("PUT", "/wallet/withdrawal-addresses/TRC20", token, body, "https://foreign.invalid"); w.Code != 403 {
		t.Fatal("cross-origin write accepted", w.Code)
	}
	if w := request("PUT", "/wallet/withdrawal-addresses/TRC20", token, strings.TrimSuffix(body, "}")+`,"user_id":"admin"}`, ""); w.Code != 400 {
		t.Fatal("caller supplied owner accepted", w.Code)
	}
	if w := request("PUT", "/wallet/withdrawal-addresses/TRC20?user_id=admin", token, body, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w := request("GET", "/wallet/withdrawal-addresses", token, "", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal(w.Code, w.Header())
	}
	var data struct {
		Data []WithdrawalAddress `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &data))
	if data.Data[0].Version != 1 {
		t.Fatal(data)
	}
	admin, _ := s.WithdrawalAddresses("admin")
	if admin[0].Address != "" {
		t.Fatal("query string changed ownership")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "member")
		if e != nil {
			return e
		}
		u.Status = "disabled"
		return put(tx, "users", u.ID, u)
	}))
	if _, err := s.SaveWithdrawalAddress("member", "TRC20", WithdrawalAddress{Version: 1, Address: data.Data[0].Address}); err != ErrForbidden {
		t.Fatal("disabled user saved address", err)
	}
}
