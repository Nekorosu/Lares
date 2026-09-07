package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/models"
	"lares/internal/netutils"
	"lares/internal/ratelimit"
	"lares/internal/securitylog"
	"lares/internal/speedlimit"
	"lares/internal/storage"
	"lares/web"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	cfg           *config.Config
	db            *sql.DB
	sm            *storage.StorageManager
	netChecker    *netutils.NetworkChecker
	rateLimiter   *ratelimit.RateLimiter
	speedLimit    *speedlimit.SpeedLimiter
	securityLog   *securitylog.Logger
	settings      atomic.Pointer[config.Config]
	templates     *template.Template
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	activeUploads sync.Map
	lifeMu        sync.Mutex
	uploadMu      [256]sync.Mutex
}
type identity struct {
	Session models.DeviceSession
	Person  *models.Person
	Admin   *models.AdminUser
}
type identityKey struct{}

func NewServer(c *config.Config, db *sql.DB) (*Server, error) {
	sm, e := storage.NewStorageManager(c.DataDir, c.TmpDir, c.DiskReserve.MinFreeSpaceGB, c.DiskReserve.CriticalFreeSpaceGB, c.DiskReserve.MinFreeInodes)
	if e != nil {
		return nil, e
	}
	nc, e := netutils.NewNetworkChecker(c.LocalCIDR)
	if e != nil {
		sm.Close()
		return nil, e
	}
	sec, e := securitylog.NewLogger(c.SecurityLog)
	if e != nil {
		sm.Close()
		return nil, e
	}
	funcs := template.FuncMap{"gib": func(n int64) string { return strconv.FormatFloat(float64(n)/(1<<30), 'f', -1, 64) }, "join": func(v []string) string { return strings.Join(v, ",") }, "joinints": func(v []int) string {
		ss := []string{}
		for _, n := range v {
			ss = append(ss, strconv.Itoa(n))
		}
		return strings.Join(ss, ",")
	}, "bytes": formatBytes, "date": dateString, "add": func(a, b int) int { return a + b }, "eqs": func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) }}
	ts, e := template.New("pages").Funcs(funcs).ParseFS(web.EmbeddedFS, "templates/*.html")
	if e != nil {
		sm.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: c, db: db, sm: sm, netChecker: nc, rateLimiter: ratelimit.NewRateLimiter(db), speedLimit: speedlimit.NewSpeedLimiter(c.SpeedLimits.ExternalUploadMbps, c.SpeedLimits.ExternalDownloadMbps, c.SpeedLimits.BurstMB), securityLog: sec, templates: ts, ctx: ctx, cancel: cancel}
	v := *c
	var raw string
	e = db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='runtime'").Scan(&raw)
	if e != nil && e != sql.ErrNoRows {
		cancel()
		sm.Close()
		return nil, e
	}
	if e == nil {
		var runtime RuntimeSettings
		if e = json.Unmarshal([]byte(raw), &runtime); e != nil {
			cancel()
			sm.Close()
			return nil, e
		}
		runtime.Apply(&v)
		if e = v.ValidateRuntime(); e != nil {
			cancel()
			sm.Close()
			return nil, e
		}
	}
	s.settings.Store(&v)
	s.speedLimit.UpdateLimits(v.SpeedLimits.ExternalUploadMbps, v.SpeedLimits.ExternalDownloadMbps, v.SpeedLimits.BurstMB)
	if e = s.recover(); e != nil {
		cancel()
		sm.Close()
		return nil, fmt.Errorf("восстановление: %w", e)
	}
	s.wg.Add(1)
	go s.background()
	return s, nil
}
func (s *Server) Close()                  { s.cancel(); s.wg.Wait(); s.sm.Close() }
func (s *Server) SetConfigPath(string)    {} // Runtime settings are stored in SQLite, never YAML.
func (s *Server) current() *config.Config { return s.settings.Load() }
func (s *Server) lockUpload(id string) *sync.Mutex {
	var n byte
	for i := range id {
		n = n*31 + id[i]
	}
	return &s.uploadMu[n]
}
func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d Б", n)
	}
	v := float64(n)
	units := []string{"Б", "КиБ", "МиБ", "ГиБ", "ТиБ", "ПиБ"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
func dateString(v any) string {
	switch t := v.(type) {
	case time.Time:
		return t.Local().Format("02.01.2006 15:04")
	case *time.Time:
		if t != nil {
			return dateString(*t)
		}
	case string:
		for _, f := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05"} {
			if d, e := time.Parse(f, t); e == nil {
				return dateString(d)
			}
		}
		return t
	}
	return "—"
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		jsonOut(w, status, map[string]string{"error": msg})
	} else {
		http.Error(w, msg, status)
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return errors.New("лишние данные JSON")
	}
	return nil
}
func (s *Server) Routes() http.Handler {
	m := http.NewServeMux()
	static, _ := fs.Sub(web.EmbeddedFS, "static")
	m.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	m.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "User-agent: *\nDisallow: /\n")
	})
	m.HandleFunc("GET /login", s.loginPage)
	m.HandleFunc("POST /login", s.inviteLogin)
	m.HandleFunc("GET /admin/login", s.loginPage)
	m.HandleFunc("POST /admin/login", s.adminLogin)
	m.HandleFunc("POST /api/auth/login", s.adminLogin)
	m.HandleFunc("POST /api/auth/invite/activate", s.inviteLogin)
	m.HandleFunc("GET /api/auth/me", s.authMe)
	m.HandleFunc("POST /logout", s.protect(false, s.logout))
	m.HandleFunc("GET /{$}", s.protect(false, s.home))
	m.HandleFunc("GET /devices", s.protect(false, s.devices))
	m.HandleFunc("POST /devices/revoke", s.protect(false, s.revokeDevice))
	m.HandleFunc("POST /api/uploads", s.protect(false, s.createUpload))
	m.HandleFunc("POST /api/files/upload/reserve", s.protect(false, s.createUpload))
	m.HandleFunc("POST /api/files/upload/direct", s.protect(false, func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, 410, "Используйте возобновляемую загрузку /api/uploads")
	}))
	for _, method := range []string{"HEAD", "PATCH", "DELETE"} {
		m.HandleFunc(method+" /api/uploads/{id}", s.protect(false, s.uploadAction))
	}
	m.HandleFunc("POST /api/uploads/{id}/complete", s.protect(false, s.uploadAction))
	m.HandleFunc("GET /api/uploads", s.protect(false, s.listMyUploads))
	m.HandleFunc("GET /download/{id}", s.protect(false, s.download))
	m.HandleFunc("GET /api/files/download/{id}", s.protect(false, s.download))
	m.HandleFunc("GET /preview/{id}", s.protect(false, s.download))
	m.HandleFunc("GET /api/zip", s.protect(false, s.zipDownload))
	m.HandleFunc("POST /files/delete/{id}", s.protect(false, s.deleteFileHandler))
	m.HandleFunc("GET /api/files", s.protect(false, s.filesAPI))
	m.HandleFunc("GET /api/stats", s.protect(false, s.statsAPI))
	m.HandleFunc("GET /admin", s.protect(true, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/dashboard", 303) }))
	for _, page := range []string{"dashboard", "people", "invites", "sessions", "files", "uploads", "quarantine", "traffic", "audit", "settings"} {
		m.HandleFunc("GET /admin/"+page, s.protect(true, s.adminPage))
	}
	m.HandleFunc("POST /admin/action/{action}", s.protect(true, s.adminAction))
	// Removed duplicate handlers have no mutating fallback.
	m.HandleFunc("/api/files/upload/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, 410, "Этот протокол заменён на /api/uploads")
	})
	return s.security(m)
}
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			s.generateCSRFToken(w, r)
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(60 * time.Second))
			defer http.NewResponseController(w).SetReadDeadline(time.Time{})
			// Bearer bypass only when no cookie is present; an invalid Bearer never authorizes a cookie mutation.
			_, cookieErr := r.Cookie("homeshare_session")
			bearer := cookieErr == http.ErrNoCookie && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !bearer && !s.validateCSRFToken(r) {
				fail(w, r, 403, "Обновите страницу: защитный код запроса отсутствует или устарел")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) generateCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if c, e := r.Cookie("homeshare_csrf"); e == nil && len(c.Value) == 64 {
		return c.Value
	}
	token := auth.GenerateRandomToken(32)
	http.SetCookie(w, &http.Cookie{Name: "homeshare_csrf", Value: token, Path: "/", Secure: netutils.IsHTTPS(r), SameSite: http.SameSiteLaxMode})
	return token
}
func (s *Server) validateCSRFToken(r *http.Request) bool {
	v := r.Header.Get("X-CSRF-Token")
	if v == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
		if r.ParseForm() != nil {
			return false
		}
		v = r.PostForm.Get("csrf_token")
	}
	c, e := r.Cookie("homeshare_csrf")
	return e == nil && v != "" && subtle.ConstantTimeCompare([]byte(v), []byte(c.Value)) == 1
}
func (s *Server) getSession(r *http.Request) (*models.DeviceSession, *models.Person, *models.AdminUser) {
	if id, ok := r.Context().Value(identityKey{}).(*identity); ok {
		return &id.Session, id.Person, id.Admin
	}
	token := ""
	if c, e := r.Cookie("homeshare_session"); e == nil {
		token = c.Value
	} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if token == "" {
		return nil, nil, nil
	}
	var sess models.DeviceSession
	var pid, aid sql.NullInt64
	var abs sql.NullTime
	now := time.Now().UTC()
	err := s.db.QueryRowContext(r.Context(), `SELECT id,person_id,admin_id,is_admin,name,idle_expires_at,absolute_expires_at,created_at,last_used_at FROM device_sessions WHERE session_token_hash=? AND revoked=0`, auth.HashWithSalt(token, s.cfg.Secrets.SessionSecret)).Scan(&sess.ID, &pid, &aid, &sess.IsAdmin, &sess.Name, &sess.IdleExpiresAt, &abs, &sess.CreatedAt, &sess.LastUsedAt)
	if err != nil || !sess.IdleExpiresAt.After(now) || (abs.Valid && !abs.Time.After(now)) {
		return nil, nil, nil
	}
	if pid.Valid {
		sess.PersonID = &pid.Int64
	}
	if aid.Valid {
		sess.AdminID = &aid.Int64
	}
	if abs.Valid {
		sess.AbsoluteExpiresAt = &abs.Time
	}
	var p *models.Person
	var a *models.AdminUser
	idle := time.Duration(s.cfg.SessionDefaults.AdminIdleHours) * time.Hour
	if sess.IsAdmin {
		if !aid.Valid || !abs.Valid {
			return nil, nil, nil
		}
		hardExpiry := sess.CreatedAt.Add(time.Duration(s.cfg.SessionDefaults.AdminAbsoluteDays) * 24 * time.Hour)
		if !hardExpiry.After(now) || !sess.LastUsedAt.Add(idle).After(now) {
			return nil, nil, nil
		}
		if abs.Time.After(hardExpiry) {
			abs.Time = hardExpiry
		}
		a = &models.AdminUser{}
		if s.db.QueryRowContext(r.Context(), "SELECT id,username FROM admin_users WHERE id=?", aid.Int64).Scan(&a.ID, &a.Username) != nil {
			return nil, nil, nil
		}
	} else {
		if !pid.Valid {
			return nil, nil, nil
		}
		p, err = s.person(r.Context(), pid.Int64)
		if err != nil || !p.Enabled {
			return nil, nil, nil
		}
		idle = time.Duration(p.SessionIdleDays) * 24 * time.Hour
	}
	if idle <= 0 || !sess.LastUsedAt.Add(idle).After(now) {
		return nil, nil, nil
	}
	expiry := now.Add(idle)
	if abs.Valid && expiry.After(abs.Time) {
		expiry = abs.Time
	}
	result, err := s.db.ExecContext(r.Context(), "UPDATE device_sessions SET last_used_at=?,idle_expires_at=?,last_ip_hash=?,last_user_agent_hash=? WHERE id=? AND revoked=0", now, expiry, auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt), auth.HashWithSalt(r.UserAgent(), s.cfg.Secrets.IPHashSalt), sess.ID)
	if err != nil {
		return nil, nil, nil
	}
	affected, e := result.RowsAffected()
	if e != nil || affected != 1 {
		return nil, nil, nil
	}
	if p != nil {
		if _, err = s.db.ExecContext(r.Context(), "UPDATE people SET last_activity_at=? WHERE id=?", now, p.ID); err != nil {
			return nil, nil, nil
		}
	}
	return &sess, p, a
}
func (s *Server) person(ctx context.Context, id int64) (*models.Person, error) {
	p := &models.Person{}
	e := s.db.QueryRowContext(ctx, `SELECT id,label,notes,enabled,storage_quota_bytes,monthly_upload_limit_bytes,monthly_download_limit_bytes,max_file_size_bytes,max_concurrent_uploads,allow_user_keep_forever,session_idle_days,session_absolute_days,ignore_traffic_quota FROM people WHERE id=? AND is_orphan=0`, id).Scan(&p.ID, &p.Label, &p.Notes, &p.Enabled, &p.StorageQuotaBytes, &p.MonthlyUploadLimitBytes, &p.MonthlyDownloadLimit, &p.MaxFileSizeBytes, &p.MaxConcurrentUploads, &p.AllowUserKeepForever, &p.SessionIdleDays, &p.SessionAbsoluteDays, &p.IgnoreTrafficQuota)
	return p, e
}
func (s *Server) protect(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, p, a := s.getSession(r)
		if sess == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				fail(w, r, 401, "Войдите в систему")
			} else {
				target := "/login"
				if admin {
					target = "/admin/login"
				}
				http.Redirect(w, r, target, 303)
			}
			return
		}
		if admin && a == nil {
			fail(w, r, 403, "Требуется администратор")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), identityKey{}, &identity{*sess, p, a}))
		if a != nil && !s.requestLimit(w, r, "admin") {
			return
		}
		next(w, r)
	}
}
func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["CSRF"] = s.generateCSRFToken(w, r)
	data["Page"] = page
	data["Config"] = s.current()
	_, p, a := s.getSession(r)
	data["Person"] = p
	data["Admin"] = a
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var rendered bytes.Buffer
	if e := s.templates.ExecuteTemplate(&rendered, page, data); e != nil {
		log.Printf("template %s: %v", page, e)
		http.Error(w, "Ошибка отображения страницы", 500)
		return
	}
	_, _ = w.Write(rendered.Bytes())
}
func (s *Server) audit(r *http.Request, event, entity, id, details string) error {
	actor := "system"
	var aid int64
	if ident, ok := r.Context().Value(identityKey{}).(*identity); ok {
		if ident.Admin != nil {
			actor = "admin"
			aid = ident.Admin.ID
		} else if ident.Person != nil {
			actor = "person"
			aid = ident.Person.ID
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancel()
	_, e := s.db.ExecContext(ctx, "INSERT INTO audit_logs(time,actor_type,actor_id,event,entity_type,entity_id,ip_hash,details) VALUES(?,?,?,?,?,?,?,?)", time.Now().UTC(), actor, aid, event, entity, id, auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt), details)
	return e
}
func (s *Server) requestLimit(w http.ResponseWriter, r *http.Request, kind string) bool {
	local := s.netChecker.IsLocal(r)
	cfg := s.current()
	if local && !cfg.RateLimits.EnforceLocal {
		return true
	}
	sess, p, a := s.getSession(r)
	if sess == nil {
		return false
	}
	var id int64
	if p != nil {
		id = p.ID
	} else if a != nil {
		id = a.ID
	}
	ip := auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt)
	key := kind + ":" + strconv.FormatInt(id, 10)
	if a != nil {
		key = "request:" + kind + ":admin:" + strconv.FormatInt(id, 10)
	}
	rl := cfg.RateLimits
	var rules []ratelimit.Rule
	switch kind {
	case "create":
		rules = []ratelimit.Rule{{Key: key, Count: rl.UploadPersonHour, Window: time.Hour}, {Key: kind + ":ip:" + ip, Count: rl.UploadIPHour, Window: time.Hour}}
		if local {
			rules = []ratelimit.Rule{{Key: key, Count: 100, Window: time.Hour}}
		}
	case "chunk":
		n := rl.ChunkMinute
		if local {
			n = 600
		}
		rules = []ratelimit.Rule{{Key: key, Count: n, Window: time.Minute}}
	case "download":
		rules = []ratelimit.Rule{{Key: key, Count: rl.DownloadPersonMinute, Window: time.Minute}, {Key: kind + ":ip:" + ip, Count: rl.DownloadIPMinute, Window: time.Minute}}
		if local {
			rules = []ratelimit.Rule{{Key: key, Count: 600, Window: time.Minute}}
		}
	case "zip":
		rules = []ratelimit.Rule{{Key: key + ":h", Count: rl.ZipHour, Window: time.Hour}, {Key: key + ":d", Count: rl.ZipDay, Window: 24 * time.Hour}}
		if local {
			rules = []ratelimit.Rule{{Key: key, Count: 20, Window: time.Hour}}
		}
	case "list":
		n := rl.ListMinute
		if local {
			n = 600
		}
		rules = []ratelimit.Rule{{Key: key, Count: n, Window: time.Minute}}
	case "admin":
		n := rl.AdminMinute
		if local {
			n = 600
		}
		rules = []ratelimit.Rule{{Key: key, Count: n, Window: time.Minute}}
	}
	if locked, d, _ := s.rateLimiter.IsLocked(key); locked {
		s.limited(w, r, key, d)
		return false
	}
	for _, v := range rules {
		if locked, d, _ := s.rateLimiter.IsLocked(v.Key); locked {
			s.limited(w, r, v.Key, d)
			return false
		}
	}
	ok, d, denied, e := s.rateLimiter.AllowDetailed(r.Context(), rules...)
	if e != nil {
		fail(w, r, 503, "Не удалось проверить ограничение запросов")
		return false
	}
	if !ok {
		key = denied
		if e = s.rateLimiter.Lock(key, "requests", "Частые запросы", d); e != nil {
			fail(w, r, 503, "Ошибка блокировки")
			return false
		}
		s.limited(w, r, key, d)
		return false
	}
	return true
}
func (s *Server) limited(w http.ResponseWriter, r *http.Request, key string, d time.Duration) {
	s.securityLog.LogEvent("rate_limited", netutils.GetClientIP(r), "ограничение запросов")
	if e := s.audit(r, "rate_limited", "lock", key, "Временная блокировка"); e != nil {
		log.Printf("audit: %v", e)
	}
	w.Header().Set("Retry-After", fmt.Sprint(max(1, int(d.Seconds())+1)))
	fail(w, r, 429, "Слишком много запросов. Повторите позже")
}

func (s *Server) recordAudit(r *http.Request, event, entity, id, details string) {
	if e := s.audit(r, event, entity, id, details); e != nil {
		s.logError(e)
	}
}
func (s *Server) systemAudit(event, entity, id, details string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 10*time.Second)
	defer cancel()
	if _, e := s.db.ExecContext(ctx, "INSERT INTO audit_logs(time,actor_type,actor_id,event,entity_type,entity_id,ip_hash,details) VALUES(?,'system',0,?,?,?,'',?)", time.Now().UTC(), event, entity, id, details); e != nil {
		s.logError(e)
	}
}
