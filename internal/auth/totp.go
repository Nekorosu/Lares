package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 10) // 80 bits is standard, 16 base32 chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func ValidateTOTP(secret, passcode string) bool {
	passcode = strings.TrimSpace(passcode)
	if len(passcode) != 6 {
		return false
	}

	secretUpper := strings.ToUpper(strings.TrimSpace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretUpper)
	if err != nil {
		return false
	}

	now := time.Now().Unix()
	step := int64(30)

	// Check current time step and +/- 5 window for clock drift tolerance
	for _, offset := range []int64{-5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5} {
		t := (now / step) + offset
		if generateCode(key, t) == passcode {
			return true
		}
	}


	return false
}

func generateCode(key []byte, counter int64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	h := hmac.New(sha1.New, key)
	h.Write(buf)
	sum := h.Sum(nil)

	offset := sum[len(sum)-1] & 0xf
	binCode := (int32(sum[offset]&0x7f) << 24) |
		(int32(sum[offset+1]&0xff) << 16) |
		(int32(sum[offset+2]&0xff) << 8) |
		(int32(sum[offset+3] & 0xff))

	otp := binCode % 1000000
	return fmt.Sprintf("%06d", otp)
}

func GenerateTOTPQRCodePNG(username, secret, issuer string) ([]byte, error) {
	authURL := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s",
		url.PathEscape(issuer), url.PathEscape(username), secret, url.QueryEscape(issuer))

	png, err := qrcode.Encode(authURL, qrcode.Medium, 256)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %w", err)
	}

	return png, nil
}
