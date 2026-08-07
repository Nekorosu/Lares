package ratelimit

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	db       *sql.DB
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	lastSeen map[string]time.Time
}

func NewRateLimiter(db *sql.DB) *RateLimiter {
	rl := &RateLimiter{
		db:       db,
		limiters: make(map[string]*rate.Limiter),
		lastSeen: make(map[string]time.Time),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		for key, t := range rl.lastSeen {
			if time.Since(t) > 30*time.Minute {
				delete(rl.limiters, key)
				delete(rl.lastSeen, key)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) AllowTokenBucket(key string, limitPerMin int, burst int) bool {
	rl.mu.Lock()
	limiter, exists := rl.limiters[key]
	if !exists {
		r := rate.Every(time.Minute / time.Duration(limitPerMin))
		limiter = rate.NewLimiter(r, burst)
		rl.limiters[key] = limiter
	}
	rl.lastSeen[key] = time.Now()
	rl.mu.Unlock()

	return limiter.Allow()
}

func (rl *RateLimiter) IsLocked(key string) (bool, time.Duration, string) {
	if rl.db == nil {
		return false, 0, ""
	}

	var expiresAt time.Time
	var reason string
	err := rl.db.QueryRow("SELECT expires_at, reason FROM rate_limit_locks WHERE key = ? AND expires_at > ?", key, time.Now().UTC()).Scan(&expiresAt, &reason)
	if err == nil {
		remaining := time.Until(expiresAt)
		return true, remaining, reason
	}
	return false, 0, ""
}

func (rl *RateLimiter) Lock(key, lockType, reason string, duration time.Duration) error {
	if rl.db == nil {
		return nil
	}

	expiresAt := time.Now().UTC().Add(duration)
	createdAt := time.Now().UTC()

	query := `
		INSERT INTO rate_limit_locks (key, type, reason, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			expires_at = excluded.expires_at,
			reason = excluded.reason,
			created_at = excluded.created_at
	`
	_, err := rl.db.Exec(query, key, lockType, reason, expiresAt, createdAt)
	return err
}

func (rl *RateLimiter) Unlock(key string) error {
	if rl.db == nil {
		return nil
	}
	_, err := rl.db.Exec("DELETE FROM rate_limit_locks WHERE key = ?", key)
	return err
}

func (rl *RateLimiter) ClearAllLocks() error {
	if rl.db == nil {
		return nil
	}
	_, err := rl.db.Exec("DELETE FROM rate_limit_locks")
	return err
}

func SetRetryAfterHeader(w http.ResponseWriter, seconds int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	w.WriteHeader(http.StatusTooManyRequests)
}
