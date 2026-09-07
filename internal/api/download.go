package api

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"lares/internal/auth"
	"lares/internal/models"
	"lares/internal/netutils"
	"lares/internal/traffic"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) file(ctx context.Context, id string) (models.FileRecord, error) {
	var f models.FileRecord
	var expiry sql.NullTime
	e := s.db.QueryRowContext(ctx, `SELECT id,person_id,uploader_name,original_name,stored_path,size,content_type,status,flagged,flag_reason,protected,keep_forever,expires_at,created_at,client_ip_hash FROM files WHERE id=?`, id).Scan(&f.ID, &f.PersonID, &f.UploaderName, &f.OriginalName, &f.StoredPath, &f.Size, &f.ContentType, &f.Status, &f.Flagged, &f.FlagReason, &f.Protected, &f.KeepForever, &expiry, &f.CreatedAt, &f.ClientIPHash)
	if expiry.Valid {
		f.ExpiresAt = &expiry.Time
	}
	return f, e
}
func safeMedia(name, ctype string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	types := map[string]string{".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp"}
	want, ok := types[ext]
	if !ok {
		return false
	}
	actual, _, _ := mime.ParseMediaType(ctype)
	return actual == want || (ext == ".ogg" && actual == "application/ogg")
}

// Multiple ranges may legally be ignored; single ranges and suffix ranges are supported.
func parseRange(header string, size int64) (start, length int64, partial bool, err error) {
	if header == "" || !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return 0, size, false, nil
	}
	a, b, ok := strings.Cut(strings.TrimPrefix(header, "bytes="), "-")
	if !ok || size == 0 {
		return 0, 0, false, errors.New("недоступный диапазон")
	}
	if a == "" {
		n, e := strconv.ParseInt(b, 10, 64)
		if e != nil || n <= 0 {
			return 0, 0, false, errors.New("неверный диапазон")
		}
		n = min(n, size)
		return size - n, n, true, nil
	}
	start, e := strconv.ParseInt(a, 10, 64)
	if e != nil || start < 0 || start >= size {
		return 0, 0, false, errors.New("неверный диапазон")
	}
	end := size - 1
	if b != "" {
		end, e = strconv.ParseInt(b, 10, 64)
		if e != nil || end < start {
			return 0, 0, false, errors.New("неверный диапазон")
		}
		end = min(end, size-1)
	}
	return start, end - start + 1, true, nil
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if !s.requestLimit(w, r, "download") {
		return
	}
	_, p, a := s.getSession(r)
	f, e := s.file(r.Context(), r.PathValue("id"))
	if e != nil || (f.ExpiresAt != nil && !f.KeepForever && !f.ExpiresAt.After(time.Now())) {
		fail(w, r, 404, "Файл не найден")
		return
	}
	if f.Status == "quarantined" && a == nil && (p == nil || p.ID != f.PersonID) {
		fail(w, r, 404, "Файл не найден")
		return
	}
	preview := strings.HasPrefix(r.URL.Path, "/preview/")
	if preview && !safeMedia(f.OriginalName, f.ContentType) {
		fail(w, r, 400, "Для этого типа файла доступно только скачивание")
		return
	}
	file, e := s.sm.Open(f.StoredPath)
	if e != nil {
		fail(w, r, 404, "Файл недоступен")
		return
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || info.Size() != f.Size {
		fail(w, r, 503, "Размер файла не соответствует метаданным")
		return
	}
	start, n, partial, e := parseRange(r.Header.Get("Range"), f.Size)
	if e != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", f.Size))
		fail(w, r, 416, "Недоступный диапазон")
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", f.ContentType)
	disposition := "attachment"
	if preview {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.OriginalName}))
	w.Header().Set("Content-Length", fmt.Sprint(n))
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+n-1, f.Size))
	}
	status := 200
	if partial {
		status = 206
	}
	if r.Method == "HEAD" {
		w.WriteHeader(status)
		return
	}
	pending, e := s.beginDownload(r, n)
	if e != nil {
		w.Header().Del("Content-Length")
		w.Header().Del("Content-Range")
		s.downloadFailure(w, r, e)
		return
	}
	defer http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.WriteHeader(status)
	writer := &transferWriter{s: s, r: r, w: w, id: pending}
	limited := s.speedLimit.NewWriter(r.Context(), writer, !s.netChecker.IsLocal(r), false)
	_, e = file.Seek(start, io.SeekStart)
	if e == nil {
		_, e = io.CopyN(limited, file, n)
	}
	if err := s.finishDownload(r, pending, writer.n, e == nil); err != nil {
		s.logError(err)
	}
	if err := s.audit(r, "file_download", "file", f.ID, fmt.Sprintf("Передано %d байт; завершено=%t", writer.n, e == nil)); err != nil {
		s.logError(err)
	}
}

var errDownloadConcurrency = errors.New("достигнут предел одновременных скачиваний")

func (s *Server) downloadFailure(w http.ResponseWriter, r *http.Request, e error) {
	if errors.Is(e, errDownloadConcurrency) {
		s.limited(w, r, "download:concurrent", time.Second)
		return
	}
	fail(w, r, 403, e.Error())
}
func (s *Server) beginDownload(r *http.Request, n int64) (string, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	_, p, _ := s.getSession(r)
	pid := int64(0)
	if p != nil {
		pid = p.ID
	}
	ext := !s.netChecker.IsLocal(r)
	if !ext {
		return "", nil
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	ip := auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt)
	var personN, ipN int
	var pending int64
	if e = tx.QueryRowContext(r.Context(), "SELECT count(*),coalesce(sum(size),0) FROM transfer_pending WHERE person_id=?", pid).Scan(&personN, &pending); e != nil {
		return "", e
	}
	if e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM transfer_pending WHERE ip_hash=?", ip).Scan(&ipN); e != nil {
		return "", e
	}
	if personN >= 2 || ipN >= 4 {
		return "", errDownloadConcurrency
	}
	if p != nil && !p.IgnoreTrafficQuota {
		used, e := traffic.Used(r.Context(), tx, pid, p.MonthlyDownloadLimit, false)
		if e != nil {
			return "", e
		}
		if !traffic.CheckGraceRule(used, pending, n, p.MonthlyDownloadLimit) {
			return "", errors.New("исчерпана месячная квота скачивания")
		}
	}
	id := auth.GenerateRandomID(16)
	if _, e = tx.ExecContext(r.Context(), "INSERT INTO transfer_pending(id,person_id,ip_hash,size,month,created_at) VALUES(?,?,?,?,?,?)", id, pid, ip, n, traffic.GetCurrentMonth(), time.Now().UTC()); e != nil {
		return "", e
	}
	return id, tx.Commit()
}

type transferWriter struct {
	s       *Server
	r       *http.Request
	w       http.ResponseWriter
	id      string
	n, last int64
}

func (t *transferWriter) Write(p []byte) (int, error) {
	_ = http.NewResponseController(t.w).SetWriteDeadline(time.Now().Add(60 * time.Second))
	n, e := t.w.Write(p)
	t.n += int64(n)
	if t.id != "" && t.n-t.last >= 1<<20 {
		if _, err := t.s.db.ExecContext(t.r.Context(), "UPDATE transfer_pending SET bytes=? WHERE id=?", t.n, t.id); err != nil {
			return n, err
		}
		t.last = t.n
	}
	return n, e
}
func (s *Server) finishDownload(r *http.Request, id string, n int64, ok bool) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, p, _ := s.getSession(r)
	pid := int64(0)
	if p != nil {
		pid = p.ID
	}
	month := traffic.GetCurrentMonth()
	kind := "local_download_bytes"
	if id != "" {
		if e = tx.QueryRowContext(ctx, "SELECT person_id,month FROM transfer_pending WHERE id=?", id).Scan(&pid, &month); e != nil {
			return e
		}
		kind = "download_aborted_bytes"
		if ok {
			kind = "download_completed_bytes"
		}
	}
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM people WHERE id=?", pid).Scan(&exists); e != nil {
		return e
	}
	if exists == 0 {
		pid = 0
	}
	if e = traffic.Record(ctx, tx, pid, month, kind, n); e != nil {
		return e
	}
	if id != "" {
		if _, e = tx.ExecContext(ctx, "DELETE FROM transfer_pending WHERE id=?", id); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Server) zipDownload(w http.ResponseWriter, r *http.Request) {
	if !s.requestLimit(w, r, "download") || !s.requestLimit(w, r, "zip") {
		return
	}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	cfg := s.current()
	if len(ids) < 1 || len(ids) > cfg.ZipLimits.MaxFiles {
		fail(w, r, 400, "Неверное число файлов ZIP")
		return
	}
	seen := map[string]bool{}
	var records []models.FileRecord
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	var total, estimated int64
	for _, id := range ids {
		if id == "" || seen[id] {
			fail(w, r, 400, "Пустой или повторный файл ZIP")
			return
		}
		seen[id] = true
		f, e := s.file(r.Context(), id)
		if e != nil || f.Status != "ready" || (f.ExpiresAt != nil && !f.KeepForever && !f.ExpiresAt.After(time.Now())) {
			fail(w, r, 400, "ZIP может содержать только доступные готовые файлы")
			return
		}
		if f.Size > (cfg.ZipLimits.MaxTotalGB<<30)-total {
			fail(w, r, 413, "Превышен суммарный размер ZIP")
			return
		}
		total += f.Size
		estimated += f.Size + int64(len(f.OriginalName))*2 + 256
		file, e := s.sm.Open(f.StoredPath)
		if e != nil {
			fail(w, r, 503, "Один из файлов недоступен")
			return
		}
		opened = append(opened, file)
		records = append(records, f)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=lares.zip")
	if r.Method == "HEAD" {
		w.WriteHeader(200)
		return
	}
	pending, e := s.beginDownload(r, estimated+128)
	if e != nil {
		s.downloadFailure(w, r, e)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=lares.zip")
	tw := &transferWriter{s: s, r: r, w: w, id: pending}
	defer http.NewResponseController(w).SetWriteDeadline(time.Time{})
	zw := zip.NewWriter(s.speedLimit.NewWriter(r.Context(), tw, !s.netChecker.IsLocal(r), false))
	names := map[string]bool{}
	success := true
	for i, f := range records {
		name := f.OriginalName
		for suffix := 2; names[name]; suffix++ {
			name = fmt.Sprintf("%d-%s", suffix, f.OriginalName)
		}
		names[name] = true
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		hdr.SetMode(0640)
		entry, err := zw.CreateHeader(hdr)
		if err == nil {
			_, err = io.CopyN(entry, opened[i], f.Size)
		}
		if err != nil {
			success = false
			break
		}
	}
	if e = zw.Close(); e != nil {
		success = false
	}
	if e = s.finishDownload(r, pending, tw.n, success); e != nil {
		s.logError(e)
	}
	s.recordAudit(r, "zip_download", "files", "", fmt.Sprintf("Файлов %d; байт %d; завершено=%t", len(records), tw.n, success))
}
func (s *Server) deleteStored(ctx context.Context, id string) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var path string
	e = tx.QueryRowContext(ctx, "SELECT stored_path FROM files WHERE id=?", id).Scan(&path)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO file_deletions(id,stored_path) VALUES(?,?) ON CONFLICT(id) DO NOTHING", id, path); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if e = s.sm.DeleteFile(path); e != nil {
		return e
	}
	tx, e = s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "DELETE FROM files WHERE id=?", id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM file_deletions WHERE id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	_, p, a := s.getSession(r)
	id := r.PathValue("id")
	f, e := s.file(r.Context(), id)
	if e != nil {
		fail(w, r, 404, "Файл не найден")
		return
	}
	if a == nil && (p == nil || p.ID != f.PersonID || f.Protected) {
		fail(w, r, 403, "Нельзя удалить этот файл")
		return
	}
	if e = s.deleteStored(r.Context(), id); e != nil {
		fail(w, r, 503, "Не удалось удалить файл")
		return
	}
	s.recordAudit(r, "file_deleted", "file", id, "Файл удалён")
	http.Redirect(w, r, "/", 303)
}
