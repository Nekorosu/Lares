package speedlimit

import (
	"context"
	"io"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type SpeedManager struct {
	mu              sync.RWMutex
	uploadLimiter   *rate.Limiter
	downloadLimiter *rate.Limiter
	uploadMbps      int
	downloadMbps    int
	burstMB         int
}

func NewSpeedManager(uploadMbps, downloadMbps, burstMB int) *SpeedManager {
	sm := &SpeedManager{}
	sm.UpdateLimits(uploadMbps, downloadMbps, burstMB)
	return sm
}

func (sm *SpeedManager) UpdateLimits(uploadMbps, downloadMbps, burstMB int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if uploadMbps < 10 {
		uploadMbps = 10
	}
	if uploadMbps > 1000 {
		uploadMbps = 1000
	}
	if downloadMbps < 10 {
		downloadMbps = 10
	}
	if downloadMbps > 1000 {
		downloadMbps = 1000
	}
	if burstMB < 1 {
		burstMB = 1
	}
	if burstMB > 128 {
		burstMB = 128
	}

	sm.uploadMbps = uploadMbps
	sm.downloadMbps = downloadMbps
	sm.burstMB = burstMB

	// Mbps -> Bytes per second
	uploadBytesPerSec := rate.Limit(uploadMbps * 125000)
	downloadBytesPerSec := rate.Limit(downloadMbps * 125000)
	burstBytes := burstMB * 1024 * 1024

	sm.uploadLimiter = rate.NewLimiter(uploadBytesPerSec, burstBytes)
	sm.downloadLimiter = rate.NewLimiter(downloadBytesPerSec, burstBytes)
}

func (sm *SpeedManager) GetUploadLimiter() *rate.Limiter {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.uploadLimiter
}

func (sm *SpeedManager) GetDownloadLimiter() *rate.Limiter {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.downloadLimiter
}

// ThrottledReader limits read speed for external uploads
type ThrottledReader struct {
	reader  io.Reader
	limiter *rate.Limiter
	isLocal bool
	ctx     context.Context
}

func NewThrottledReader(ctx context.Context, r io.Reader, limiter *rate.Limiter, isLocal bool) io.Reader {
	if isLocal || limiter == nil {
		return r
	}
	return &ThrottledReader{
		reader:  r,
		limiter: limiter,
		isLocal: isLocal,
		ctx:     ctx,
	}
}

func (tr *ThrottledReader) Read(p []byte) (n int, err error) {
	n, err = tr.reader.Read(p)
	if n > 0 && !tr.isLocal {
		if tr.limiter.WaitN(tr.ctx, n) != nil {
			// Context canceled or timeout
			return n, io.ErrUnexpectedEOF
		}
	}
	return n, err
}

// ThrottledWriter limits write speed for external downloads
type ThrottledWriter struct {
	writer  io.Writer
	limiter *rate.Limiter
	isLocal bool
	ctx     context.Context
}

func NewThrottledWriter(ctx context.Context, w io.Writer, limiter *rate.Limiter, isLocal bool) io.Writer {
	if isLocal || limiter == nil {
		return w
	}
	return &ThrottledWriter{
		writer:  w,
		limiter: limiter,
		isLocal: isLocal,
		ctx:     ctx,
	}
}

func (tw *ThrottledWriter) Write(p []byte) (n int, err error) {
	chunkSize := 64 * 1024 // 64KB per token chunk
	totalWritten := 0

	for totalWritten < len(p) {
		end := totalWritten + chunkSize
		if end > len(p) {
			end = len(p)
		}
		chunk := p[totalWritten:end]

		if !tw.isLocal && tw.limiter != nil {
			if err := tw.limiter.WaitN(tw.ctx, len(chunk)); err != nil {
				return totalWritten, err
			}
		}

		nw, werr := tw.writer.Write(chunk)
		totalWritten += nw
		if werr != nil {
			return totalWritten, werr
		}
	}
	return totalWritten, nil
}

// SpeedTracker tracks bandwidth statistics for Dashboard
type SpeedTracker struct {
	mu            sync.Mutex
	uploadBytes   int64
	downloadBytes int64
	lastCheck     time.Time
	uploadBps     float64
	downloadBps   float64
}

func NewSpeedTracker() *SpeedTracker {
	return &SpeedTracker{lastCheck: time.Now()}
}

func (st *SpeedTracker) RecordUpload(bytes int64) {
	st.mu.Lock()
	st.uploadBytes += bytes
	st.mu.Unlock()
}

func (st *SpeedTracker) RecordDownload(bytes int64) {
	st.mu.Lock()
	st.downloadBytes += bytes
	st.mu.Unlock()
}

func (st *SpeedTracker) CalculateSpeeds() (uploadMbps float64, downloadMbps float64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(st.lastCheck).Seconds()
	if elapsed >= 1.0 {
		st.uploadBps = (float64(st.uploadBytes) * 8.0) / (elapsed * 1000000.0)
		st.downloadBps = (float64(st.downloadBytes) * 8.0) / (elapsed * 1000000.0)
		st.uploadBytes = 0
		st.downloadBytes = 0
		st.lastCheck = now
	}
	return st.uploadBps, st.downloadBps
}
