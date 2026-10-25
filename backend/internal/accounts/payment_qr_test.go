package accounts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func paymentQRFixture(t *testing.T, format string, size int, noise bool) string {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	if noise {
		_, err := rand.New(rand.NewSource(1)).Read(im.Pix)
		must(t, err)
	}
	var data bytes.Buffer
	if format == "png" {
		must(t, png.Encode(&data, im))
	} else {
		must(t, jpeg.Encode(&data, im, nil))
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}

func TestPaymentQRValidation(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		must(t, validatePaymentQR(paymentQRFixture(t, format, 64, false)))
	}
	valid := paymentQRFixture(t, "png", 64, false)
	for _, bad := range []string{"https://tracking.invalid/qr.png", "javascript:alert(1)", "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>")), strings.Replace(valid, "image/png", "image/jpeg", 1), "data:image/png;base64,%%%", paymentQRFixture(t, "png", 32, false), paymentQRFixture(t, "png", 2049, false), "data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxPaymentQRBytes)+4), valid[:len(valid)-24]} {
		if validatePaymentQR(bad) == nil {
			t.Fatal("invalid image accepted", len(bad))
		}
	}
}

func TestPaymentQRPermissionsExposurePersistenceAndAudit(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	cfg := enabledUSDTSettings(t, s)
	cfg.QRCodeDataURL = paymentQRFixture(t, "png", 64, false)
	cfg.ManualQRCodeDataURL = paymentQRFixture(t, "jpeg", 64, false)
	if _, err := s.SavePaymentSettings("member", cfg); err != ErrForbidden {
		t.Fatal(err)
	}
	cfg, err := s.SavePaymentSettings("admin", cfg)
	must(t, err)
	public, err := s.DepositPaymentSettings("member")
	must(t, err)
	if public.QRCodeDataURL != cfg.QRCodeDataURL || public.ManualQRCodeDataURL != "" {
		t.Fatal("configured images not exposed")
	}
	changed := cfg
	changed.Address = "T" + strings.Repeat("A", 33)
	if _, err = s.SavePaymentSettings("admin", changed); err == nil {
		t.Fatal("old QR retained after address change")
	}
	changed.QRCodeDataURL = ""
	cfg, err = s.SavePaymentSettings("admin", changed)
	must(t, err)
	cfg.Enabled = false
	cfg, err = s.SavePaymentSettings("admin", cfg)
	must(t, err)
	public, err = s.DepositPaymentSettings("member")
	must(t, err)
	if public.Enabled || public.Address != "" || public.QRCodeDataURL != "" || public.ManualQRCodeDataURL != "" {
		t.Fatal("disabled USDT or retired manual QR leaked")
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	got, err := s.PaymentSettings("admin")
	must(t, err)
	if got != cfg {
		t.Fatal("QR settings lost after restart")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		rows, e := list[Audit](tx, "audit")
		if e != nil {
			return e
		}
		found := false
		for _, row := range rows {
			if row.Action == "payment_settings.update" {
				if strings.Contains(row.Detail, "data:image/") {
					t.Fatal("audit duplicated full QR image")
				}
				found = found || strings.Contains(row.Detail, "image-sha256:")
			}
		}
		if !found {
			t.Fatal("missing image audit fingerprint")
		}
		return nil
	}))
}

func TestPaymentQRHTTPBoundedLargeImageUpload(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	cfg := enabledUSDTSettings(t, s)
	cfg.QRCodeDataURL = paymentQRFixture(t, "png", 192, true)
	cfg.ManualQRCodeDataURL = cfg.QRCodeDataURL
	body, err := json.Marshal(cfg)
	must(t, err)
	if len(body) <= 131072 || len(body) >= 786432 {
		t.Fatal("fixture did not exercise dedicated body limit", len(body))
	}
	var token string
	must(t, s.db.Update(func(tx *bolt.Tx) error { var e error; token, e = newLogin(tx, "admin"); return e }))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	r := httptest.NewRequest("PUT", "http://localhost/api/v1/admin/payment-settings", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
	w := httptest.NewRecorder()
	h.Protect(mux).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	r = httptest.NewRequest("PUT", "http://localhost/api/v1/admin/payment-settings", strings.NewReader(strings.Repeat(" ", 786433)))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "starlight_session", Value: token})
	w = httptest.NewRecorder()
	h.Protect(mux).ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("oversized request accepted", w.Code)
	}
}
