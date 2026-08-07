package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	ArgonTime    = 1
	ArgonMemory  = 64 * 1024 // 64 MB
	ArgonThreads = 4
	ArgonKeyLen  = 32
	SaltLen      = 16
)

var (
	ErrPasswordTooShort   = errors.New("пароль должен состоять минимум из 12 символов")
	ErrPasswordTooLong    = errors.New("пароль не должен превышать 256 символов")
	ErrPasswordEqualsUser = errors.New("пароль не должен совпадать с именем пользователя")
	ErrCommonPassword     = errors.New("выбран слишком простой или распространенный пароль")
)

var commonPasswords = map[string]bool{
	"admin":         true,
	"admin123":      true,
	"password":      true,
	"123456":        true,
	"12345678":      true,
	"123456789":     true,
	"administrator": true,
}

func ValidatePassword(username, password string) error {
	if len(password) < 12 {
		return ErrPasswordTooShort
	}
	if len(password) > 256 {
		return ErrPasswordTooLong
	}
	if strings.EqualFold(username, password) {
		return ErrPasswordEqualsUser
	}
	if commonPasswords[strings.ToLower(password)] {
		return ErrCommonPassword
	}
	return nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, ArgonTime, ArgonMemory, ArgonThreads, ArgonKeyLen)

	// Format: $argon2id$v=19$m=65536,t=1,p=4$<salt_hex>$<hash_hex>
	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		ArgonMemory, ArgonTime, ArgonThreads, hex.EncodeToString(salt), hex.EncodeToString(hash))

	return encoded, nil
}

func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) < 6 {
		return false, errors.New("invalid hash parts")
	}

	var memory uint32
	var time uint32
	var threads uint8

	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false, fmt.Errorf("invalid hash params: %w", err)
	}

	salt, err := hex.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("invalid salt hex: %w", err)
	}

	expectedHash, err := hex.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("invalid hash hex: %w", err)
	}

	calculatedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(calculatedHash, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}
