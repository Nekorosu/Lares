package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lares/internal/auth"
	"lares/internal/cleanup"
	"lares/internal/models"
	"lares/internal/netutils"
	"lares/internal/storage"
	"lares/internal/traffic"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const chunkSize int64 = 32 << 20

type upload struct {
	ID                              string
	PersonID, SessionID             int64
	Secret, Name, Status            string
	Size, Received, External, Local int64
	Expiry                          int
	Until, Created                  time.Time
	ExternalReserved                bool
	Seconds                         float64
	FinalID, Record                 string
}

func (s *Server) upload(ctx context.Context, id string) (upload, error) {
	var u upload
	var sid sql.NullInt64
	e := s.db.QueryRowContext(ctx, `SELECT id,person_id,session_id,upload_secret_hash,original_name,status,declared_size,received_bytes,external_received_bytes,local_received_bytes,expiry_days,reservation_expires_at,created_at,external_reserved,io_seconds,final_file_id,file_record FROM uploads WHERE id=?`, id).Scan(&u.ID, &u.PersonID, &sid, &u.Secret, &u.Name, &u.Status, &u.Size, &u.Received, &u.External, &u.Local, &u.Expiry, &u.Until, &u.Created, &u.ExternalReserved, &u.Seconds, &u.FinalID, &u.Record)
	u.SessionID = sid.Int64
	return u, e
}
func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	sess, p, _ := s.getSession(r)
	if p == nil {
		fail(w, r, 403, "Для загрузки используйте пользовательскую сессию")
		return
	}
	if !s.requestLimit(w, r, "create") {
		return
	}
	var req struct {
		Filename     string `json:"filename"`
		Size         int64  `json:"size"`
		DeclaredSize *int64 `json:"declared_size"`
		ContentType  string `json:"content_type"`
		Expiry       *int   `json:"expiry_days"`
		Forever      bool   `json:"keep_forever"`
	}
	if decode(w, r, &req) != nil {
		fail(w, r, 400, "Неверный запрос загрузки")
		return
	}
	if req.DeclaredSize != nil {
		req.Size = *req.DeclaredSize
	}
	cfg := s.current()
	expiry := cfg.StorageDefaults.DefaultExpiryDays
	if req.Expiry != nil {
		expiry = *req.Expiry
	}
	if req.Forever {
		expiry = 0
	}
	if expiry == 0 && !p.AllowUserKeepForever {
		fail(w, r, 403, "Бессрочное хранение не разрешено")
		return
	}
	valid := expiry == 0
	for _, d := range cfg.ExpiryOptions {
		if expiry == d {
			valid = true
		}
	}
	if !valid || req.Size < 0 || req.Size > 1<<60 {
		fail(w, r, 400, "Неверный размер или срок хранения")
		return
	}
	ext := !s.netChecker.IsLocal(r)
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, r, 503, "Ошибка резервирования")
		return
	}
	defer tx.Rollback()
	var enabled, foreverAllowed bool
	var quota, maxFile, trafficLimit int64
	var concurrent int
	var ignore bool
	var speed float64
	e = tx.QueryRowContext(r.Context(), "SELECT enabled,storage_quota_bytes,max_file_size_bytes,max_concurrent_uploads,monthly_upload_limit_bytes,ignore_traffic_quota,upload_speed,allow_user_keep_forever FROM people WHERE id=?", p.ID).Scan(&enabled, &quota, &maxFile, &concurrent, &trafficLimit, &ignore, &speed, &foreverAllowed)
	if e != nil || !enabled {
		fail(w, r, 403, "Пользователь отключён")
		return
	}
	if expiry == 0 && !foreverAllowed {
		fail(w, r, 403, "Бессрочное хранение не разрешено")
		return
	}
	if req.Size > maxFile {
		fail(w, r, 413, "Файл больше разрешённого размера")
		return
	}
	var active int
	var stored, reserved, diskPending, trafficPending int64
	for _, q := range []struct {
		sql  string
		args []any
		dest any
	}{{"SELECT count(*) FROM uploads WHERE person_id=? AND status IN ('reserved','uploading')", []any{p.ID}, &active}, {"SELECT coalesce(sum(size),0) FROM files WHERE person_id=? AND status='ready'", []any{p.ID}, &stored}, {"SELECT coalesce(sum(declared_size),0) FROM uploads WHERE person_id=? AND status IN ('reserved','uploading')", []any{p.ID}, &reserved}, {"SELECT coalesce(sum(max(0,declared_size-received_bytes)),0) FROM uploads WHERE status IN ('reserved','uploading')", nil, &diskPending}, {"SELECT coalesce(sum(declared_size),0) FROM uploads WHERE person_id=? AND external_reserved=1 AND status IN ('reserved','uploading')", []any{p.ID}, &trafficPending}} {
		if e = tx.QueryRowContext(r.Context(), q.sql, q.args...).Scan(q.dest); e != nil {
			fail(w, r, 503, "Ошибка расчёта резерва")
			return
		}
	}
	if active >= concurrent {
		fail(w, r, 409, "Достигнут предел одновременных загрузок")
		return
	}
	if stored > quota || reserved > quota-stored || req.Size > quota-stored-reserved {
		fail(w, r, 403, "Недостаточно места в вашей квоте")
		return
	}
	if ext && !ignore {
		used, err := traffic.Used(r.Context(), tx, p.ID, trafficLimit, true)
		if err != nil {
			fail(w, r, 503, "Ошибка учёта трафика")
			return
		}
		if !traffic.CheckGraceRule(used, trafficPending, req.Size, trafficLimit) {
			fail(w, r, 403, "Исчерпана месячная квота загрузки")
			return
		}
	}
	if e = s.sm.CheckReserved(req.Size, diskPending); e != nil {
		fail(w, r, 507, e.Error())
		return
	}
	id, secret := auth.GenerateRandomID(16), auth.GenerateRandomToken(32)
	now := time.Now().UTC()
	until := now.Add(cleanup.ReservationTTL(req.Size, speed))
	_, e = tx.ExecContext(r.Context(), `INSERT INTO uploads(id,person_id,session_id,upload_secret_hash,original_name,declared_size,status,expiry_days,reservation_expires_at,created_at,client_ip_hash,external_reserved) VALUES(?,?,?,?,?,?,'reserved',?,?,?,?,?)`, id, p.ID, sess.ID, auth.HashWithSalt(secret, s.cfg.Secrets.IPHashSalt), storage.SanitizeFilename(req.Filename), req.Size, expiry, until, now, auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt), ext)
	if e != nil {
		fail(w, r, 503, "Не удалось создать резерв")
		return
	}
	f, _, e := s.sm.PreparePartFile(id)
	if e != nil {
		fail(w, r, 507, "Не удалось создать временный файл")
		return
	}
	e = f.Close()
	if e != nil {
		fail(w, r, 507, "Ошибка временного файла")
		return
	}
	if e = tx.Commit(); e != nil {
		s.sm.DeletePartFile(id)
		fail(w, r, 503, "Резерв не сохранён")
		return
	}
	if e = s.audit(r, "upload_reserved", "upload", id, "Загрузка зарезервирована"); e != nil {
		fail(w, r, 503, "Ошибка журнала")
		return
	}
	jsonOut(w, 200, map[string]any{"upload_id": id, "upload_secret": secret, "chunk_size": chunkSize, "expires_at": until})
}
func (s *Server) uploadAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lock := s.lockUpload(id)
	lock.Lock()
	defer lock.Unlock()
	u, e := s.upload(r.Context(), id)
	if e != nil {
		fail(w, r, 404, "Загрузка не найдена")
		return
	}
	_, p, _ := s.getSession(r)
	secret := r.Header.Get("X-Upload-Secret")
	if p == nil || p.ID != u.PersonID || subtle.ConstantTimeCompare([]byte(auth.HashWithSalt(secret, s.cfg.Secrets.IPHashSalt)), []byte(u.Secret)) != 1 {
		fail(w, r, 403, "Нет доступа к загрузке")
		return
	}
	// Re-read access after waiting on the upload lock; the session may have been revoked meanwhile.
	var allowed int
	e = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM device_sessions ds JOIN people p ON p.id=ds.person_id WHERE ds.id=? AND ds.revoked=0 AND ds.idle_expires_at>? AND (ds.absolute_expires_at IS NULL OR ds.absolute_expires_at>?) AND p.enabled=1`, r.Context().Value(identityKey{}).(*identity).Session.ID, time.Now().UTC(), time.Now().UTC()).Scan(&allowed)
	if e != nil || allowed != 1 {
		fail(w, r, 403, "Доступ отозван")
		return
	}
	if u.Status != "reserved" && u.Status != "uploading" {
		if u.Status == "completed" && (r.Method == "HEAD" || r.Method == "POST") {
			if r.Method == "HEAD" {
				w.Header().Set("Upload-Offset", fmt.Sprint(u.Received))
				w.Header().Set("Upload-Length", fmt.Sprint(u.Size))
				w.WriteHeader(200)
			} else {
				jsonOut(w, 200, map[string]string{"status": "completed"})
			}
			return
		}
		if r.Method == "DELETE" && u.Status == "canceled" {
			w.WriteHeader(204)
			return
		}
		fail(w, r, 409, "Загрузка уже завершена или отменена")
		return
	}
	if !u.Until.After(time.Now()) {
		if e = s.cancelUpload(u, "expired"); e != nil {
			fail(w, r, 503, "Ошибка очистки")
			return
		}
		fail(w, r, 410, "Срок резерва истёк")
		return
	}
	if r.Method == "HEAD" {
		w.Header().Set("Upload-Offset", fmt.Sprint(u.Received))
		w.Header().Set("Upload-Length", fmt.Sprint(u.Size))
		w.WriteHeader(200)
		return
	}
	if r.Method == "DELETE" {
		if e = s.cancelUpload(u, "canceled"); e != nil {
			fail(w, r, 503, "Не удалось отменить загрузку")
			return
		}
		s.recordAudit(r, "upload_canceled", "upload", id, "Загрузка отменена")
		w.WriteHeader(204)
		return
	}
	if r.Method == "POST" {
		if e = s.completeUpload(r, u, p); e != nil {
			fail(w, r, 409, e.Error())
			return
		}
		jsonOut(w, 200, map[string]any{"status": "completed"})
		return
	}
	if !s.requestLimit(w, r, "chunk") {
		return
	}
	offset, e := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if e != nil || offset != u.Received {
		fail(w, r, 409, fmt.Sprintf("Ожидается позиция %d", u.Received))
		return
	}
	remaining := u.Size - u.Received
	if remaining < 0 {
		fail(w, r, 409, "Повреждённый резерв")
		return
	}
	bound := min(chunkSize, remaining)
	if r.ContentLength > bound {
		fail(w, r, 413, "Фрагмент превышает зарезервированный размер")
		return
	}
	ext := !s.netChecker.IsLocal(r)
	if ext && !u.ExternalReserved {
		if e = s.reserveExternal(r, u, p); e != nil {
			fail(w, r, 403, e.Error())
			return
		}
	}
	if e = s.sm.CheckDiskSpaceCritical(); e != nil {
		s.cancelUpload(u, "failed")
		fail(w, r, 507, e.Error())
		return
	}
	f, _, e := s.sm.PreparePartFile(id)
	if e != nil {
		fail(w, r, 507, "Ошибка открытия фрагмента")
		return
	}
	defer f.Close()
	if e = f.Truncate(u.Received); e != nil {
		fail(w, r, 507, "Ошибка длины файла")
		return
	}
	if _, e = f.Seek(u.Received, io.SeekStart); e != nil {
		fail(w, r, 507, "Ошибка позиции")
		return
	}
	controller := http.NewResponseController(w)
	chunkCtx, stop := context.WithCancel(r.Context())
	s.activeUploads.Store(id, func() { stop(); _ = controller.SetReadDeadline(time.Now()) })
	defer func() { s.activeUploads.Delete(id); stop(); _ = controller.SetReadDeadline(time.Time{}) }()
	pr := &progressReader{r: r.Body, controller: controller, sm: s.sm}
	reader := s.speedLimit.NewReader(chunkCtx, pr, ext, true)
	start := time.Now()
	n, copyErr := io.CopyBuffer(f, io.LimitReader(reader, bound+1), make([]byte, 64<<10))
	syncErr := f.Sync()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancel()
	if e = s.recordChunk(ctx, u, n, ext, time.Since(start).Seconds()); e != nil {
		fail(w, r, 503, "Не удалось записать прогресс; повторите запрос позиции")
		return
	}
	u, e = s.upload(ctx, id)
	if e != nil {
		fail(w, r, 503, "Ошибка состояния")
		return
	}
	if n > bound || syncErr != nil || errors.Is(copyErr, storage.ErrCriticalDiskSpace) {
		if err := s.cancelUpload(u, "failed"); err != nil {
			s.logError(err)
		}
		code := 507
		if n > bound {
			code = 413
		}
		fail(w, r, code, "Передача превысила резерв или исчерпала доступное место")
		return
	}
	if copyErr != nil {
		fail(w, r, 400, "Передача прервана; можно продолжить с сохранённой позиции")
		return
	}
	w.Header().Set("Upload-Offset", fmt.Sprint(u.Received))
	w.WriteHeader(204)
}

type progressReader struct {
	r          io.Reader
	controller *http.ResponseController
	sm         *storage.StorageManager
}

func (p *progressReader) Read(b []byte) (int, error) {
	if e := p.sm.CheckDiskSpaceCritical(); e != nil {
		return 0, e
	}
	_ = p.controller.SetReadDeadline(time.Now().Add(60 * time.Second))
	return p.r.Read(b)
}
func (s *Server) reserveExternal(r *http.Request, u upload, p *models.Person) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	used, e := traffic.Used(r.Context(), tx, p.ID, p.MonthlyUploadLimitBytes, true)
	if e != nil {
		return e
	}
	var pending int64
	if e = tx.QueryRowContext(r.Context(), "SELECT coalesce(sum(declared_size),0) FROM uploads WHERE person_id=? AND external_reserved=1 AND status IN ('reserved','uploading')", p.ID).Scan(&pending); e != nil {
		return e
	}
	if !p.IgnoreTrafficQuota && !traffic.CheckGraceRule(used, pending, u.Size-u.Received, p.MonthlyUploadLimitBytes) {
		return errors.New("исчерпана внешняя квота загрузки")
	}
	if _, e = tx.ExecContext(r.Context(), "UPDATE uploads SET external_reserved=1 WHERE id=?", u.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) recordChunk(ctx context.Context, u upload, n int64, external bool, seconds float64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	ext, local := int64(0), int64(0)
	if external {
		ext = n
	} else {
		local = n
	}
	var speed float64
	if e = tx.QueryRowContext(ctx, "SELECT upload_speed FROM people WHERE id=?", u.PersonID).Scan(&speed); e != nil {
		return e
	}
	until := time.Now().UTC().Add(cleanup.ReservationTTL(max(0, u.Size-u.Received-n), speed))
	if until.Before(u.Until) {
		until = u.Until
	}
	if _, e = tx.ExecContext(ctx, "UPDATE uploads SET received_bytes=received_bytes+?,external_received_bytes=external_received_bytes+?,local_received_bytes=local_received_bytes+?,io_seconds=io_seconds+?,status='uploading',reservation_expires_at=? WHERE id=?", n, ext, local, seconds, until, u.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO upload_traffic(upload_id,month,external_bytes,local_bytes) VALUES(?,?,?,?) ON CONFLICT(upload_id,month) DO UPDATE SET external_bytes=external_bytes+excluded.external_bytes,local_bytes=local_bytes+excluded.local_bytes", u.ID, traffic.GetCurrentMonth(), ext, local); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) settleUpload(ctx context.Context, tx *sql.Tx, u upload, completed bool) error {
	rows, e := tx.QueryContext(ctx, "SELECT month,external_bytes,local_bytes FROM upload_traffic WHERE upload_id=?", u.ID)
	if e != nil {
		return e
	}
	type count struct {
		month      string
		ext, local int64
	}
	var counts []count
	for rows.Next() {
		var c count
		if e = rows.Scan(&c.month, &c.ext, &c.local); e != nil {
			rows.Close()
			return e
		}
		counts = append(counts, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	kind := "upload_aborted_bytes"
	if completed {
		kind = "upload_completed_bytes"
	}
	for _, c := range counts {
		if e = traffic.Record(ctx, tx, u.PersonID, c.month, kind, c.ext); e != nil {
			return e
		}
		if e = traffic.Record(ctx, tx, u.PersonID, c.month, "local_upload_bytes", c.local); e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, "DELETE FROM upload_traffic WHERE upload_id=?", u.ID)
	return e
}
func (s *Server) cancelUpload(u upload, status string) error {
	if u.Status != "reserved" && u.Status != "uploading" {
		return nil
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 10*time.Second)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if u.FinalID != "" {
		if e = s.sm.DeleteFile(s.sm.GetShardedPath(u.FinalID)); e != nil {
			return e
		}
	}
	if e = s.sm.DeletePartFile(u.ID); e != nil {
		return e
	}
	if e = s.settleUpload(ctx, tx, u, false); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE uploads SET status=? WHERE id=? AND status IN ('reserved','uploading')", status, u.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) completeUpload(r *http.Request, u upload, p *models.Person) error {
	if u.FinalID != "" {
		final, err := s.sm.Open(s.sm.GetShardedPath(u.FinalID))
		if err == nil {
			final.Close()
			var record models.FileRecord
			if err = json.Unmarshal([]byte(u.Record), &record); err != nil {
				return err
			}
			return s.commitFile(u, record)
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	f, e := s.sm.Open(s.sm.GetPartPath(u.ID))
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if st.Size() > u.Size || st.Size() != u.Received {
		return errors.New("размер файла не соответствует резерву")
	}
	prefix, e := storage.Sniff(f)
	if e != nil {
		return e
	}
	ctype := http.DetectContentType([]byte(prefix))
	status, reason := s.suspicious(u.Name, ctype)
	if e = f.Sync(); e != nil {
		return e
	}
	id := auth.GenerateRandomID(16)
	var expires *time.Time
	if u.Expiry > 0 {
		t := time.Now().UTC().AddDate(0, 0, u.Expiry)
		expires = &t
	}
	record := models.FileRecord{ID: id, PersonID: u.PersonID, UploaderName: p.Label, OriginalName: u.Name, StoredPath: s.sm.GetShardedPath(id), Size: u.Received, ContentType: ctype, Status: models.FileStatus(status), Flagged: reason != "", FlagReason: reason, KeepForever: u.Expiry == 0, ExpiresAt: expires, CreatedAt: time.Now().UTC(), ClientIPHash: auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt)}
	data, e := json.Marshal(record)
	if e != nil {
		return e
	}
	// Persist recovery intent before rename. Recovery completes this exact metadata after a crash.
	if _, e = s.db.ExecContext(r.Context(), "UPDATE uploads SET final_file_id=?,file_record=? WHERE id=?", id, string(data), u.ID); e != nil {
		return e
	}
	u.FinalID = id
	u.Record = string(data)
	if _, e = s.sm.FinalizeUpload(u.ID, id); e != nil {
		return e
	}
	if e = s.commitFile(u, record); e != nil {
		return e
	}
	return s.audit(r, "upload_completed", "file", id, "Файл сохранён")
}
func (s *Server) commitFile(u upload, f models.FileRecord) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	tx, e := s.db.BeginTx(context.WithoutCancel(s.ctx), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var enabled bool
	if e = tx.QueryRow("SELECT enabled FROM people WHERE id=?", u.PersonID).Scan(&enabled); e != nil {
		return e
	}
	if !enabled {
		return errors.New("пользователь отключён")
	}
	_, e = tx.Exec(`INSERT INTO files(id,person_id,uploader_name,original_name,stored_path,size,content_type,status,flagged,flag_reason,protected,keep_forever,expires_at,created_at,client_ip_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, f.ID, f.PersonID, f.UploaderName, f.OriginalName, f.StoredPath, f.Size, f.ContentType, f.Status, f.Flagged, f.FlagReason, false, f.KeepForever, f.ExpiresAt, f.CreatedAt, f.ClientIPHash)
	if e != nil {
		return e
	}
	if e = s.settleUpload(context.WithoutCancel(s.ctx), tx, u, true); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE uploads SET status='completed',completed_at=? WHERE id=?", time.Now().UTC(), u.ID); e != nil {
		return e
	}
	if u.Seconds > 0 && u.Received > 0 {
		if _, e = tx.Exec("UPDATE people SET upload_speed=0.8*upload_speed+0.2*? WHERE id=?", float64(u.Received)/u.Seconds, u.PersonID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Server) suspicious(name, ctype string) (string, string) {
	cfg := s.current()
	reason := ""
	parts := strings.Split(strings.ToLower(name), ".")
	for _, part := range parts[1:] {
		for _, ext := range cfg.SuspiciousExtensions {
			if part == strings.ToLower(ext) {
				reason = "Подозрительное расширение ." + part
			}
		}
	}
	expected := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	expected, _, _ = mime.ParseMediaType(expected)
	actual, _, _ := mime.ParseMediaType(ctype)
	if expected != "" && actual != "application/octet-stream" && expected != actual {
		if !(strings.HasPrefix(expected, "text/") && actual == "text/plain") {
			reason = "Содержимое не соответствует расширению"
		}
	}
	if reason != "" && cfg.StorageDefaults.QuarantineSuspicious {
		return "quarantined", reason
	}
	return "ready", reason
}
func (s *Server) listMyUploads(w http.ResponseWriter, r *http.Request) {
	if !s.requestLimit(w, r, "list") {
		return
	}
	_, p, _ := s.getSession(r)
	if p == nil {
		jsonOut(w, 200, []any{})
		return
	}
	rows, e := s.query(r.Context(), "SELECT id,original_name,declared_size,received_bytes,reservation_expires_at FROM uploads WHERE person_id=? AND status IN ('reserved','uploading')", p.ID)
	if e != nil {
		fail(w, r, 503, "Ошибка списка")
		return
	}
	jsonOut(w, 200, rows)
}

func (s *Server) stopActiveUpload(id string) {
	if stop, ok := s.activeUploads.Load(id); ok {
		stop.(func())()
	}
}
