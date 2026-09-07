package ratelimit

import (
	"context"
	"lares/internal/db"
	"path/filepath"
	"testing"
	"time"
)

func TestSlidingWindowsPersistAndReset(t *testing.T) {
	d, e := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	r := NewRateLimiter(d)
	rule := Rule{"create:1", 2, time.Hour}
	for i := 0; i < 2; i++ {
		ok, _, e := r.Allow(context.Background(), rule)
		if !ok || e != nil {
			t.Fatal(ok, e)
		}
	}
	r = NewRateLimiter(d)
	ok, wait, e := r.Allow(context.Background(), rule)
	if ok || wait < 59*time.Minute || e != nil {
		t.Fatal(ok, wait, e)
	}
	if e = r.Lock(rule.Key, "test", "test", wait); e != nil {
		t.Fatal(e)
	}
	if locked, _, _ := r.IsLocked(rule.Key); !locked {
		t.Fatal("not locked")
	}
	if e = r.Unlock(rule.Key); e != nil {
		t.Fatal(e)
	}
	ok, _, e = r.Allow(context.Background(), rule)
	if !ok || e != nil {
		t.Fatal("reset failed", e)
	}
}
