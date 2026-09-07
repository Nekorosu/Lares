package auth

import (
	"crypto/hmac"
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
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(input))
	return hex.EncodeToString(mac.Sum(nil))
}

func GenerateRandomToken(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func GenerateRandomID(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// GenerateInviteCode uses 32 symbols, excluding 0/O and 1/I, in four groups of four.
func GenerateInviteCode() string {
	const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			sb.WriteString("-")
		}
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
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

func NormalizeInviteCode(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))

	cyrToLat := map[rune]rune{
		'А': 'A', 'В': 'B', 'С': 'C', 'Е': 'E', 'Н': 'H',
		'К': 'K', 'М': 'M', 'О': 'O', 'Р': 'P', 'Т': 'T',
		'Х': 'X', 'У': 'Y', 'а': 'A', 'в': 'B', 'с': 'C',
		'е': 'E', 'н': 'H', 'к': 'K', 'м': 'M', 'о': 'O',
		'р': 'P', 'т': 'T', 'х': 'X', 'у': 'Y',
	}

	var sb strings.Builder
	for _, r := range c {
		if lat, ok := cyrToLat[r]; ok {
			sb.WriteRune(lat)
		} else {
			sb.WriteRune(r)
		}
	}
	c = sb.String()
	c = strings.ReplaceAll(c, " ", "")
	c = strings.ReplaceAll(c, "-", "")

	if len(c) == 16 {
		return fmt.Sprintf("%s-%s-%s-%s", c[0:4], c[4:8], c[8:12], c[12:16])
	}
	return c
}
