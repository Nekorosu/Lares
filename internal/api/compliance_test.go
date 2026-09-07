package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/models"
	"lares/internal/traffic"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func complianceServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	s, _ := setupTestServer(t)
	insertTestUserAndSession(t, s, "user")
	return s, s.Routes()
}
func request(h http.Handler, method, path, body, token string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "homeshare_csrf", Value: strings.Repeat("c", 64)})
	r.Header.Set("X-CSRF-Token", strings.Repeat("c", 64))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "homeshare_session", Value: token})
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP %d, want %d: %s", w.Code, want, w.Body.String())
	}
}
func reserve(t *testing.T, h http.Handler, path string, size int64) (string, map[string]string) {
	t.Helper()
	w := request(h, "POST", path, fmt.Sprintf(`{"filename":"test.txt","size":%d}`, size), "user", nil)
	requireStatus(t, w, 200)
	var v struct {
		ID     string `json:"upload_id"`
		Secret string `json:"upload_secret"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if v.ID == "" || v.Secret == "" {
		t.Fatal("missing reservation credentials")
	}
	return v.ID, map[string]string{"X-Upload-Secret": v.Secret}
}
func execSQL(t *testing.T, s *Server, q string, args ...any) {
	t.Helper()
	if _, e := s.db.Exec(q, args...); e != nil {
		t.Fatal(e)
	}
}
func countSQL(t *testing.T, s *Server, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if e := s.db.QueryRow(q, args...).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func addAdmin(t *testing.T, s *Server) {
	t.Helper()
	now := time.Now().UTC()
	execSQL(t, s, "INSERT INTO admin_users(id,username,password_hash,totp_secret,created_at) VALUES(5,'admin','unused','unused',?)", now)
	execSQL(t, s, `INSERT INTO device_sessions(admin_id,is_admin,name,session_token_hash,created_at,last_used_at,last_ip_hash,last_user_agent_hash,idle_expires_at,absolute_expires_at) VALUES(5,1,'admin',?,?,?,'','',?,?)`, auth.HashWithSalt("admin", s.cfg.Secrets.SessionSecret), now, now, now.Add(time.Hour), now.Add(7*24*time.Hour))
}
func addFile(t *testing.T, s *Server, id, name, mime string, relative bool) string {
	t.Helper()
	p := s.sm.GetShardedPath(id)
	if e := os.MkdirAll(filepath.Dir(p), 0750); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte("1234567890"), 0640); e != nil {
		t.Fatal(e)
	}
	stored := p
	if relative {
		stored, _ = filepath.Rel(s.cfg.DataDir, p)
	}
	execSQL(t, s, `INSERT INTO files(id,person_id,uploader_name,original_name,stored_path,size,content_type,status,created_at,client_ip_hash) VALUES(?,1,'TestUser',?,?,10,?,'ready',?,'')`, id, name, stored, mime, time.Now().UTC())
	return p
}
func formRequest(h http.Handler, path string, v url.Values, token string) *httptest.ResponseRecorder {
	v.Set("csrf_token", strings.Repeat("c", 64))
	return request(h, "POST", path, v.Encode(), token, map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}
func cachedRequest(s *Server) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "homeshare_session", Value: "user"})
	ss, p, a := s.getSession(r)
	return r.WithContext(context.WithValue(r.Context(), identityKey{}, &identity{*ss, p, a}))
}

func TestComplianceUnifiedReservationAndAtomicQuota(t *testing.T) {
	s, h := complianceServer(t)
	execSQL(t, s, "UPDATE people SET storage_quota_bytes=10,max_file_size_bytes=10 WHERE id=1")
	requireStatus(t, request(h, "POST", "/api/files/upload/reserve", `{"filename":"x","size":11}`, "user", nil), 413)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, path := range []string{"/api/uploads", "/api/files/upload/reserve"} {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			codes <- request(h, "POST", path, `{"filename":"x","size":6}`, "user", nil).Code
		}(path)
	}
	wg.Wait()
	close(codes)
	good := 0
	for c := range codes {
		if c == 200 {
			good++
		} else if c != 403 {
			t.Fatalf("unexpected %d", c)
		}
	}
	if good != 1 {
		t.Fatalf("atomic quota accepted %d", good)
	}
	if n := countSQL(t, s, "SELECT sum(declared_size) FROM uploads"); n != 6 {
		t.Fatal(n)
	}
}
func TestComplianceFullReservationAfterChunkAndGrace(t *testing.T) {
	s, h := complianceServer(t)
	execSQL(t, s, "UPDATE people SET storage_quota_bytes=10,monthly_upload_limit_bytes=100 WHERE id=1")
	id, hdr := reserve(t, h, "/api/uploads", 8)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=0", "123456", "user", hdr), 204)
	requireStatus(t, request(h, "POST", "/api/uploads", `{"filename":"x","size":3}`, "user", nil), 403)
	execSQL(t, s, "UPDATE people SET storage_quota_bytes=100,monthly_upload_limit_bytes=5 WHERE id=1")
	// A transfer admitted by grace can finish after its limit is lowered.
	requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "user", hdr), 200)
	if n := countSQL(t, s, "SELECT upload_completed_bytes FROM traffic_counters WHERE person_id=1"); n != 6 {
		t.Fatal(n)
	}
	requireStatus(t, request(h, "POST", "/api/uploads", `{"filename":"x","size":1}`, "user", nil), 403)
}
func TestComplianceUploadStateAndUnknownLength(t *testing.T) {
	s, h := complianceServer(t)
	id, hdr := reserve(t, h, "/api/uploads", 3)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=0", "1234", "user", hdr), 413)
	r := httptest.NewRequest("PATCH", "/api/uploads/"+id+"?offset=0", io.NopCloser(strings.NewReader("1234")))
	r.ContentLength = -1
	r.Header.Set("Authorization", "Bearer user")
	r.Header.Set("X-Upload-Secret", hdr["X-Upload-Secret"])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireStatus(t, w, 413)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=0", "1", "user", hdr), 409)
	if _, e := s.sm.Open(s.sm.GetPartPath(id)); !os.IsNotExist(e) {
		t.Fatalf("failed part remains: %v", e)
	}
	if n := countSQL(t, s, "SELECT upload_aborted_bytes FROM traffic_counters WHERE person_id=1"); n != 4 {
		t.Fatal(n)
	}
	id, hdr = reserve(t, h, "/api/uploads", 5)
	requireStatus(t, request(h, "DELETE", "/api/uploads/"+id, "", "user", hdr), 204)
	requireStatus(t, request(h, "DELETE", "/api/uploads/"+id, "", "user", hdr), 204)
	requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "user", hdr), 409)
	requireStatus(t, request(h, "POST", "/api/files/upload/direct", "unbounded", "user", nil), 410)
}
func TestComplianceUploadSecretSessionAndOffset(t *testing.T) {
	_, h := complianceServer(t)
	id, hdr := reserve(t, h, "/api/uploads", 10)
	requireStatus(t, request(h, "HEAD", "/api/uploads/"+id, "", "user", nil), 403)
	requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "", hdr), 401)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=1", "x", "user", hdr), 409)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=0", "123", "user", hdr), 204)
	w := request(h, "HEAD", "/api/uploads/"+id, "", "user", hdr)
	requireStatus(t, w, 200)
	if w.Header().Get("Upload-Offset") != "3" {
		t.Fatal(w.Header())
	}
	requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "user", hdr), 200)
	requireStatus(t, request(h, "HEAD", "/api/uploads/"+id, "", "user", hdr), 200)
	requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "user", hdr), 200)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=3", "x", "user", hdr), 409)

}
func TestComplianceCSRFAndDeletedAdmin(t *testing.T) {
	s, h := complianceServer(t)
	addAdmin(t, s)
	requireStatus(t, request(h, "POST", "/api/uploads", `{"filename":"x","size":1}`, "user", map[string]string{"X-CSRF-Token": ""}), 403)
	request(h, "GET", "/admin/action/person_disable?id=1", "", "admin", nil)
	if n := countSQL(t, s, "SELECT enabled FROM people WHERE id=1"); n != 1 {
		t.Fatal("GET changed Person")
	}
	requireStatus(t, formRequest(h, "/admin/action/person_disable", url.Values{"id": {"1"}}, "admin"), 303)
	if n := countSQL(t, s, "SELECT enabled FROM people WHERE id=1"); n != 0 {
		t.Fatal("POST did not disable")
	}
	execSQL(t, s, "DELETE FROM admin_users WHERE id=5")
	requireStatus(t, request(h, "GET", "/admin/people", "", "admin", nil), 303)
	if n := countSQL(t, s, "SELECT count(*) FROM device_sessions WHERE admin_id=5"); n != 0 {
		t.Fatal("admin sessions survived")
	}
}
func TestCompliancePersonDeletionAndDisable(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			s, h := complianceServer(t)
			addAdmin(t, s)
			p := addFile(t, s, "existing", "file.txt", "text/plain", false)
			id, _ := reserve(t, h, "/api/uploads", 10)
			v := url.Values{"id": {"1"}}
			if remove {
				v.Set("delete_files", "on")
			}
			requireStatus(t, formRequest(h, "/admin/action/person_delete", v, "admin"), 303)
			_, err := os.Stat(p)
			if remove {
				if !os.IsNotExist(err) {
					t.Fatal("bytes retained")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if n := countSQL(t, s, "SELECT person_id FROM files WHERE id='existing'"); n != 0 {
					t.Fatal(n)
				}
				var label string
				s.db.QueryRow("SELECT uploader_name FROM files WHERE id='existing'").Scan(&label)
				if !strings.Contains(label, "удалён") {
					t.Fatal(label)
				}
			}
			if _, e := os.Stat(s.sm.GetPartPath(id)); !os.IsNotExist(e) {
				t.Fatal("part survived deletion")
			}
		})
	}
}
func TestComplianceDownloadRangeHeadPreviewZip(t *testing.T) {
	s, h := complianceServer(t)
	addFile(t, s, "media", "a.jpg", "image/jpeg", true)
	w := request(h, "GET", "/download/media", "", "user", map[string]string{"Range": "bytes=2-4"})
	requireStatus(t, w, 206)
	if w.Body.String() != "345" {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, request(h, "HEAD", "/download/media", "", "user", nil), 200)
	requireStatus(t, request(h, "HEAD", "/api/zip?ids=media", "", "user", nil), 200)
	if n := countSQL(t, s, "SELECT download_completed_bytes FROM traffic_counters WHERE person_id=1"); n != 3 {
		t.Fatal(n)
	}
	w = request(h, "GET", "/api/zip?ids=media", "", "user", nil)
	requireStatus(t, w, 200)
	z, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	if len(z.File) != 1 || z.File[0].Method != zip.Store {
		t.Fatal("invalid zip")
	}
	f, e := z.File[0].Open()
	if e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(f)
	f.Close()
	if e != nil || string(data) != "1234567890" {
		t.Fatalf("zip data: %q %v", data, e)
	}
	if n := countSQL(t, s, "SELECT download_completed_bytes FROM traffic_counters WHERE person_id=1"); n != 3+int64(w.Body.Len()) {
		t.Fatal(n)
	}
	execSQL(t, s, "UPDATE people SET monthly_download_limit_bytes=1 WHERE id=1")
	requireStatus(t, request(h, "GET", "/preview/media", "", "user", nil), 403)
	requireStatus(t, request(h, "GET", "/preview/media", "", "user", map[string]string{"X-Real-IP": "192.168.32.2"}), 403) // Remote peer is external, cannot spoof locality.
}

type brokenWriter struct {
	h http.Header
	n int
}

func (w *brokenWriter) Header() http.Header { return w.h }
func (w *brokenWriter) WriteHeader(int)     {}
func (w *brokenWriter) Write(p []byte) (int, error) {
	n := min(3, len(p))
	w.n += n
	return n, io.ErrClosedPipe
}
func TestComplianceAbortAndDownloadConcurrency(t *testing.T) {
	s, h := complianceServer(t)
	addFile(t, s, "abort", "a.txt", "text/plain", false)
	r := httptest.NewRequest("GET", "/download/abort", nil)
	r.Header.Set("Authorization", "Bearer user")
	w := &brokenWriter{h: http.Header{}}
	h.ServeHTTP(w, r)
	if n := countSQL(t, s, "SELECT download_aborted_bytes FROM traffic_counters WHERE person_id=1"); n != 3 {
		t.Fatal(n)
	}
	if n := countSQL(t, s, "SELECT count(*) FROM transfer_pending"); n != 0 {
		t.Fatal(n)
	}
	cached := cachedRequest(s)
	a, e := s.beginDownload(cached, 10)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.beginDownload(cached, 10)
	if e != nil {
		t.Fatal(e)
	}
	resp := request(h, "GET", "/download/abort", "", "user", nil)
	requireStatus(t, resp, 429)
	if resp.Header().Get("Retry-After") == "" {
		t.Fatal("no retry")
	}
	if e = s.finishDownload(cached, a, 0, false); e != nil {
		t.Fatal(e)
	}
	if e = s.finishDownload(cached, b, 0, false); e != nil {
		t.Fatal(e)
	}
}
func TestComplianceInviteLocksAndAtomicUse(t *testing.T) {
	s, h := complianceServer(t)
	code := auth.GenerateInviteCode()
	now := time.Now().UTC()
	execSQL(t, s, `INSERT INTO invite_codes(person_id,code_hash,code_prefix,expires_at,created_at,created_by_admin_id) VALUES(1,?,'test',?,?,5)`, auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt), now.Add(24*time.Hour), now)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(h, "POST", "/api/auth/invite/activate", fmt.Sprintf(`{"code":%q}`, code), "", nil).Code
		}()
	}
	wg.Wait()
	close(codes)
	good := 0
	for c := range codes {
		if c == 200 {
			good++
		}
	}
	if good != 1 {
		t.Fatalf("one-use invite activated %d times", good)
	}
	for i := 0; i < 6; i++ {
		w := request(h, "POST", "/api/auth/invite/activate", `{"code":"invalid"}`, "", nil)
		if w.Code == 429 {
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("retry missing")
			}
			return
		}
	}
	t.Fatal("invite not locked")
}
func TestComplianceSessionIdleAndCookie(t *testing.T) {
	s, h := complianceServer(t)
	execSQL(t, s, "UPDATE people SET session_idle_days=1,session_absolute_days=0 WHERE id=1")
	execSQL(t, s, "UPDATE device_sessions SET absolute_expires_at=NULL WHERE person_id=1")
	requireStatus(t, request(h, "GET", "/api/auth/me", "", "user", nil), 200)
	var expiry time.Time
	if e := s.db.QueryRow("SELECT idle_expires_at FROM device_sessions WHERE person_id=1").Scan(&expiry); e != nil {
		t.Fatal(e)
	}
	if time.Until(expiry) > 25*time.Hour || time.Until(expiry) < 23*time.Hour {
		t.Fatal(expiry)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://host/", nil)
	s.sessionCookie(w, r, "secret")
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatal(c)
	}
}
func TestCompliancePagesAndQuarantine(t *testing.T) {
	s, h := complianceServer(t)
	addAdmin(t, s)
	addFile(t, s, "image", "<script>.jpg", "image/jpeg", false)
	execSQL(t, s, "UPDATE files SET status='quarantined',flagged=1,flag_reason='reason' WHERE id='image'")
	for _, path := range []string{"/login", "/admin/login", "/", "/devices", "/admin/dashboard", "/admin/people", "/admin/people?edit=1", "/admin/invites", "/admin/sessions", "/admin/files", "/admin/uploads", "/admin/quarantine", "/admin/traffic", "/admin/audit", "/admin/settings"} {
		token := "user"
		if strings.HasPrefix(path, "/admin/") {
			token = "admin"
		}
		w := request(h, "GET", path, "", token, nil)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), "</html>") || strings.Contains(w.Body.String(), "<script>.jpg") {
			t.Fatalf("broken or unescaped page %s", path)
		}
	}
	if status, _ := s.suspicious("report.exe.txt", "text/plain"); status != "quarantined" {
		t.Fatal(status)
	}
	if status, _ := s.suspicious("report.jpg", "text/html"); status != "quarantined" {
		t.Fatal(status)
	}
	c := *s.current()
	c.StorageDefaults.QuarantineSuspicious = false
	s.settings.Store(&c)
	if status, reason := s.suspicious("report.exe", "application/octet-stream"); status != "ready" || reason == "" {
		t.Fatal(status, reason)
	}
	requireStatus(t, request(h, "GET", "/api/zip?ids=image", "", "user", nil), 400)
}
func TestComplianceRecoveryAndDailyBackup(t *testing.T) {
	s, h := complianceServer(t)
	id, hdr := reserve(t, h, "/api/uploads", 10)
	requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=0", "123", "user", hdr), 204)
	f, orphan, e := s.sm.PreparePartFile("orphan")
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	// Simulate durable finalization intent followed by rename, then restart before DB commit.
	record := models.FileRecord{ID: "recovered", PersonID: 1, UploaderName: "TestUser", OriginalName: "test.txt", StoredPath: s.sm.GetShardedPath("recovered"), Size: 3, ContentType: "text/plain", Status: "ready", CreatedAt: time.Now().UTC()}
	data, _ := json.Marshal(record)
	execSQL(t, s, "UPDATE uploads SET final_file_id=?,file_record=? WHERE id=?", record.ID, string(data), id)
	if _, e = s.sm.FinalizeUpload(id, record.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.recover(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(orphan); !os.IsNotExist(e) {
		t.Fatal("orphan retained")
	}
	if n := countSQL(t, s, "SELECT size FROM files WHERE id='recovered'"); n != 3 {
		t.Fatal(n)
	}
	if n := countSQL(t, s, "SELECT upload_completed_bytes FROM traffic_counters WHERE person_id=1"); n != 3 {
		t.Fatal(n)
	}
	if e = s.recover(); e != nil {
		t.Fatal(e)
	}
	if n := countSQL(t, s, "SELECT upload_completed_bytes FROM traffic_counters WHERE person_id=1"); n != 3 {
		t.Fatal("double accounting", n)
	}
	if e = s.backup(); e != nil {
		t.Fatal(e)
	}
	entries, _ := os.ReadDir(s.cfg.BackupDir)
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	info, _ := entries[0].Info()
	if e = s.backup(); e != nil {
		t.Fatal(e)
	}
	after, _ := os.Stat(filepath.Join(s.cfg.BackupDir, entries[0].Name()))
	if !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("daily backup rewritten")
	}
}
func TestComplianceRuntimePersistenceAndValidation(t *testing.T) {
	s, h := complianceServer(t)
	addAdmin(t, s)
	v := url.Values{"quota_gb": {"101"}, "upload_limit_gb": {"201"}, "download_limit_gb": {"301"}, "max_file_gb": {"51"}, "max_concurrent": {"2"}, "default_expiry": {"7"}, "upload_mbps": {"100"}, "download_mbps": {"200"}, "burst_mb": {"8"}, "zip_files": {"90"}, "zip_gb": {"40"}, "expiry_options": {"1,7,14,30"}, "suspicious": {"exe,js"}, "quarantine": {"on"}}
	requireStatus(t, formRequest(h, "/admin/action/settings_save", v, "admin"), 303)
	if s.current().StorageDefaults.QuotaBytes != 101<<30 {
		t.Fatal("not applied")
	}
	var raw string
	if e := s.db.QueryRow("SELECT value FROM settings WHERE key='runtime'").Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var rt RuntimeSettings
	if e := json.Unmarshal([]byte(raw), &rt); e != nil {
		t.Fatal(e)
	}
	c := config.DefaultConfig()
	rt.Apply(c)
	if e := c.ValidateRuntime(); e != nil {
		t.Fatal(e)
	}
	// Open a second server only after stopping the first, as real startup does.
	s.Close()
	restarted, e := NewServer(s.cfg, s.db)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if restarted.current().StorageDefaults.QuotaBytes != 101<<30 || restarted.current().SpeedLimits.ExternalUploadMbps != 100 {
		t.Fatal("settings lost")
	}
	v.Set("quota_gb", "NaN")
	requireStatus(t, formRequest(restarted.Routes(), "/admin/action/settings_save", v, "admin"), 400)
}
func TestComplianceMonthlyTrafficAndRetention(t *testing.T) {
	s, h := complianceServer(t)
	id, hdr := reserve(t, h, "/api/uploads", 10)
	// A local chunk followed by external completion remains local in the ledger.
	r := httptest.NewRequest("PATCH", "/api/uploads/"+id+"?offset=0", strings.NewReader("123"))
	r.RemoteAddr = "127.0.0.1:9999"
	r.Header.Set("X-Real-IP", "192.168.32.2")
	r.Header.Set("Authorization", "Bearer user")
	r.Header.Set("X-Upload-Secret", hdr["X-Upload-Secret"])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireStatus(t, w, 204)
	requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "user", hdr), 200)
	if n := countSQL(t, s, "SELECT local_upload_bytes FROM traffic_counters WHERE person_id=1"); n != 3 {
		t.Fatal(n)
	}
	if n := countSQL(t, s, "SELECT upload_completed_bytes FROM traffic_counters WHERE person_id=1"); n != 0 {
		t.Fatal(n)
	}
	old := monthStart(time.Now()).AddDate(0, -12, 0).Format("2006-01")
	keep := monthStart(time.Now()).AddDate(0, -11, 0).Format("2006-01")
	for _, m := range []string{old, keep} {
		execSQL(t, s, "INSERT INTO traffic_counters(person_id,month,updated_at) VALUES(1,?,?)", m, time.Now().UTC())
	}
	if e := s.cleanup(); e != nil {
		t.Fatal(e)
	}
	if n := countSQL(t, s, "SELECT count(*) FROM traffic_counters WHERE month=?", old); n != 0 {
		t.Fatal("old month retained")
	}
	if n := countSQL(t, s, "SELECT count(*) FROM traffic_counters WHERE month=?", keep); n != 1 {
		t.Fatal("12th month lost")
	}
	if traffic.GetCurrentMonth() != time.Now().Local().Format("2006-01") {
		t.Fatal("wrong month")
	}
}

func TestComplianceZIPLockCanActuallyBeReset(t *testing.T) {
	s, h := complianceServer(t)
	addFile(t, s, "ziplimit", "file.txt", "text/plain", false)
	c := *s.current()
	c.RateLimits.ZipHour = 1
	s.settings.Store(&c)
	requireStatus(t, request(h, "GET", "/api/zip?ids=ziplimit", "", "user", nil), 200)
	w := request(h, "GET", "/api/zip?ids=ziplimit", "", "user", nil)
	requireStatus(t, w, 429)
	var key string
	if e := s.db.QueryRow("SELECT key FROM rate_limit_locks").Scan(&key); e != nil {
		t.Fatal(e)
	}
	if key != "zip:1:h" {
		t.Fatal("wrong rule locked", key)
	}
	if e := s.rateLimiter.Unlock(key); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(h, "GET", "/api/zip?ids=ziplimit", "", "user", nil), 200)
}
func TestComplianceExpiredSessionDoesNotCascadeActiveUpload(t *testing.T) {
	s, h := complianceServer(t)
	id, _ := reserve(t, h, "/api/uploads", 10)
	execSQL(t, s, "UPDATE device_sessions SET idle_expires_at=? WHERE person_id=1", time.Now().UTC().Add(-time.Second))
	if e := s.cleanup(); e != nil {
		t.Fatal(e)
	}
	if n := countSQL(t, s, "SELECT count(*) FROM uploads WHERE id=? AND session_id IS NULL", id); n != 1 {
		t.Fatal("reservation cascaded", n)
	}
	execSQL(t, s, "UPDATE uploads SET reservation_expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Second), id)
	if e := s.cleanup(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.sm.GetPartPath(id)); !os.IsNotExist(e) {
		t.Fatal("expired part remains")
	}
}
func TestComplianceCompleteIntentRetryAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			s, h := complianceServer(t)
			id, hdr := reserve(t, h, "/api/uploads", 10)
			requireStatus(t, request(h, "PATCH", "/api/uploads/"+id+"?offset=0", "123", "user", hdr), 204)
			record := models.FileRecord{ID: "intentfile", PersonID: 1, UploaderName: "Owner", OriginalName: "x.txt", StoredPath: s.sm.GetShardedPath("intentfile"), Size: 3, ContentType: "text/plain", Status: "ready", CreatedAt: time.Now().UTC()}
			b, _ := json.Marshal(record)
			execSQL(t, s, "UPDATE uploads SET final_file_id=?,file_record=? WHERE id=?", record.ID, string(b), id)
			if _, e := s.sm.FinalizeUpload(id, record.ID); e != nil {
				t.Fatal(e)
			}
			if cancel {
				requireStatus(t, request(h, "DELETE", "/api/uploads/"+id, "", "user", hdr), 204)
				if _, e := os.Stat(record.StoredPath); !os.IsNotExist(e) {
					t.Fatal("renamed bytes leaked")
				}
			} else {
				requireStatus(t, request(h, "POST", "/api/uploads/"+id+"/complete", "", "user", hdr), 200)
				if n := countSQL(t, s, "SELECT count(*) FROM files WHERE id=?", record.ID); n != 1 {
					t.Fatal(n)
				}
			}
		})
	}
}
func TestComplianceQuarantineOtherUserAndProtected(t *testing.T) {
	s, h := complianceServer(t)
	addFile(t, s, "private", "x.txt", "text/plain", false)
	execSQL(t, s, "UPDATE files SET person_id=0,status='quarantined' WHERE id='private'")
	requireStatus(t, request(h, "GET", "/download/private", "", "user", nil), 404)
	w := request(h, "GET", "/api/files", "", "user", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("foreign quarantine visible")
	}
	execSQL(t, s, "UPDATE files SET person_id=1,status='ready',protected=1 WHERE id='private'")
	requireStatus(t, formRequest(h, "/files/delete/private", url.Values{}, "user"), 403)
}

func TestComplianceMandatoryTOTPAndReplay(t *testing.T) {
	s, h := complianceServer(t)
	password := "Unique strong test pass 529!"
	hash, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	secret, e := auth.GenerateTOTPSecret()
	if e != nil {
		t.Fatal(e)
	}
	execSQL(t, s, "INSERT INTO admin_users(username,password_hash,totp_secret,totp_enabled,created_at) VALUES('secure',?,?,0,?)", hash, secret, time.Now().UTC())
	payload := map[string]string{"username": "secure", "password": password}
	b, _ := json.Marshal(payload)
	requireStatus(t, request(h, "POST", "/api/auth/login", string(b), "", nil), 401)
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	payload["totp_code"] = fmt.Sprintf("%06d", code%1000000)
	b, _ = json.Marshal(payload)
	w := request(h, "POST", "/api/auth/login", string(b), "", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "token") {
		t.Fatal("token in response")
	}
	cookies := w.Result().Cookies()
	has := false
	for _, c := range cookies {
		if c.Name == "homeshare_session" {
			has = c.HttpOnly && c.SameSite == http.SameSiteLaxMode
		}
	}
	if !has {
		t.Fatal("session cookie missing")
	}
	requireStatus(t, request(h, "POST", "/api/auth/login", string(b), "", nil), 401)
}
