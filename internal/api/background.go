package api

import (
	"context"
	"encoding/json"
	"fmt"
	"lares/internal/models"
	"lares/internal/traffic"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func monthStart(t time.Time) time.Time {
	t = t.Local()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}
func (s *Server) logError(e error) { log.Printf("Lares: %v", e) }
func (s *Server) background() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var ticks int
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.speedLimit.Tick()
			ticks++
			if ticks%60 == 0 {
				if e := s.cleanup(); e != nil {
					s.logError(e)
				}
				if e := s.backup(); e != nil {
					s.logError(e)
				}
			}
		}
	}
}
func (s *Server) recover() error {
	rows, e := s.query(s.ctx, "SELECT id FROM uploads WHERE status IN ('reserved','uploading')")
	if e != nil {
		return e
	}
	for _, row := range rows {
		id := fmt.Sprint(row["id"])
		u, e := s.upload(s.ctx, id)
		if e != nil {
			return e
		}
		if u.FinalID != "" {
			f, err := s.sm.Open(s.sm.GetShardedPath(u.FinalID))
			if err == nil {
				f.Close()
				var record models.FileRecord
				if e = json.Unmarshal([]byte(u.Record), &record); e != nil {
					return e
				}
				p, err := s.person(s.ctx, u.PersonID)
				if err != nil {
					return err
				}
				if !p.Enabled {
					e = s.cancelUpload(u, "canceled")
				} else {
					e = s.commitFile(u, record)
				}
				if e != nil {
					return e
				}
				continue
			}
		}
		f, e := s.sm.Open(s.sm.GetPartPath(id))
		if os.IsNotExist(e) {
			if e = s.cancelUpload(u, "failed"); e != nil {
				return e
			}
			continue
		}
		if e != nil {
			return e
		}
		st, e := f.Stat()
		f.Close()
		if e != nil {
			return e
		}
		if st.Size() > u.Size || st.Size() < u.Received {
			if e = s.cancelUpload(u, "failed"); e != nil {
				return e
			}
			continue
		}
		if st.Size() > u.Received {
			if e = s.recordChunk(s.ctx, u, st.Size()-u.Received, u.ExternalReserved, 0); e != nil {
				return e
			}
		}
	}
	// Persisted download checkpoints represent aborted transfers after a process restart.
	tx, e := s.db.BeginTx(s.ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rs, e := tx.QueryContext(s.ctx, "SELECT person_id,month,bytes FROM transfer_pending")
	if e != nil {
		return e
	}
	type count struct {
		pid, n int64
		month  string
	}
	var counts []count
	for rs.Next() {
		var c count
		if e = rs.Scan(&c.pid, &c.month, &c.n); e != nil {
			rs.Close()
			return e
		}
		counts = append(counts, c)
	}
	e = rs.Err()
	rs.Close()
	if e != nil {
		return e
	}
	for _, c := range counts {
		if e = traffic.Record(s.ctx, tx, c.pid, c.month, "download_aborted_bytes", c.n); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(s.ctx, "DELETE FROM transfer_pending"); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	pending, e := s.query(s.ctx, "SELECT id,stored_path FROM file_deletions")
	if e != nil {
		return e
	}
	for _, row := range pending {
		if e = s.deleteStored(s.ctx, fmt.Sprint(row["id"])); e != nil {
			return e
		}
	}
	known := map[string]bool{}
	rs2, e := s.query(s.ctx, "SELECT stored_path FROM files")
	if e != nil {
		return e
	}
	for _, r := range rs2 {
		p := s.sm.ResolvePath(fmt.Sprint(r["stored_path"]))
		if p == "" {
			return fmt.Errorf("небезопасный stored_path в БД")
		}
		known[p] = true
	}
	ups, e := s.query(s.ctx, "SELECT id FROM uploads WHERE status IN ('reserved','uploading')")
	if e != nil {
		return e
	}
	for _, r := range ups {
		known[s.sm.GetPartPath(fmt.Sprint(r["id"]))] = true
	}
	if e = s.sm.Walk(func(p string, st os.FileInfo) error {
		if !known[p] {
			return s.sm.DeleteFile(p)
		}
		return nil
	}); e != nil {
		return e
	}
	return s.cleanup()
}
func (s *Server) cleanup() error {
	now := time.Now().UTC()
	critical := s.sm.CheckDiskSpaceCritical() != nil
	uploads, e := s.query(s.ctx, "SELECT id FROM uploads WHERE status IN ('reserved','uploading') AND (reservation_expires_at<=? OR ?)", now, critical)
	if e != nil {
		return e
	}
	for _, row := range uploads {
		id := fmt.Sprint(row["id"])
		s.stopActiveUpload(id)
		l := s.lockUpload(id)
		if !l.TryLock() {
			continue
		}
		u, err := s.upload(s.ctx, id)
		if err == nil && (critical || !u.Until.After(now)) {
			status := "expired"
			if critical {
				status = "failed"
			}
			err = s.cancelUpload(u, status)
			if err == nil {
				s.systemAudit("upload_"+status, "upload", id, "Резерв освобождён фоновой очисткой")
			}
		}
		l.Unlock()
		if err != nil {
			return err
		}
	}
	files, e := s.query(s.ctx, "SELECT id FROM files WHERE keep_forever=0 AND expires_at IS NOT NULL AND expires_at<=?", now)
	if e != nil {
		return e
	}
	for _, f := range files {
		if e = s.deleteStored(s.ctx, fmt.Sprint(f["id"])); e != nil {
			return e
		}
		s.systemAudit("file_expired", "file", fmt.Sprint(f["id"]), "Файл удалён по сроку хранения")
	}
	tx, e := s.db.BeginTx(s.ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	queries := []struct {
		q    string
		args []any
	}{
		{"DELETE FROM invite_codes WHERE expires_at<=?", []any{now}},
		{"UPDATE uploads SET session_id=NULL WHERE session_id IN (SELECT id FROM device_sessions WHERE revoked=1 OR idle_expires_at<=? OR (absolute_expires_at IS NOT NULL AND absolute_expires_at<=?))", []any{now, now}},
		{"DELETE FROM device_sessions WHERE revoked=1 OR idle_expires_at<=? OR (absolute_expires_at IS NOT NULL AND absolute_expires_at<=?)", []any{now, now}},
		{"DELETE FROM rate_limit_locks WHERE expires_at<=?", []any{now}},
		{"DELETE FROM request_events WHERE time<?", []any{now.Add(-24 * time.Hour)}},
		{"DELETE FROM audit_logs WHERE time<?", []any{now.AddDate(0, 0, -180)}},
		{"DELETE FROM traffic_counters WHERE month<?", []any{monthStart(time.Now()).AddDate(0, -11, 0).Format("2006-01")}},
	}
	for _, q := range queries {
		if _, e = tx.ExecContext(s.ctx, q.q, q.args...); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return s.securityLog.Rotate()
}
func (s *Server) backup() error {
	if s.cfg.BackupDir == "" {
		return nil
	}
	if e := os.MkdirAll(s.cfg.BackupDir, 0750); e != nil {
		return e
	}
	name := "lares-" + time.Now().Local().Format("2006-01-02") + ".db"
	dest := filepath.Join(s.cfg.BackupDir, name)
	if _, e := os.Stat(dest); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	temp := dest + ".tmp"
	if e := os.Remove(temp); e != nil && !os.IsNotExist(e) {
		return e
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	if _, e := s.db.ExecContext(ctx, "VACUUM INTO ?", temp); e != nil {
		return e
	}
	if e := os.Chmod(temp, 0640); e != nil {
		return e
	}
	if e := os.Rename(temp, dest); e != nil {
		return e
	}
	entries, e := os.ReadDir(s.cfg.BackupDir)
	if e != nil {
		return e
	}
	var backups []string
	for _, v := range entries {
		if !v.IsDir() && strings.HasPrefix(v.Name(), "lares-") && strings.HasSuffix(v.Name(), ".db") {
			backups = append(backups, v.Name())
		}
	}
	sort.Strings(backups)
	for len(backups) > 14 {
		if e = os.Remove(filepath.Join(s.cfg.BackupDir, backups[0])); e != nil {
			return e
		}
		backups = backups[1:]
	}
	return nil
}
