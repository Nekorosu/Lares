package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

func HashString(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

func HashWithSalt(input, salt string) string {
	h := sha256.Sum256([]byte(input + salt))
	return hex.EncodeToString(h[:])
}

func GenerateRandomToken(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func GenerateRandomID(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateInviteCode creates a 16-character base32 code in format XXXX-XXXX-XXXX-XXXX without ambiguous characters (no 0, O, 1, I, L, 8, B).
func GenerateInviteCode() string {
	const charset = "2345679ACDEFGHJKMNPQRSTVWXYZ"
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			sb.WriteString("-")
		}
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			sb.WriteByte(charset[0])
		} else {
			sb.WriteByte(charset[num.Int64()])
		}
	}
	return sb.String()
}

func FormatCodePrefix(code string) string {
	clean := strings.ReplaceAll(code, "-", "")
	if len(clean) >= 4 {
		return fmt.Sprintf("%s...", clean[:4])
	}
	return "xxxx..."
}
