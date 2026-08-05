package download

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"homeshare/internal/models"
	"homeshare/internal/speedlimit"
	"homeshare/internal/storage"
	"homeshare/internal/traffic"
)

type Manager struct {
	db           *sql.DB
	storage      *storage.Storage
	traffic      *traffic.Tracker
	speedManager *speedlimit.SpeedManager
	speedTracker *speedlimit.SpeedTracker
}

func NewManager(db *sql.DB, st *storage.Storage, tr *traffic.Tracker, sm *speedlimit.SpeedManager, stTr *speedlimit.SpeedTracker) *Manager {
	return &Manager{
		db:           db,
		storage:      st,
		traffic:      tr,
		speedManager: sm,
		speedTracker: stTr,
	}
}

func (m *Manager) GetFileRecord(fileID string) (*models.FileRecord, error) {
	row := m.db.QueryRow(`
		SELECT id, person_id, original_name, stored_path, size, content_type, status,
		       flagged, flag_reason, protected, keep_forever, expires_at, created_at, client_ip_hash
		FROM files WHERE id = ?
	`, fileID)

	var f models.FileRecord
	err := row.Scan(&f.ID, &f.PersonID, &f.OriginalName, &f.StoredPath, &f.Size, &f.ContentType,
		&f.Status, &f.Flagged, &f.FlagReason, &f.Protected, &f.KeepForever, &f.ExpiresAt, &f.CreatedAt, &f.ClientIPHash)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func IsPreviewable(filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".webm", ".mp3", ".ogg", ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	switch contentType {
	case "video/mp4", "video/webm", "audio/mpeg", "audio/ogg", "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

func (m *Manager) ServeFileDownload(w http.ResponseWriter, r *http.Request, file *models.FileRecord, person *models.Person, isAdmin bool, isInline bool, isLocal bool) {
	// Check quarantine access
	if file.Status == models.FileStatusQuarantined {
		if !isAdmin && file.PersonID != person.ID {
			http.Error(w, "Доступ к файлу заблокирован (карантин)", http.StatusForbidden)
			return
		}
	}

	fullPath := m.storage.GetFullPath(file.StoredPath)
	f, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "Файл не найден на диске", http.StatusNotFound)
		return
	}
	defer f.Close()

	// Set Security Headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; media-src 'self'; img-src 'self'; style-src 'unsafe-inline'")

	// Inline preview check
	if isInline && IsPreviewable(file.OriginalName, file.ContentType) {
		w.Header().Set("Content-Type", file.ContentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", sanitizeHeaderFilename(file.OriginalName)))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sanitizeHeaderFilename(file.OriginalName)))
	}

	// Traffic check grace if external
	if !isLocal && person != nil && !person.IgnoreTrafficQuota {
		if err := m.traffic.CheckDownloadTrafficGrace(person, 0, file.Size, isLocal); err != nil {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
	}

	// Speed Limiter Wrapper
	var outWriter io.Writer = w
	if !isLocal && m.speedManager != nil {
		limiter := m.speedManager.GetDownloadLimiter()
		outWriter = speedlimit.NewThrottledWriter(r.Context(), w, limiter, isLocal)
	}

	// Serve content with http.ServeContent (handles HTTP Range automatically)
	cw := &countingWriter{Writer: outWriter, speedTracker: m.speedTracker}
	http.ServeContent(cw, r, file.OriginalName, file.CreatedAt, f)

	// Record completed or aborted download bytes
	if person != nil {
		if cw.bytesWritten >= file.Size {
			m.traffic.AddDownloadCompleted(person.ID, cw.bytesWritten)
		} else if cw.bytesWritten > 0 {
			m.traffic.AddDownloadAborted(person.ID, cw.bytesWritten)
		}
	}
}

type countingWriter struct {
	io.Writer
	bytesWritten int64
	speedTracker *speedlimit.SpeedTracker
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.Writer.Write(p)
	cw.bytesWritten += int64(n)
	if cw.speedTracker != nil {
		cw.speedTracker.RecordDownload(int64(n))
	}
	return n, err
}

func sanitizeHeaderFilename(name string) string {
	name = strings.ReplaceAll(name, "\"", "\\\"")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}
