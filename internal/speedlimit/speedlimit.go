package speedlimit

import (
	"context"
	"golang.org/x/time/rate"
	"io"
	"sync/atomic"
	"time"
)

type SpeedLimiter struct {
	up, down           *rate.Limiter
	upBytes, downBytes atomic.Int64
	upRate, downRate   atomic.Int64
}

func NewSpeedLimiter(up, down, burst int) *SpeedLimiter {
	s := &SpeedLimiter{up: rate.NewLimiter(rate.Limit(up*1000000/8), burst<<20), down: rate.NewLimiter(rate.Limit(down*1000000/8), burst<<20)}
	return s
}
func (s *SpeedLimiter) UpdateLimits(up, down, burst int) {
	now := time.Now()
	s.up.SetLimitAt(now, rate.Limit(up*1000000/8))
	s.down.SetLimitAt(now, rate.Limit(down*1000000/8))
	s.up.SetBurstAt(now, burst<<20)
	s.down.SetBurstAt(now, burst<<20)
}
func (s *SpeedLimiter) Tick() {
	s.upRate.Store(s.upBytes.Swap(0))
	s.downRate.Store(s.downBytes.Swap(0))
}
func (s *SpeedLimiter) GetStats() (int64, int64) { return s.upRate.Load(), s.downRate.Load() }

type reader struct {
	ctx context.Context
	r   io.Reader
	l   *rate.Limiter
	n   *atomic.Int64
}

func (r *reader) Read(p []byte) (int, error) {
	if len(p) > 32768 {
		p = p[:32768]
	}
	n, e := r.r.Read(p)
	r.n.Add(int64(n))
	if n > 0 {
		if err := r.l.WaitN(r.ctx, n); err != nil {
			return n, err
		}
	}
	return n, e
}

type writer struct {
	ctx context.Context
	w   io.Writer
	l   *rate.Limiter
	n   *atomic.Int64
}

func (w *writer) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := min(len(p), 32768)
		if err := w.l.WaitN(w.ctx, n); err != nil {
			return total, err
		}
		k, e := w.w.Write(p[:n])
		w.n.Add(int64(k))
		total += k
		if e != nil {
			return total, e
		}
		if k != n {
			return total, io.ErrShortWrite
		}
		p = p[n:]
	}
	return total, nil
}
func (s *SpeedLimiter) NewReader(ctx context.Context, r io.Reader, ext, upload bool) io.Reader {
	if !ext {
		return r
	}
	l, n := s.up, &s.upBytes
	if !upload {
		l, n = s.down, &s.downBytes
	}
	return &reader{ctx, r, l, n}
}
func (s *SpeedLimiter) NewWriter(ctx context.Context, w io.Writer, ext, upload bool) io.Writer {
	if !ext {
		return w
	}
	l, n := s.down, &s.downBytes
	if upload {
		l, n = s.up, &s.upBytes
	}
	return &writer{ctx, w, l, n}
}
