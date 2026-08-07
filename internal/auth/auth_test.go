package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	password := "SuperSecretPass123!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	valid, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword error: %v", err)
	}
	if !valid {
		t.Fatal("Expected password to verify successfully, but got false")
	}

	validWrong, _ := VerifyPassword("WrongPassword123!", hash)
	if validWrong {
		t.Fatal("Expected wrong password to fail verification, but got true")
	}
}

func TestTOTPValidation(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("Failed to generate TOTP secret: %v", err)
	}

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatalf("Failed to decode base32 secret: %v", err)
	}

	now := time.Now().Unix()
	code := generateCode(key, now/30)

	if !ValidateTOTP(secret, code) {
		t.Fatalf("Expected valid TOTP code %s for secret %s to validate", code, secret)
	}
}
