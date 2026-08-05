package ratelimit

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"homeshare/internal/models"
)

type Limiter struct {
	db *sql.DB

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func NewLimiter(db *sql.DB) *Limiter {
	return &Limiter{
		db:       db,
		attempts: make(map[string][]time.Time),
	}
}

func (l *Limiter) IsLocked(key string) (bool, string, time.Time, error) {
	var lock models.RateLimitLock
	row := l.db.QueryRow(`
		SELECT key, type, reason, expires_at, created_at
		FROM rate_limit_locks
		WHERE key = ? AND expires_at > ?
	`, key, time.Now())

	err := row.Scan(&lock.Key, &lock.Type, &lock.Reason, &lock.ExpiresAt, &lock.CreatedAt)
	if err == sql.ErrNoRows {
		return false, "", time.Time{}, nil
	}
	if err != nil {
		return false, "", time.Time{}, err
	}
	return true, lock.Reason, lock.ExpiresAt, nil
}

func (l *Limiter) Lock(key string, lockType string, reason string, duration time.Duration) error {
	expiresAt := time.Now().Add(duration)
	_, err := l.db.Exec(`
		INSERT INTO rate_limit_locks (key, type, reason, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			reason = excluded.reason,
			expires_at = excluded.expires_at,
			created_at = excluded.created_at
	`, key, lockType, reason, expiresAt, time.Now())
	return err
}

func (l *Limiter) Unlock(key string) error {
	_, err := l.db.Exec("DELETE FROM rate_limit_locks WHERE key = ?", key)
	return err
}

func (l *Limiter) GetAllLocks() ([]models.RateLimitLock, error) {
	rows, err := l.db.Query("SELECT key, type, reason, expires_at, created_at FROM rate_limit_locks WHERE expires_at > ?", time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locks []models.RateLimitLock
	for rows.Next() {
		var lock models.RateLimitLock
		if err := rows.Scan(&lock.Key, &lock.Type, &lock.Reason, &lock.ExpiresAt, &lock.CreatedAt); err != nil {
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func (l *Limiter) RecordAttempt(key string, window time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	times := l.attempts[key]
	valid := make([]time.Time, 0, len(times)+1)
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	valid = append(valid, now)
	l.attempts[key] = valid

	return len(valid)
}

// Admin Login Rate Limiter Checks
func (l *Limiter) CheckAdminLogin(username, ip string) error {
	userKey := fmt.Sprintf("admin_login_user:%s", username)
	ipUserKey := fmt.Sprintf("admin_login_ipuser:%s:%s", username, ip)

	// Check DB locks
	if locked, reason, expires, _ := l.IsLocked(userKey); locked {
		return fmt.Errorf("учетная запись заблокирована до %s (%s)", expires.Format("15:04:05"), reason)
	}
	if locked, reason, expires, _ := l.IsLocked(ipUserKey); locked {
		return fmt.Errorf("попытки входа с вашего IP заблокированы до %s (%s)", expires.Format("15:04:05"), reason)
	}
	return nil
}

func (l *Limiter) RecordAdminFailedPassword(username, ip string) error {
	userKey := fmt.Sprintf("admin_login_user:%s", username)
	ipUserKey := fmt.Sprintf("admin_login_ipuser:%s:%s", username, ip)

	countIPUser := l.RecordAttempt("fail_pass_ipuser:"+ipUserKey, 15*time.Minute)
	if countIPUser >= 5 {
		l.Lock(ipUserKey, "admin_login", "слишком много неверных паролей", 15*time.Minute)
	}

	countUser := l.RecordAttempt("fail_pass_user:"+userKey, 1*time.Hour)
	if countUser >= 20 {
		l.Lock(userKey, "admin_login", "массовый перебор паролей", 1*time.Hour)
	}
	return nil
}

func (l *Limiter) RecordAdminFailedTOTP(username, ip string) error {
	ipUserKey := fmt.Sprintf("admin_login_ipuser:%s:%s", username, ip)

	countIPUser := l.RecordAttempt("fail_totp_ipuser:"+ipUserKey, 15*time.Minute)
	if countIPUser >= 5 {
		l.Lock(ipUserKey, "admin_totp", "слишком много неверных кодов TOTP", 15*time.Minute)
	}
	return nil
}

// Invite Activation Rate Limiter
func (l *Limiter) CheckInviteActivation(ip string) error {
	ip1hKey := fmt.Sprintf("invite_ip_1h:%s", ip)
	ip24hKey := fmt.Sprintf("invite_ip_24h:%s", ip)

	if locked, reason, expires, _ := l.IsLocked(ip1hKey); locked {
		return fmt.Errorf("активация инвайтов с вашего IP заблокирована до %s (%s)", expires.Format("15:04:05"), reason)
	}
	if locked, reason, expires, _ := l.IsLocked(ip24hKey); locked {
		return fmt.Errorf("активация инвайтов заблокирована до %s (%s)", expires.Format("15:04:05"), reason)
	}
	return nil
}

func (l *Limiter) RecordInviteFailed(ip string) error {
	ip1hKey := fmt.Sprintf("invite_ip_1h:%s", ip)
	ip24hKey := fmt.Sprintf("invite_ip_24h:%s", ip)

	c15m := l.RecordAttempt("fail_invite_15m:"+ip, 15*time.Minute)
	if c15m >= 5 {
		l.Lock(ip1hKey, "invite", "превышен лимит попыток инвайта (5/15 мин)", 1*time.Hour)
	}

	c1h := l.RecordAttempt("fail_invite_1h:"+ip, 1*time.Hour)
	if c1h >= 30 {
		l.Lock(ip24hKey, "invite", "превышен лимит попыток инвайта (30/1 час)", 24*time.Hour)
	}
	return nil
}

// Generic Request Limit Check
func (l *Limiter) CheckRequestLimit(key string, isLocal bool, extLimit int, localLimit int, window time.Duration, msg string) error {
	limit := extLimit
	if isLocal {
		limit = localLimit
	}
	count := l.RecordAttempt(key, window)
	if count > limit {
		return fmt.Errorf("превышен лимит запросов: %s (макс %d)", msg, limit)
	}
	return nil
}
