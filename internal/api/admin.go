package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/skip2/go-qrcode"
	"html/template"
	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/traffic"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RuntimeSettings struct {
	Storage    config.StorageDefaults `json:"storage"`
	Speed      config.SpeedLimits     `json:"speed"`
	Zip        config.ZipLimits       `json:"zip"`
	Expiry     []int                  `json:"expiry"`
	Suspicious []string               `json:"suspicious"`
}

func (v RuntimeSettings) Apply(c *config.Config) {
	c.StorageDefaults = v.Storage
	c.SpeedLimits = v.Speed
	c.ZipLimits = v.Zip
	c.ExpiryOptions = v.Expiry
	c.SuspiciousExtensions = v.Suspicious
}
func number(r *http.Request, key string, min, max int64) (int64, error) {
	n, e := strconv.ParseInt(r.FormValue(key), 10, 64)
	if e != nil || n < min || n > max {
		return 0, fmt.Errorf("неверное значение: %s", key)
	}
	return n, nil
}
func gib(r *http.Request, key string) (int64, error) {
	v, e := strconv.ParseFloat(r.FormValue(key), 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1048576 {
		return 0, fmt.Errorf("неверная квота: %s", key)
	}
	return int64(v * (1 << 30)), nil
}
func checkbox(r *http.Request, key string) bool {
	return r.FormValue(key) == "on" || r.FormValue(key) == "1"
}
func (s *Server) adminAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	fileID := r.FormValue("file_id")
	var e error
	target := "/admin/dashboard"
	switch action {
	case "person_save":
		target = "/admin/people"
		e = s.savePerson(r, id)
	case "person_enable", "person_disable":
		target = "/admin/people"
		if id <= 0 {
			e = errors.New("неверный пользователь")
			break
		}
		enable := action == "person_enable"
		if !enable {
			e = s.disablePerson(r.Context(), id)
		} else {
			_, e = s.db.ExecContext(r.Context(), "UPDATE people SET enabled=1 WHERE id=? AND is_orphan=0", id)
		}
	case "person_delete":
		target = "/admin/people"
		if id <= 0 {
			e = errors.New("неверный пользователь")
			break
		}
		e = s.disablePerson(r.Context(), id)
		if e == nil {
			e = s.deletePerson(r, id, checkbox(r, "delete_files"))
		}
	case "invite_create":
		e = s.createInvite(w, r)
		if e == nil {
			return
		}
		target = "/admin/invites"
	case "invite_revoke":
		target = "/admin/invites"
		_, e = s.db.ExecContext(r.Context(), "UPDATE invite_codes SET enabled=0 WHERE id=?", id)
	case "session_revoke":
		target = "/admin/sessions"
		_, e = s.db.ExecContext(r.Context(), "UPDATE device_sessions SET revoked=1 WHERE id=?", id)
	case "session_revoke_all":
		target = "/admin/sessions"
		if id <= 0 {
			e = errors.New("неверный пользователь")
			break
		}
		_, e = s.db.ExecContext(r.Context(), "UPDATE device_sessions SET revoked=1 WHERE person_id=?", id)
	case "upload_cancel":
		target = "/admin/uploads"
		upID := r.FormValue("upload_id")
		s.stopActiveUpload(upID)
		l := s.lockUpload(upID)
		l.Lock()
		var u upload
		u, e = s.upload(r.Context(), upID)
		if e == nil {
			e = s.cancelUpload(u, "canceled")
		}
		l.Unlock()
	case "file_delete":
		target = "/admin/files"
		e = s.deleteStored(r.Context(), fileID)
	case "file_save":
		target = "/admin/files"
		e = s.saveFile(r, fileID)
	case "file_approve":
		target = "/admin/quarantine"
		_, e = s.db.ExecContext(r.Context(), "UPDATE files SET status='ready' WHERE id=?", fileID)
	case "file_unflag":
		target = "/admin/files"
		_, e = s.db.ExecContext(r.Context(), "UPDATE files SET flagged=0,flag_reason='' WHERE id=?", fileID)
	case "traffic_reset":
		target = "/admin/traffic"
		_, e = s.db.ExecContext(r.Context(), "DELETE FROM traffic_counters WHERE person_id=? AND month=?", id, traffic.GetCurrentMonth())
	case "settings_save":
		target = "/admin/settings"
		e = s.saveSettings(r)
	case "lock_clear":
		target = "/admin/settings"
		e = s.rateLimiter.Unlock(r.FormValue("key"))
	case "locks_clear":
		target = "/admin/settings"
		e = s.rateLimiter.ClearAllLocks()
	default:
		fail(w, r, 404, "Действие не найдено")
		return
	}
	if e != nil {
		fail(w, r, 400, e.Error())
		return
	}
	entity, entityID := "settings", "runtime"
	switch {
	case strings.HasPrefix(action, "person_"):
		entity, entityID = "person", fmt.Sprint(id)
	case strings.HasPrefix(action, "file_"):
		entity, entityID = "file", fileID
	case strings.HasPrefix(action, "upload_"):
		entity, entityID = "upload", r.FormValue("upload_id")
	case strings.HasPrefix(action, "session_"):
		entity, entityID = "session", fmt.Sprint(id)
	case strings.HasPrefix(action, "invite_"):
		entity, entityID = "invite", fmt.Sprint(id)
	case strings.HasPrefix(action, "traffic_"):
		entity, entityID = "person", fmt.Sprint(id)
	case strings.HasPrefix(action, "lock"):
		entity, entityID = "lock", r.FormValue("key")
	}
	if e = s.audit(r, action, entity, entityID, "Действие выполнено"); e != nil {
		fail(w, r, 503, "Действие выполнено, но журнал недоступен")
		return
	}
	http.Redirect(w, r, target, 303)
}
func (s *Server) savePerson(r *http.Request, id int64) error {
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" || len([]rune(label)) > 100 || len(r.FormValue("notes")) > 4096 {
		return errors.New("имя обязательно, до 100 символов; заметки до 4096 байт")
	}
	nums := map[string]int64{}
	for _, key := range []string{"storage_quota_gb", "monthly_upload_gb", "monthly_download_gb", "max_file_size_gb"} {
		n, e := gib(r, key)
		if e != nil {
			return e
		}
		nums[key] = n
	}
	var e error
	for _, key := range []string{"max_concurrent_uploads", "session_idle_days", "session_absolute_days"} {
		min := int64(1)
		if key == "session_absolute_days" {
			min = 0
		}
		max := int64(3650)
		if key == "max_concurrent_uploads" {
			max = 100
		}
		nums[key], e = number(r, key, min, max)
		if e != nil {
			return e
		}
	}
	args := []any{label, r.FormValue("notes"), nums["storage_quota_gb"], nums["monthly_upload_gb"], nums["monthly_download_gb"], nums["max_file_size_gb"], nums["max_concurrent_uploads"], checkbox(r, "allow_user_keep_forever"), nums["session_idle_days"], nums["session_absolute_days"], checkbox(r, "ignore_traffic_quota")}
	if id > 0 {
		args = append(args, id)
		res, e := s.db.ExecContext(r.Context(), `UPDATE people SET label=?,notes=?,storage_quota_bytes=?,monthly_upload_limit_bytes=?,monthly_download_limit_bytes=?,max_file_size_bytes=?,max_concurrent_uploads=?,allow_user_keep_forever=?,session_idle_days=?,session_absolute_days=?,ignore_traffic_quota=? WHERE id=? AND is_orphan=0`, args...)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errors.New("пользователь не найден")
		}
		return nil
	}
	args = append(args, time.Now().UTC())
	_, e = s.db.ExecContext(r.Context(), `INSERT INTO people(label,notes,storage_quota_bytes,monthly_upload_limit_bytes,monthly_download_limit_bytes,max_file_size_bytes,max_concurrent_uploads,allow_user_keep_forever,session_idle_days,session_absolute_days,ignore_traffic_quota,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	return e
}
func (s *Server) disablePerson(ctx context.Context, id int64) error {
	s.lifeMu.Lock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		s.lifeMu.Unlock()
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE people SET enabled=0 WHERE id=?", id); e == nil {
		_, e = tx.ExecContext(ctx, "UPDATE device_sessions SET revoked=1 WHERE person_id=?", id)
	}
	if e == nil {
		e = tx.Commit()
	} else {
		tx.Rollback()
	}
	s.lifeMu.Unlock()
	if e != nil {
		return e
	}
	rows, e := s.query(ctx, "SELECT id FROM uploads WHERE person_id=? AND status IN ('reserved','uploading')", id)
	if e != nil {
		return e
	}
	for _, row := range rows {
		upID := fmt.Sprint(row["id"])
		s.stopActiveUpload(upID)
		l := s.lockUpload(upID)
		l.Lock()
		u, err := s.upload(ctx, upID)
		if err == nil {
			err = s.cancelUpload(u, "canceled")
		}
		l.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) deletePerson(r *http.Request, id int64, deleteFiles bool) error {
	if deleteFiles {
		rows, e := s.query(r.Context(), "SELECT id FROM files WHERE person_id=?", id)
		if e != nil {
			return e
		}
		for _, row := range rows {
			if e = s.deleteStored(r.Context(), fmt.Sprint(row["id"])); e != nil {
				return e
			}
		}
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if !deleteFiles {
		if _, e = tx.ExecContext(r.Context(), "UPDATE files SET uploader_name=uploader_name||' (удалён)',person_id=0 WHERE person_id=?", id); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(r.Context(), "DELETE FROM upload_traffic WHERE upload_id IN (SELECT id FROM uploads WHERE person_id=?)", id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(r.Context(), "UPDATE transfer_pending SET person_id=0 WHERE person_id=?", id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(r.Context(), "DELETE FROM people WHERE id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) error {
	pid, e := number(r, "person_id", 1, 1<<60)
	if e != nil {
		return e
	}
	p, e := s.person(r.Context(), pid)
	if e != nil || !p.Enabled {
		return errors.New("выберите активного пользователя")
	}
	count, e := number(r, "max_activations", 1, 10000)
	if e != nil {
		return e
	}
	hours, e := number(r, "expires_hours", 1, 8760)
	if e != nil {
		return e
	}
	_, _, a := s.getSession(r)
	code := auth.GenerateInviteCode()
	now := time.Now().UTC()
	res, e := s.db.ExecContext(r.Context(), `INSERT INTO invite_codes(person_id,code_hash,code_prefix,max_activations,expires_at,created_at,created_by_admin_id) VALUES(?,?,?,?,?,?,?)`, pid, auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt), auth.FormatCodePrefix(code), count, now.Add(time.Duration(hours)*time.Hour), now, a.ID)
	if e != nil {
		return e
	}
	id, e := res.LastInsertId()
	if e != nil {
		return e
	}
	if e = s.audit(r, "invite_created", "invite", fmt.Sprint(id), "Создан инвайт для выбранного пользователя"); e != nil {
		return e
	}
	png, e := qrcode.Encode(code, qrcode.Medium, 256)
	if e != nil {
		return e
	}
	s.render(w, r, "invite_created", map[string]any{"Code": code, "QR": template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)), "Label": p.Label})
	return nil
}
func (s *Server) saveFile(r *http.Request, id string) error {
	f, e := s.file(r.Context(), id)
	if e != nil {
		return e
	}
	forever := checkbox(r, "keep_forever")
	expires := f.ExpiresAt
	if days := r.FormValue("expiry_days"); days != "" {
		n, e := number(r, "expiry_days", 1, 3650)
		if e != nil {
			return e
		}
		t := time.Now().UTC().AddDate(0, 0, int(n))
		expires = &t
	}
	if !forever && expires == nil {
		t := time.Now().UTC().AddDate(0, 0, s.current().StorageDefaults.DefaultExpiryDays)
		expires = &t
	}
	_, e = s.db.ExecContext(r.Context(), "UPDATE files SET protected=?,keep_forever=?,expires_at=? WHERE id=?", checkbox(r, "protected"), forever, expires, id)
	return e
}
func (s *Server) saveSettings(r *http.Request) error {
	old := s.current()
	c := *old
	d := c.StorageDefaults
	fields := []struct {
		key  string
		dest *int64
	}{{"quota_gb", &d.QuotaBytes}, {"upload_limit_gb", &d.MonthlyUploadLimit}, {"download_limit_gb", &d.MonthlyDownloadLimit}, {"max_file_gb", &d.MaxFileSize}}
	for _, f := range fields {
		n, e := gib(r, f.key)
		if e != nil {
			return e
		}
		*f.dest = n
	}
	ints := []struct {
		key      string
		dest     *int
		min, max int64
	}{{"max_concurrent", &d.MaxConcurrentUploads, 1, 100}, {"default_expiry", &d.DefaultExpiryDays, 1, 3650}, {"upload_mbps", &c.SpeedLimits.ExternalUploadMbps, 10, 1000}, {"download_mbps", &c.SpeedLimits.ExternalDownloadMbps, 10, 1000}, {"burst_mb", &c.SpeedLimits.BurstMB, 1, 128}, {"zip_files", &c.ZipLimits.MaxFiles, 1, 10000}}
	for _, f := range ints {
		n, e := number(r, f.key, f.min, f.max)
		if e != nil {
			return e
		}
		*f.dest = int(n)
	}
	n, e := number(r, "zip_gb", 1, 1048576)
	if e != nil {
		return e
	}
	c.ZipLimits.MaxTotalGB = n
	c.ExpiryOptions = nil
	for _, v := range strings.Split(r.FormValue("expiry_options"), ",") {
		n, e := strconv.Atoi(strings.TrimSpace(v))
		if e != nil {
			return e
		}
		c.ExpiryOptions = append(c.ExpiryOptions, n)
	}
	c.SuspiciousExtensions = nil
	for _, v := range strings.Split(r.FormValue("suspicious"), ",") {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			c.SuspiciousExtensions = append(c.SuspiciousExtensions, v)
		}
	}
	d.AllowUserKeepForever = checkbox(r, "allow_forever")
	d.QuarantineSuspicious = checkbox(r, "quarantine")
	c.StorageDefaults = d
	if e = c.ValidateRuntime(); e != nil {
		return e
	}
	v := RuntimeSettings{c.StorageDefaults, c.SpeedLimits, c.ZipLimits, c.ExpiryOptions, c.SuspiciousExtensions}
	data, e := json.Marshal(v)
	if e != nil {
		return e
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if _, e = s.db.ExecContext(r.Context(), "INSERT INTO settings(key,value,updated_at) VALUES('runtime',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", string(data), time.Now().UTC()); e != nil {
		return e
	}
	s.speedLimit.UpdateLimits(c.SpeedLimits.ExternalUploadMbps, c.SpeedLimits.ExternalDownloadMbps, c.SpeedLimits.BurstMB)
	s.settings.Store(&c)
	return nil
}
