package speedlimit

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestRuntimeUpdatesKeepSharedLimiters(t *testing.T) {
	s := NewSpeedLimiter(250, 250, 16)
	a := s.NewReader(context.Background(), bytes.NewReader([]byte("test")), true, true).(*reader)
	b := s.NewReader(context.Background(), bytes.NewReader(nil), true, true).(*reader)
	if a.l != b.l || float64(a.l.Limit()) != 31250000 {
		t.Fatal("not shared or wrong Mbps")
	}
	s.UpdateLimits(10, 20, 1)
	if a.l != s.up || b.l != s.up || float64(a.l.Limit()) != 1250000 || a.l.Burst() != 1<<20 {
		t.Fatal("update replaced limiter")
	}
	data, e := io.ReadAll(a)
	if e != nil || string(data) != "test" {
		t.Fatal(e)
	}
	s.Tick()
	up, _ := s.GetStats()
	if up != 4 {
		t.Fatal(up)
	}
	local := bytes.NewReader(nil)
	if s.NewReader(context.Background(), local, false, true) != local {
		t.Fatal("local throttled")
	}
}
