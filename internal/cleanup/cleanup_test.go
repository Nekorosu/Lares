package cleanup

import (
	"testing"
	"time"
)

func TestReservationTTL(t *testing.T) {
	for _, tt := range []struct {
		size  int64
		speed float64
		want  time.Duration
	}{{0, 0, time.Hour}, {1 << 20, 0, time.Hour}, {10 << 30, 2 << 20, 10240 * time.Second}, {1 << 60, 1, 72 * time.Hour}} {
		if got := ReservationTTL(tt.size, tt.speed); got != tt.want {
			t.Errorf("TTL(%d,%f)=%s want %s", tt.size, tt.speed, got, tt.want)
		}
	}
}
