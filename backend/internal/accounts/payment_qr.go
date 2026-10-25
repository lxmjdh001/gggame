package accounts

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

const maxPaymentQRBytes = 256 * 1024

// Embedded raster images avoid third-party tracking, remote fetches and SVG
// scripts. Bound both compressed size and decoded pixels before full decoding.
func validatePaymentQR(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > base64.StdEncoding.EncodedLen(maxPaymentQRBytes)+32 {
		return errors.New("二维码图片不能超过256KB")
	}
	format := ""
	for _, kind := range []string{"png", "jpeg"} {
		if strings.HasPrefix(value, "data:image/"+kind+";base64,") {
			format = kind
			break
		}
	}
	if format == "" {
		return errors.New("二维码仅支持PNG或JPEG图片，不支持链接或SVG")
	}
	encoded := strings.TrimPrefix(value, "data:image/"+format+";base64,")
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) > maxPaymentQRBytes || base64.StdEncoding.EncodeToString(raw) != encoded {
		return errors.New("二维码图片内容不正确或超过256KB")
	}
	cfg, actual, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || actual != format || cfg.Width < 64 || cfg.Height < 64 || cfg.Width > 2048 || cfg.Height > 2048 {
		return errors.New("二维码图片须为64–2048像素的PNG或JPEG图片")
	}
	if _, decoded, err := image.Decode(bytes.NewReader(raw)); err != nil || decoded != format {
		return errors.New("二维码图片已损坏，请重新上传")
	}
	return nil
}

func paymentSettingsForAudit(cfg PaymentSettings) PaymentSettings {
	for _, value := range []*string{&cfg.QRCodeDataURL, &cfg.ManualQRCodeDataURL} {
		if *value != "" {
			*value = fmt.Sprintf("image-sha256:%x", sha256.Sum256([]byte(*value)))
		}
	}
	return cfg
}
