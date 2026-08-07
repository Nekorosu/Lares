package cleanup

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lares/internal/storage"
	"lares/internal/traffic"
)

type Worker struct {
	db             *sql.DB
	sm             *storage.StorageManager
	tm             *traffic.Manager
	backupDir      string
	securityLogPath string
}

func NewWorker(db *sql.DB, sm *storage.StorageManager, tm *traffic.Manager, backupDir, securityLogPath string) *Worker {
	return &Worker{
		db:             db,
		sm:             sm,
		tm:             tm,
		backupDir:      backupDir,
		securityLogPath: securityLogPath,
	}
}

func (w *Worker) StartBackgroundJobs() {
	// Initial cleanup on startup
	w.RunCleanup()

	// Every 60 seconds cleanup worker
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			w.RunCleanup()
		}
	}()

	// Daily backup worker
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		// Run first backup on startup after 1 minute delay
		time.Sleep(1 * time.Minute)
		w.RunBackup()
		for range ticker.C {
			w.RunBackup()
		}
	}()
}

func (w *Worker) RunCleanup() {
	now := time.Now().UTC()

	// 1. Delete expired files
	rows, err := w.db.Query(`
		SELECT id, stored_path, person_id, size
		FROM files
		WHERE keep_forever = 0 AND expires_at IS NOT NULL AND expires_at <= ?
	`, now)
	if err == nil {
		type expiredFile struct {
			id         string
			storedPath string
			personID   int64
			size       int64
		}
		var toDelete []expiredFile
		for rows.Next() {
			var f expiredFile
			if err := rows.Scan(&f.id, &f.storedPath, &f.personID, &f.size); err == nil {
				toDelete = append(toDelete, f)
			}
		}
		rows.Close()

		for _, f := range toDelete {
			_ = w.sm.DeleteFile(f.storedPath)
			_, _ = w.db.Exec("DELETE FROM files WHERE id = ?", f.id)
			log.Printf("[Cleanup] Deleted expired file %s (%d bytes)", f.id, f.size)
		}
	}

	// 2. Cancel expired uploads/reservations
	upRows, err := w.db.Query(`
		SELECT id, person_id, received_bytes
		FROM uploads
		WHERE status IN ('reserved', 'uploading') AND reservation_expires_at <= ?
	`, now)
	if err == nil {
		type expiredUpload struct {
			id            string
			personID      int64
			receivedBytes int64
		}
		var expiredUps []expiredUpload
		for upRows.Next() {
			var u expiredUpload
			if err := upRows.Scan(&u.id, &u.personID, &u.receivedBytes); err == nil {
				expiredUps = append(expiredUps, u)
			}
		}
		upRows.Close()

		for _, u := range expiredUps {
			w.sm.DeletePartFile(u.id)
			_, _ = w.db.Exec("UPDATE uploads SET status = 'expired' WHERE id = ?", u.id)
			if u.receivedBytes > 0 {
				_ = w.tm.RecordUploadAborted(u.personID, u.receivedBytes, false)
			}
			log.Printf("[Cleanup] Expired upload %s (person %d)", u.id, u.personID)
		}
	}

	// 3. Mark revoked/expired sessions
	_, _ = w.db.Exec(`
		UPDATE device_sessions
		SET revoked = 1
		WHERE revoked = 0 AND (
			idle_expires_at <= ? OR
			(absolute_expires_at IS NOT NULL AND absolute_expires_at <= ?)
		)
	`, now, now)

	// 4. Delete expired rate limit locks
	_, _ = w.db.Exec("DELETE FROM rate_limit_locks WHERE expires_at <= ?", now)

	// 5. Purge old audit logs (> 180 days)
	cutoffAudit := now.AddDate(0, 0, -180)
	_, _ = w.db.Exec("DELETE FROM audit_logs WHERE time < ?", cutoffAudit)

	// 6. Purge traffic history (> 12 months)
	cutoffTraffic := now.AddDate(-1, 0, 0).Format("2006-01")
	_, _ = w.db.Exec("DELETE FROM traffic_counters WHERE month < ?", cutoffTraffic)
}

func (w *Worker) RunBackup() {
	if w.backupDir == "" {
		return
	}

	if err := os.MkdirAll(w.backupDir, 0750); err != nil {
		log.Printf("[Backup Error] Failed to create backup dir: %v", err)
		return
	}

	timestamp := time.Now().Format("2006-01-02_150405")
	backupPath := filepath.Join(w.backupDir, fmt.Sprintf("homeshare_backup_%s.db", timestamp))

	// Execute VACUUM INTO for safe online SQLite backup
	query := fmt.Sprintf("VACUUM INTO '%s'", backupPath)
	if _, err := w.db.Exec(query); err != nil {
		log.Printf("[Backup Error] VACUUM INTO failed: %v", err)
		return
	}

	log.Printf("[Backup] Successfully created SQLite backup: %s", backupPath)

	// Rotate backups - keep 14 latest
	w.rotateBackups()
}

func (w *Worker) rotateBackups() {
	entries, err := os.ReadDir(w.backupDir)
	if err != nil {
		return
	}

	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "homeshare_backup_") && strings.HasSuffix(entry.Name(), ".db") {
			backups = append(backups, filepath.Join(w.backupDir, entry.Name()))
		}
	}

	if len(backups) <= 14 {
		return
	}

	sort.Strings(backups)
	for i := 0; i < len(backups)-14; i++ {
		_ = os.Remove(backups[i])
		log.Printf("[Backup] Rotated old backup file: %s", backups[i])
	}
}
