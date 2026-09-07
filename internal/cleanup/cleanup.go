package cleanup

import (
	"math"
	"time"
)

func ReservationTTL(bytes int64, speed float64) time.Duration {
	if speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		speed = 2 * 1024 * 1024
	}
	seconds := 2 * float64(max(0, bytes)) / speed
	if seconds < 3600 {
		seconds = 3600
	}
	if seconds > 72*3600 {
		seconds = 72 * 3600
	}
	return time.Duration(seconds * float64(time.Second))
}
