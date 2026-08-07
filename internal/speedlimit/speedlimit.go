package speedlimit

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type SpeedLimiter struct {
	mu sync.Mutex

	uploadMbps   int
	downloadMbps int
	burstMB      int

	uploadLimiter   *rate.Limiter
	downloadLimiter *rate.Limiter

	uploadBytesWindow   atomic.Int64
	downloadBytesWindow atomic.Int64
	currentUploadBps    atomic.Int64
	currentDownloadBps  atomic.Int64
}

func NewSpeedLimiter(uploadMbps, downloadMbps, burstMB int) *SpeedLimiter {
	sl := &SpeedLimiter{
		uploadMbps:   uploadMbps,
		downloadMbps: downloadMbps,
		burstMB:      burstMB,
	}
	sl.rebuildLimiters()
	go sl.statsLoop()
	return sl
}

func (sl *SpeedLimiter) rebuildLimiters() {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	upBytesPerSec := rate.Limit(int64(sl.uploadMbps) * 1_000_000 / 8)
	downBytesPerSec := rate.Limit(int64(sl.downloadMbps) * 1_000_000 / 8)
	burstBytes := sl.burstMB * 1024 * 1024

	sl.uploadLimiter = rate.NewLimiter(upBytesPerSec, burstBytes)
	sl.downloadLimiter = rate.NewLimiter(downBytesPerSec, burstBytes)
}

func (sl *SpeedLimiter) UpdateLimits(uploadMbps, downloadMbps, burstMB int) {
	if uploadMbps < 10 {
		uploadMbps = 10
	} else if uploadMbps > 1000 {
		uploadMbps = 1000
	}
	if downloadMbps < 10 {
		downloadMbps = 10
	} else if downloadMbps > 1000 {
		downloadMbps = 1000
	}
	if burstMB < 1 {
		burstMB = 1
	} else if burstMB > 128 {
		burstMB = 128
	}

	sl.uploadMbps = uploadMbps
	sl.downloadMbps = downloadMbps
	sl.burstMB = burstMB
	sl.rebuildLimiters()
}

func (sl *SpeedLimiter) statsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		up := sl.uploadBytesWindow.Swap(0)
		down := sl.downloadBytesWindow.Swap(0)
		sl.currentUploadBps.Store(up)
		sl.currentDownloadBps.Store(down)
	}
}

func (sl *SpeedLimiter) GetStats() (uploadBps, downloadBps int64) {
	return sl.currentUploadBps.Load(), sl.currentDownloadBps.Load()
}

type LimitedReader struct {
	r          io.Reader
	limiter    *rate.Limiter
	isExternal bool
	stat       *atomic.Int64
	ctx        context.Context
}

func (sl *SpeedLimiter) NewReader(ctx context.Context, r io.Reader, isExternal bool, isUpload bool) io.Reader {
	if !isExternal {
		return r
	}
	sl.mu.Lock()
	lim := sl.uploadLimiter
	if !isUpload {
		lim = sl.downloadLimiter
	}
	sl.mu.Unlock()

	var stat *atomic.Int64
	if isUpload {
		stat = &sl.uploadBytesWindow
	} else {
		stat = &sl.downloadBytesWindow
	}

	return &LimitedReader{
		r:          r,
		limiter:    lim,
		isExternal: true,
		stat:       stat,
		ctx:        ctx,
	}
}

func (lr *LimitedReader) Read(p []byte) (n int, err error) {
	n, err = lr.r.Read(p)
	if n > 0 && lr.isExternal && lr.limiter != nil {
		if lr.stat != nil {
			lr.stat.Add(int64(n))
		}
		// Wait for token in chunks if n > burst
		chunkSize := 32 * 1024
		for i := 0; i < n; i += chunkSize {
			end := i + chunkSize
			if end > n {
				end = n
			}
			sz := end - i
			if errWait := lr.limiter.WaitN(lr.ctx, sz); errWait != nil {
				return n, errWait
			}
		}
	}
	return n, err
}

type LimitedWriter struct {
	w          io.Writer
	limiter    *rate.Limiter
	isExternal bool
	stat       *atomic.Int64
	ctx        context.Context
}

func (sl *SpeedLimiter) NewWriter(ctx context.Context, w io.Writer, isExternal bool, isUpload bool) io.Writer {
	if !isExternal {
		return w
	}
	sl.mu.Lock()
	lim := sl.uploadLimiter
	if !isUpload {
		lim = sl.downloadLimiter
	}
	sl.mu.Unlock()

	var stat *atomic.Int64
	if isUpload {
		stat = &sl.uploadBytesWindow
	} else {
		stat = &sl.downloadBytesWindow
	}

	return &LimitedWriter{
		w:          w,
		limiter:    lim,
		isExternal: true,
		stat:       stat,
		ctx:        ctx,
	}
}

func (lw *LimitedWriter) Write(p []byte) (n int, err error) {
	if lw.isExternal && lw.limiter != nil {
		chunkSize := 32 * 1024
		for i := 0; i < len(p); i += chunkSize {
			end := i + chunkSize
			if end > len(p) {
				end = len(p)
			}
			chunk := p[i:end]
			if errWait := lw.limiter.WaitN(lw.ctx, len(chunk)); errWait != nil {
				return n, errWait
			}
			written, errWrite := lw.w.Write(chunk)
			n += written
			if lw.stat != nil {
				lw.stat.Add(int64(written))
			}
			if errWrite != nil {
				return n, errWrite
			}
		}
		return n, nil
	}

	return lw.w.Write(p)
}
