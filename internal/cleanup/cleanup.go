package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lares/internal/audit"
	"lares/internal/config"
	"lares/internal/models"
	"lares/internal/storage"
	"lares/internal/traffic"
)

type Worker struct {
	db      *sql.DB
	cfg     *config.Config
	storage *storage.Storage
	audit   *audit.Logger
	traffic *traffic.Tracker
}

func NewWorker(db *sql.DB, cfg *config.Config, st *storage.Storage, au *audit.Logger, tr *traffic.Tracker) *Worker {
	return &Worker{
		db:      db,
		cfg:     cfg,
		storage: st,
		audit:   au,
		traffic: tr,
	}
}

func (w *Worker) Start(ctx context.Context) {
	// Run initial cleanup at startup
	w.RunCleanup()
	w.RunDailyBackup()

	ticker := time.NewTicker(60 * time.Second)
	backupTicker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	defer backupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.RunCleanup()
		case <-backupTicker.C:
			w.RunDailyBackup()
		}
	}
}

func (w *Worker) RunCleanup() {
	now := time.Now()

	// 1. Delete expired files (not keep_forever)
	rows, err := w.db.Query(`
		SELECT id, person_id, original_name, stored_path
		FROM files
		WHERE keep_forever = 0 AND expires_at IS NOT NULL AND expires_at < ?
	`, now)

	if err == nil {
		var expiredFiles []models.FileRecord
		for rows.Next() {
			var f models.FileRecord
			rows.Scan(&f.ID, &f.PersonID, &f.OriginalName, &f.StoredPath)
			expiredFiles = append(expiredFiles, f)
		}
		rows.Close()

		for _, f := range expiredFiles {
			fullPath := w.storage.GetFullPath(f.StoredPath)
			os.Remove(fullPath)
			w.db.Exec("DELETE FROM files WHERE id = ?", f.ID)
			w.audit.Log("system", 0, "file_expired", "file", f.ID, "system", fmt.Sprintf("Файл %s автоматически удален по истечению срока", f.OriginalName))
		}
	}

	// 2. Delete expired uploads and partial files
	uRows, err := w.db.Query(`
		SELECT id, person_id, received_bytes FROM uploads
		WHERE status IN ('reserved', 'uploading') AND reservation_expires_at < ?
	`, now)

	if err == nil {
		type expUpload struct {
			id       string
			personID int64
			recBytes int64
		}
		var expUploads []expUpload
		for uRows.Next() {
			var eu expUpload
			uRows.Scan(&eu.id, &eu.personID, &eu.recBytes)
			expUploads = append(expUploads, eu)
		}
		uRows.Close()

		for _, eu := range expUploads {
			partialPath := w.storage.GetPartialPath(eu.id)
			os.Remove(partialPath)
			w.db.Exec("UPDATE uploads SET status = ? WHERE id = ?", models.UploadStatusExpired, eu.id)
			if eu.recBytes > 0 {
				w.traffic.AddUploadAborted(eu.personID, eu.recBytes)
			}
			w.audit.Log("system", 0, "upload_expired", "upload", eu.id, "system", "Резервирование загрузки истекло")
		}
	}

	// 3. Clean abandoned .part files in tmp_dir that are not in DB
	entries, err := os.ReadDir(w.cfg.Paths.TmpDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".part") {
				info, err := entry.Info()
				if err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
					os.Remove(filepath.Join(w.cfg.Paths.TmpDir, entry.Name()))
				}
			}
		}
	}

	// 4. Revoke expired sessions
	w.db.Exec(`
		UPDATE device_sessions SET revoked = 1
		WHERE revoked = 0 AND (
			idle_expires_at < ? OR
			(absolute_expires_at IS NOT NULL AND absolute_expires_at < ?)
		)
	`, now, now)

	// 5. Disable expired invites
	w.db.Exec("UPDATE invite_codes SET enabled = 0 WHERE enabled = 1 AND expires_at < ?", now)

	// 6. Purge audit logs > 180 days
	w.audit.PurgeOldLogs(180)

	// 7. Purge traffic history > 12 months
	w.traffic.PurgeOldHistory(12)
}

func (w *Worker) RunDailyBackup() {
	if w.cfg.Paths.BackupDir == "" {
		return
	}
	if err := os.MkdirAll(w.cfg.Paths.BackupDir, 0750); err != nil {
		log.Printf("[BACKUP ERROR] failed to create backup dir: %v", err)
		return
	}

	backupName := fmt.Sprintf("lares_backup_%s.db", time.Now().Format("20060102_150405"))
	backupPath := filepath.Join(w.cfg.Paths.BackupDir, backupName)

	// Safe VACUUM INTO for online SQLite backup
	query := fmt.Sprintf("VACUUM INTO '%s'", backupPath)
	if _, err := w.db.Exec(query); err != nil {
		log.Printf("[BACKUP ERROR] VACUUM INTO failed: %v", err)
		return
	}

	// Keep last 14 backups
	entries, err := os.ReadDir(w.cfg.Paths.BackupDir)
	if err != nil {
		return
	}

	var backupFiles []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "lares_backup_") {
			backupFiles = append(backupFiles, entry)
		}
	}

	if len(backupFiles) > 14 {
		sort.Slice(backupFiles, func(i, j int) bool {
			infoI, _ := backupFiles[i].Info()
			infoJ, _ := backupFiles[j].Info()
			return infoI.ModTime().Before(infoJ.ModTime())
		})

		toDelete := len(backupFiles) - 14
		for i := 0; i < toDelete; i++ {
			os.Remove(filepath.Join(w.cfg.Paths.BackupDir, backupFiles[i].Name()))
		}
	}
}
