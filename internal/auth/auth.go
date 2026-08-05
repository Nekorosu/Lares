package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"math"
	"net/url"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/argon2"
)

// Password Policy Constants
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
	ArgonTime      = 1
	ArgonMemory    = 64 * 1024
	ArgonThreads   = 4
	ArgonKeyLen    = 32
	SaltLen        = 16
)

var CommonPasswords = map[string]bool{
	"password1234": true,
	"123456789012": true,
	"administrator": true,
	"admin1234567": true,
	"qwerty123456": true,
}

func ValidatePassword(username, password string) error {
	if len(password) < MinPasswordLen {
		return fmt.Errorf("пароль должен быть не менее %d символов", MinPasswordLen)
	}
	if len(password) > MaxPasswordLen {
		return fmt.Errorf("пароль должен быть не более %d символов", MaxPasswordLen)
	}
	if strings.EqualFold(username, password) {
		return errors.New("пароль не может совпадать с именем пользователя")
	}
	if CommonPasswords[strings.ToLower(password)] {
		return errors.New("выбран слишком простой или распространенный пароль")
	}
	return nil
}

// HashPassword generates an Argon2id hash with embedded parameters and salt:
// format: $argon2id$v=19$m=65536,t=1,p=4$SALT_HEX$HASH_HEX
func HashPassword(password string) (string, error) {
	salt := make([]byte, SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, ArgonTime, ArgonMemory, ArgonThreads, ArgonKeyLen)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		ArgonMemory, ArgonTime, ArgonThreads,
		hex.EncodeToString(salt), hex.EncodeToString(hash)), nil
}

func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory uint32
	var time uint32
	var threads uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false
	}
	salt, err := hex.DecodeString(parts[4])
	if err != nil {
		return false
	}
	hash, err := hex.DecodeString(parts[5])
	if err != nil {
		return false
	}

	calculatedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(hash)))
	return hmac.Equal(hash, calculatedHash)
}

// GenerateInviteCode creates a 16-char base32 formatted string: XXXX-XXXX-XXXX-XXXX
// Using un-ambiguous charset (A-Z, 2-7, omitting 0/O, 1/I if needed, standard base32 is ok)
func GenerateInviteCode() (string, string) {
	b := make([]byte, 10)
	rand.Read(b)
	encoder := base32.StdEncoding.WithPadding(base32.NoPadding)
	raw := encoder.EncodeToString(b)
	raw = strings.ToUpper(raw)
	if len(raw) > 16 {
		raw = raw[:16]
	}
	formatted := fmt.Sprintf("%s-%s-%s-%s", raw[0:4], raw[4:8], raw[8:12], raw[12:16])
	hash := HashString(formatted)
	return formatted, hash
}

// HashString produces SHA-256 hash string
func HashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// HashIP creates a salted HMAC-SHA256 of IP address for privacy in audit logs
func HashIP(ip, salt string) string {
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// GenerateRandomToken generates secure random string and its hash
func GenerateRandomToken() (string, string) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	hash := HashString(token)
	return token, hash
}

// --- TOTP Utilities (RFC 6238) ---

func GenerateTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func ValidateTOTP(secret string, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	now := time.Now().Unix()
	step := int64(30)

	// Allow window of -1, 0, +1
	for t := -1; t <= 1; t++ {
		counter := (now / step) + int64(t)
		if generateTOTPCode(secret, counter) == code {
			return true
		}
	}
	return false
}

func generateTOTPCode(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha256.New, key)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff
	otp := truncated % uint32(math.Pow10(6))

	return fmt.Sprintf("%06d", otp)
}

func GenerateTOTPQR(username, secret, issuer string) (string, error) {
	uri := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA256&digits=6&period=30",
		url.PathEscape(issuer), url.PathEscape(username), secret, url.QueryEscape(issuer))
	png, err := qrcode.Encode(uri, qrcode.Medium, 200)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64Encode(png), nil
}

func base64Encode(b []byte) string {
	const base64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	for i := 0; i < len(b); i += 3 {
		val := uint32(b[i]) << 16
		if i+1 < len(b) {
			val |= uint32(b[i+1]) << 8
		}
		if i+2 < len(b) {
			val |= uint32(b[i+2])
		}
		result.WriteByte(base64Table[(val>>18)&0x3F])
		result.WriteByte(base64Table[(val>>12)&0x3F])
		if i+1 < len(b) {
			result.WriteByte(base64Table[(val>>6)&0x3F])
		} else {
			result.WriteByte('=')
		}
		if i+2 < len(b) {
			result.WriteByte(base64Table[val&0x3F])
		} else {
			result.WriteByte('=')
		}
	}
	return result.String()
}
