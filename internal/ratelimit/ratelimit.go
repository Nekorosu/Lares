package ratelimit

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

type RateLimiter struct{ db *sql.DB }

func NewRateLimiter(db *sql.DB) *RateLimiter { return &RateLimiter{db} }

type Rule struct {
	Key    string
	Count  int
	Window time.Duration
}

// Sliding windows persisted in SQLite; all requested keys are checked and recorded atomically.
func (r *RateLimiter) Allow(ctx context.Context, rules ...Rule) (bool, time.Duration, error) {
	ok, wait, _, err := r.AllowDetailed(ctx, rules...)
	return ok, wait, err
}
func (r *RateLimiter) AllowDetailed(ctx context.Context, rules ...Rule) (bool, time.Duration, string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	for _, v := range rules {
		var count int
		var first time.Time
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM request_events WHERE key=? AND time>?", v.Key, now.Add(-v.Window)).Scan(&count); err != nil {
			return false, 0, "", err
		}
		if count >= v.Count {
			wait := v.Window
			if err = tx.QueryRowContext(ctx, "SELECT time FROM request_events WHERE key=? AND time>? ORDER BY time LIMIT 1", v.Key, now.Add(-v.Window)).Scan(&first); err == nil {
				wait = time.Until(first.Add(v.Window))
			} else {
				return false, 0, "", err
			}
			return false, wait, v.Key, nil
		}
	}
	for _, v := range rules {
		if _, err = tx.ExecContext(ctx, "INSERT INTO request_events(key,time) VALUES(?,?)", v.Key, now); err != nil {
			return false, 0, "", err
		}
	}
	return true, 0, "", tx.Commit()
}
func (r *RateLimiter) IsLocked(key string) (bool, time.Duration, string) {
	var t time.Time
	var reason string
	err := r.db.QueryRow("SELECT expires_at,reason FROM rate_limit_locks WHERE key=? AND expires_at>?", key, time.Now().UTC()).Scan(&t, &reason)
	if err == sql.ErrNoRows {
		return false, 0, ""
	}
	if err != nil {
		return true, time.Minute, "ошибка проверки блокировки"
	}
	return true, time.Until(t), reason
}
func (r *RateLimiter) Lock(key, typ, reason string, d time.Duration) error {
	_, e := r.db.Exec("INSERT INTO rate_limit_locks(key,type,reason,expires_at,created_at) VALUES(?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET expires_at=excluded.expires_at,reason=excluded.reason", key, typ, reason, time.Now().UTC().Add(d), time.Now().UTC())
	return e
}
func (r *RateLimiter) Unlock(key string) error {
	tx, e := r.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM rate_limit_locks WHERE key=?", key); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM request_events WHERE key=? OR substr(key,1,?)=?", key, len(key)+1, key+":"); e != nil {
		return e
	}
	return tx.Commit()
}
func (r *RateLimiter) ClearAllLocks() error {
	tx, e := r.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM rate_limit_locks"); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM request_events"); e != nil {
		return e
	}
	return tx.Commit()
}
func SetRetryAfterHeader(w http.ResponseWriter, seconds int) {
	w.Header().Set("Retry-After", fmt.Sprint(max(1, seconds)))
	w.WriteHeader(429)
}
