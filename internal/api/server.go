package api

import (
	"archive/zip"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lares/internal/audit"
	"lares/internal/auth"
	"lares/internal/cleanup"
	"lares/internal/config"
	"lares/internal/models"
	"lares/internal/netutils"
	"lares/internal/ratelimit"
	"lares/internal/securitylog"
	"lares/internal/speedlimit"
	"lares/internal/storage"
	"lares/internal/traffic"
	"lares/web"
)


type Server struct {
	cfg         *config.Config
	configPath  string
	db          *sql.DB
	sm          *storage.StorageManager
	tm          *traffic.Manager
	netChecker  *netutils.NetworkChecker
	rateLimiter *ratelimit.RateLimiter
	speedLimit  *speedlimit.SpeedLimiter
	auditLog    *audit.Logger
	securityLog *securitylog.Logger
	cleaner     *cleanup.Worker
	templates   map[string]*template.Template
}

func parseTemplates() (map[string]*template.Template, error) {
	pages := []string{
		"login.html",
		"admin_login.html",
		"user_dashboard.html",
		"admin_dashboard.html",
		"admin_people.html",
		"admin_invites.html",
		"admin_sessions.html",
		"admin_files.html",
		"admin_quarantine.html",
		"admin_traffic.html",
		"admin_settings.html",
	}

	tmplMap := make(map[string]*template.Template)
	for _, page := range pages {
		t, err := template.ParseFS(web.EmbeddedFS, "templates/layout.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
		}
		tmplMap[page] = t
	}
	return tmplMap, nil
}

func NewServer(cfg *config.Config, db *sql.DB) (*Server, error) {
	sm, err := storage.NewStorageManager(
		cfg.DataDir, cfg.TmpDir,
		cfg.DiskReserve.MinFreeSpaceGB,
		cfg.DiskReserve.CriticalFreeSpaceGB,
		cfg.DiskReserve.MinFreeInodes,
	)
	if err != nil {
		return nil, err
	}

	netChecker, err := netutils.NewNetworkChecker(cfg.LocalCIDR)
	if err != nil {
		return nil, err
	}

	secLogger, err := securitylog.NewLogger(cfg.SecurityLog)
	if err != nil {
		log.Printf("[Warning] Failed to initialize security logger: %v", err)
	}

	tm := traffic.NewManager(db)
	rl := ratelimit.NewRateLimiter(db)
	sl := speedlimit.NewSpeedLimiter(
		cfg.SpeedLimits.ExternalUploadMbps,
		cfg.SpeedLimits.ExternalDownloadMbps,
		cfg.SpeedLimits.BurstMB,
	)
	al := audit.NewLogger(db, cfg.Secrets.IPHashSalt)
	cleaner := cleanup.NewWorker(db, sm, tm, cfg.BackupDir, cfg.SecurityLog)

	tmplMap, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML templates: %w", err)
	}

	srv := &Server{
		cfg:         cfg,
		configPath:  "config.yaml",
		db:          db,
		sm:          sm,
		tm:          tm,
		netChecker:  netChecker,
		rateLimiter: rl,
		speedLimit:  sl,
		auditLog:    al,
		securityLog: secLogger,
		cleaner:     cleaner,
		templates:   tmplMap,
	}

	cleaner.StartBackgroundJobs()
	return srv, nil
}

func (s *Server) SetConfigPath(path string) {
	if path != "" {
		s.configPath = path
	}
}

func (s *Server) renderTemplate(w http.ResponseWriter, pageName string, data interface{}) {
	tmpl, ok := s.templates[pageName]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("[Template Error] %v", err)
	}
}


func findDistDir() string {
	candidates := []string{
		"./dist",
		"../dist",
		"/srv/media/tmp/Lares/dist",
		"/var/lib/homeshare/dist",
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				return dir
			}
		}
	}
	return ""
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	distDir := findDistDir()
	if distDir != "" {
		mux.Handle("/assets/", http.FileServer(http.Dir(distDir)))
	}

	// Static files
	mux.Handle("/static/", http.FileServer(http.FS(web.EmbeddedFS)))

	// JSON API routes for React SPA & API Clients
	mux.HandleFunc("/api/auth/login", s.handleAPIAuthLogin)
	mux.HandleFunc("/api/auth/me", s.handleAPIAuthMe)
	mux.HandleFunc("/api/stats", s.handleAPIStats)
	mux.HandleFunc("/api/files", s.handleAPIFiles)
	mux.HandleFunc("/api/admin/invites", s.handleAPIAdminInvites)
	mux.HandleFunc("/api/admin/sessions", s.handleAPIAdminSessions)
	mux.HandleFunc("/api/admin/sessions/", s.handleAPIAdminSessions)
	mux.HandleFunc("/api/admin/quarantine/", s.handleAPIAdminQuarantineApprove)

	// Auth routes
	mux.HandleFunc("/login", s.handleUserLogin)
	mux.HandleFunc("/admin/login", s.handleAdminLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// User dashboard & file operations
	mux.HandleFunc("/", s.handleUserDashboard)
	mux.HandleFunc("/download/", s.handleDownloadFile)
	mux.HandleFunc("/preview/", s.handlePreviewFile)
	mux.HandleFunc("/files/delete/", s.handleUserDeleteFile)
	mux.HandleFunc("/api/zip", s.handleZipDownload)

	// Chunked Upload Protocol API
	mux.HandleFunc("/api/uploads", s.handleUploadCreate)
	mux.HandleFunc("/api/uploads/", s.handleUploadChunk) // Handles HEAD, PATCH, DELETE, POST complete

	// Admin Dashboard & Pages
	mux.HandleFunc("/admin/dashboard", s.requireAdmin(s.handleAdminDashboard))
	mux.HandleFunc("/admin/people", s.requireAdmin(s.handleAdminPeople))
	mux.HandleFunc("/admin/people/create", s.requireAdmin(s.handleAdminPeopleCreate))
	mux.HandleFunc("/admin/people/disable/", s.requireAdmin(s.handleAdminPeopleDisable))
	mux.HandleFunc("/admin/people/enable/", s.requireAdmin(s.handleAdminPeopleEnable))
	mux.HandleFunc("/admin/people/delete/", s.requireAdmin(s.handleAdminPeopleDelete))
	mux.HandleFunc("/admin/invites", s.requireAdmin(s.handleAdminInvites))
	mux.HandleFunc("/admin/invites/create", s.requireAdmin(s.handleAdminInvitesCreate))
	mux.HandleFunc("/admin/invites/revoke/", s.requireAdmin(s.handleAdminInvitesRevoke))
	mux.HandleFunc("/admin/sessions", s.requireAdmin(s.handleAdminSessions))
	mux.HandleFunc("/admin/sessions/revoke/", s.requireAdmin(s.handleAdminSessionsRevoke))
	mux.HandleFunc("/admin/sessions/revoke-all/", s.requireAdmin(s.handleAdminSessionsRevokeAll))
	mux.HandleFunc("/admin/files", s.requireAdmin(s.handleAdminFiles))
	mux.HandleFunc("/admin/files/delete/", s.requireAdmin(s.handleAdminFilesDelete))
	mux.HandleFunc("/admin/files/toggle-protected/", s.requireAdmin(s.handleAdminFilesToggleProtected))
	mux.HandleFunc("/admin/files/toggle-forever/", s.requireAdmin(s.handleAdminFilesToggleForever))
	mux.HandleFunc("/admin/quarantine", s.requireAdmin(s.handleAdminQuarantine))
	mux.HandleFunc("/admin/quarantine/approve/", s.requireAdmin(s.handleAdminQuarantineApprove))
	mux.HandleFunc("/admin/traffic", s.requireAdmin(s.handleAdminTraffic))
	mux.HandleFunc("/admin/traffic/reset/", s.requireAdmin(s.handleAdminTrafficReset))
	mux.HandleFunc("/admin/settings", s.requireAdmin(s.handleAdminSettings))
	mux.HandleFunc("/admin/settings/save", s.requireAdmin(s.handleAdminSettingsSave))
	mux.HandleFunc("/admin/locks/clear/", s.requireAdmin(s.handleAdminLocksClear))
	mux.HandleFunc("/admin/locks/clear-all", s.requireAdmin(s.handleAdminLocksClearAll))

	return s.applySecurityHeaders(mux)
}

func (s *Server) applySecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none';")
		next.ServeHTTP(w, r)
	})
}

// Session resolution middleware helper
func (s *Server) getSession(r *http.Request) (*models.DeviceSession, *models.Person, *models.AdminUser) {
	var tokenStr string
	cookie, err := r.Cookie("homeshare_session")
	if err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	if tokenStr == "" {
		return nil, nil, nil
	}

	tokenHash := auth.HashWithSalt(tokenStr, s.cfg.Secrets.SessionSecret)
	now := time.Now().UTC()

	var sess models.DeviceSession
	var p models.Person
	var admin models.AdminUser

	err = s.db.QueryRow(`
		SELECT id, person_id, admin_id, is_admin, name, idle_expires_at, absolute_expires_at, revoked
		FROM device_sessions
		WHERE session_token_hash = ? AND revoked = 0
	`, tokenHash).Scan(&sess.ID, &sess.PersonID, &sess.AdminID, &sess.IsAdmin, &sess.Name, &sess.IdleExpiresAt, &sess.AbsoluteExpiresAt, &sess.Revoked)

	if err != nil {
		return nil, nil, nil
	}

	// Check session expiry
	if now.After(sess.IdleExpiresAt) || (sess.AbsoluteExpiresAt != nil && now.After(*sess.AbsoluteExpiresAt)) {
		_, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", sess.ID)
		return nil, nil, nil
	}

	// Touch last_used_at and update idle_expires_at
	var idleDays int = 30
	if sess.IsAdmin {
		idleDays = 1
	}
	newIdle := now.Add(time.Duration(idleDays) * 24 * time.Hour)
	_, _ = s.db.Exec("UPDATE device_sessions SET last_used_at = ?, idle_expires_at = ? WHERE id = ?", now, newIdle, sess.ID)

	if sess.IsAdmin && sess.AdminID != nil {
		err = s.db.QueryRow("SELECT id, username FROM admin_users WHERE id = ?", *sess.AdminID).Scan(&admin.ID, &admin.Username)
		if err == nil {
			return &sess, nil, &admin
		}
	}

	err = s.db.QueryRow(`
		SELECT id, label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, allow_user_keep_forever, session_idle_days, session_absolute_days, ignore_traffic_quota
		FROM people WHERE id = ? AND enabled = 1
	`, sess.PersonID).Scan(
		&p.ID, &p.Label, &p.Notes, &p.Enabled, &p.StorageQuotaBytes, &p.MonthlyUploadLimitBytes,
		&p.MonthlyDownloadLimit, &p.MaxFileSizeBytes, &p.MaxConcurrentUploads, &p.AllowUserKeepForever,
		&p.SessionIdleDays, &p.SessionAbsoluteDays, &p.IgnoreTrafficQuota,
	)

	if err != nil {
		return nil, nil, nil
	}

	return &sess, &p, nil
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, _, admin := s.getSession(r)
		if sess == nil || !sess.IsAdmin || admin == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized: Admin access required"})
				return
			}
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// User Invite Login
func (s *Server) handleUserLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := netutils.GetClientIP(r)

	// Rate limit check on invite activation
	if locked, remaining, reason := s.rateLimiter.IsLocked("invite_lock_" + clientIP); locked {
		s.securityLog.LogEvent("invite_failed", clientIP, "ip rate locked: "+reason)
		ratelimit.SetRetryAfterHeader(w, int(remaining.Seconds()))
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Ошибка доступа", "Error": fmt.Sprintf("Доступ временно заблокирован: %s", reason),
		})
		return
	}

	if r.Method == "GET" {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Активация инвайта", "Page": "login", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	code := strings.TrimSpace(r.FormValue("invite_code"))
	deviceName := strings.TrimSpace(r.FormValue("device_name"))
	if deviceName == "" {
		deviceName = "Браузер " + r.UserAgent()
		if len(deviceName) > 50 {
			deviceName = deviceName[:50]
		}
	}

	codeHash := auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt)
	now := time.Now().UTC()

	var inv models.InviteCode
	var person models.Person
	err := s.db.QueryRow(`
		SELECT id, person_id, enabled, max_activations, activations_used, expires_at
		FROM invite_codes WHERE code_hash = ?
	`, codeHash).Scan(&inv.ID, &inv.PersonID, &inv.Enabled, &inv.MaxActivations, &inv.ActivationsUsed, &inv.ExpiresAt)

	if err != nil || !inv.Enabled || inv.ActivationsUsed >= inv.MaxActivations || now.After(inv.ExpiresAt) {
		s.securityLog.LogEvent("invite_failed", clientIP, "invalid or expired code")
		s.rateLimiter.Lock("invite_lock_"+clientIP, "invite_failed", "Неверный или просроченный инвайт-код", 15*time.Minute)

		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Ошибка входа", "Error": "Неверный, использованный или просроченный инвайт-код", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	// Fetch Person
	err = s.db.QueryRow("SELECT id, session_idle_days, session_absolute_days FROM people WHERE id = ? AND enabled = 1", inv.PersonID).Scan(&person.ID, &person.SessionIdleDays, &person.SessionAbsoluteDays)
	if err != nil {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Ошибка", "Error": "Пользователь заблокирован администратором", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	// Increment invite activation
	_, _ = s.db.Exec("UPDATE invite_codes SET activations_used = activations_used + 1 WHERE id = ?", inv.ID)

	// Create DeviceSession
	token := auth.GenerateRandomToken(32)
	tokenHash := auth.HashWithSalt(token, s.cfg.Secrets.SessionSecret)

	idleExpires := now.Add(time.Duration(person.SessionIdleDays) * 24 * time.Hour)
	var absExpires *time.Time
	if person.SessionAbsoluteDays > 0 {
		t := now.Add(time.Duration(person.SessionAbsoluteDays) * 24 * time.Hour)
		absExpires = &t
	}

	ipHash := auth.HashWithSalt(clientIP, s.cfg.Secrets.IPHashSalt)
	uaHash := auth.HashWithSalt(r.UserAgent(), s.cfg.Secrets.IPHashSalt)

	_, err = s.db.Exec(`
		INSERT INTO device_sessions (person_id, name, session_token_hash, created_at, last_used_at, last_ip_hash, last_user_agent_hash, idle_expires_at, absolute_expires_at, revoked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`, person.ID, deviceName, tokenHash, now, now, ipHash, uaHash, idleExpires, absExpires)

	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	s.auditLog.Log("person", person.ID, "invite_activated", "invite_code", fmt.Sprintf("%d", inv.ID), clientIP, "Device session created")

	http.SetCookie(w, &http.Cookie{
		Name:     "homeshare_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Admin Login Handler
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := netutils.GetClientIP(r)

	if r.Method == "GET" {
		s.renderTemplate(w, "admin_login.html", map[string]interface{}{
			"Title": "Вход администратора", "Page": "admin_login", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	totpCode := strings.TrimSpace(r.FormValue("totp_code"))

	lockKey := fmt.Sprintf("admin_lock_%s_%s", username, clientIP)
	if locked, remaining, reason := s.rateLimiter.IsLocked(lockKey); locked {
		s.securityLog.LogEvent("admin_login_failed", clientIP, "locked username="+username)
		ratelimit.SetRetryAfterHeader(w, int(remaining.Seconds()))
		s.renderTemplate(w, "admin_login.html", map[string]interface{}{
			"Title": "Ошибка входа", "Error": fmt.Sprintf("Вход заблокирован: %s", reason),
		})
		return
	}

	var admin models.AdminUser
	err := s.db.QueryRow("SELECT id, username, password_hash, totp_secret, totp_enabled FROM admin_users WHERE username = ?", username).Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &admin.TOTPSecret, &admin.TOTPEnabled)
	if err != nil {
		s.securityLog.LogEvent("admin_login_failed", clientIP, "user not found "+username)
		if !s.rateLimiter.AllowTokenBucket("admin_fail_"+clientIP, 5, 5) {
			_ = s.rateLimiter.Lock(lockKey, "admin_failed", "Слишком много неудачных попыток входа", 15*time.Minute)
		}
		s.renderTemplate(w, "admin_login.html", map[string]interface{}{
			"Title": "Ошибка входа", "Error": "Неверное имя пользователя, пароль или TOTP-код", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	validPass, err := auth.VerifyPassword(password, admin.PasswordHash)
	if !validPass || err != nil {
		s.securityLog.LogEvent("admin_login_failed", clientIP, "wrong password "+username)
		if !s.rateLimiter.AllowTokenBucket("admin_fail_"+clientIP, 5, 5) {
			_ = s.rateLimiter.Lock(lockKey, "admin_failed", "Слишком много неудачных попыток входа", 15*time.Minute)
		}
		s.renderTemplate(w, "admin_login.html", map[string]interface{}{
			"Title": "Ошибка входа", "Error": "Неверное имя пользователя, пароль или TOTP-код", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	// Verify Mandatory TOTP
	if !auth.ValidateTOTP(admin.TOTPSecret, totpCode) {
		s.securityLog.LogEvent("admin_totp_failed", clientIP, "invalid totp "+username)
		if !s.rateLimiter.AllowTokenBucket("admin_fail_"+clientIP, 5, 5) {
			_ = s.rateLimiter.Lock(lockKey, "admin_totp_failed", "Слишком много неудачных попыток входа", 15*time.Minute)
		}
		s.renderTemplate(w, "admin_login.html", map[string]interface{}{
			"Title": "Ошибка входа", "Error": "Неверный 6-значный TOTP-код", "CSRFToken": s.generateCSRFToken(),
		})
		return
	}

	_ = s.rateLimiter.Unlock(lockKey)


	// Success -> Create Admin DeviceSession
	token := auth.GenerateRandomToken(32)
	tokenHash := auth.HashWithSalt(token, s.cfg.Secrets.SessionSecret)

	now := time.Now().UTC()
	idleExpires := now.Add(12 * time.Hour)
	absExpires := now.Add(7 * 24 * time.Hour)

	ipHash := auth.HashWithSalt(clientIP, s.cfg.Secrets.IPHashSalt)
	uaHash := auth.HashWithSalt(r.UserAgent(), s.cfg.Secrets.IPHashSalt)

	// Admin session uses NULL person_id
	_, err = s.db.Exec(`
		INSERT INTO device_sessions (person_id, admin_id, is_admin, name, session_token_hash, created_at, last_used_at, last_ip_hash, last_user_agent_hash, idle_expires_at, absolute_expires_at, revoked)
		VALUES (NULL, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`, admin.ID, "Admin Session", tokenHash, now, now, ipHash, uaHash, idleExpires, absExpires)


	if err != nil {
		log.Printf("[Session Error] Failed to insert admin session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}


	s.auditLog.Log("admin", admin.ID, "admin_login", "admin_user", fmt.Sprintf("%d", admin.ID), clientIP, "Admin logged in successfully")

	http.SetCookie(w, &http.Cookie{
		Name:     "homeshare_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil,
	})

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("homeshare_session")
	if err == nil && cookie.Value != "" {
		tokenHash := auth.HashWithSalt(cookie.Value, s.cfg.Secrets.SessionSecret)
		_, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE session_token_hash = ?", tokenHash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "homeshare_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// User Dashboard
func (s *Server) handleUserDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	distDir := findDistDir()
	if distDir != "" {
		indexPath := filepath.Join(distDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	sess, person, admin := s.getSession(r)
	if admin != nil {
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}
	if sess == nil || person == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get user files & quotas
	month := traffic.GetCurrentMonth()
	var uploadCompleted, uploadAborted, downloadCompleted, downloadAborted int64
	_ = s.db.QueryRow(`
		SELECT upload_completed_bytes, upload_aborted_bytes, download_completed_bytes, download_aborted_bytes
		FROM traffic_counters WHERE person_id = ? AND month = ?
	`, person.ID, month).Scan(&uploadCompleted, &uploadAborted, &downloadCompleted, &downloadAborted)

	var storageUsed int64
	_ = s.db.QueryRow("SELECT COALESCE(SUM(size), 0) FROM files WHERE person_id = ? AND status = 'ready'", person.ID).Scan(&storageUsed)

	effUpload := traffic.CalculateEffectiveUsed(uploadCompleted, uploadAborted, person.MonthlyUploadLimitBytes, true)
	effDownload := traffic.CalculateEffectiveUsed(downloadCompleted, downloadAborted, person.MonthlyDownloadLimit, false)

	// Fetch files
	rows, err := s.db.Query(`
		SELECT id, original_name, uploader_name, size, status, keep_forever, expires_at, created_at, protected, person_id
		FROM files WHERE status = 'ready' ORDER BY created_at DESC
	`)

	type fileItem struct {
		ID                 string
		OriginalName       string
		UploaderName       string
		SizeFormatted      string
		CreatedAtFormatted string
		ExpiresAtFormatted string
		KeepForever        bool
		IsPreviewable      bool
		CanDelete          bool
	}

	var files []fileItem
	if err == nil {
		for rows.Next() {
			var f models.FileRecord
			_ = rows.Scan(&f.ID, &f.OriginalName, &f.UploaderName, &f.Size, &f.Status, &f.KeepForever, &f.ExpiresAt, &f.CreatedAt, &f.Protected, &f.PersonID)

			expStr := "Срок не задан"
			if f.ExpiresAt != nil {
				expStr = f.ExpiresAt.Format("02.01.2006 15:04")
			}

			files = append(files, fileItem{
				ID:                 f.ID,
				OriginalName:       f.OriginalName,
				UploaderName:       f.UploaderName,
				SizeFormatted:      formatBytes(f.Size),
				CreatedAtFormatted: f.CreatedAt.Format("02.01.2006 15:04"),
				ExpiresAtFormatted: expStr,
				KeepForever:        f.KeepForever,
				IsPreviewable:      isPreviewableType(f.OriginalName),
				CanDelete:          (!f.Protected && f.PersonID == person.ID),
			})
		}
		rows.Close()
	}

	// Fetch user's quarantined files
	qRows, err := s.db.Query(`
		SELECT id, original_name, size, created_at
		FROM files WHERE person_id = ? AND status = 'quarantined'
	`, person.ID)

	type qItem struct {
		OriginalName       string
		SizeFormatted      string
		CreatedAtFormatted string
	}
	var qFiles []qItem
	if err == nil {
		for qRows.Next() {
			var q models.FileRecord
			_ = qRows.Scan(&q.ID, &q.OriginalName, &q.Size, &q.CreatedAt)
			qFiles = append(qFiles, qItem{
				OriginalName:       q.OriginalName,
				SizeFormatted:      formatBytes(q.Size),
				CreatedAtFormatted: q.CreatedAt.Format("02.01.2006 15:04"),
			})
		}
		qRows.Close()
	}

	storagePercent := float64(storageUsed) / float64(person.StorageQuotaBytes) * 100
	uploadPercent := float64(effUpload) / float64(person.MonthlyUploadLimitBytes) * 100
	downloadPercent := float64(effDownload) / float64(person.MonthlyDownloadLimit) * 100

	s.renderTemplate(w, "user_dashboard.html", map[string]interface{}{
		"Title":                  "Файлообменник",
		"Page":                   "home",
		"User":                   person,
		"CSRFToken":              s.generateCSRFToken(),
		"StorageUsedFormatted":   formatBytes(storageUsed),
		"StorageQuotaFormatted":  formatBytes(person.StorageQuotaBytes),
		"StoragePercent":         fmt.Sprintf("%.1f", storagePercent),
		"UploadUsedFormatted":    formatBytes(effUpload),
		"UploadLimitFormatted":   formatBytes(person.MonthlyUploadLimitBytes),
		"UploadPercent":          fmt.Sprintf("%.1f", uploadPercent),
		"DownloadUsedFormatted":  formatBytes(effDownload),
		"DownloadLimitFormatted": formatBytes(person.MonthlyDownloadLimit),
		"DownloadPercent":        fmt.Sprintf("%.1f", downloadPercent),
		"MaxFileSizeFormatted":   formatBytes(person.MaxFileSizeBytes),
		"Files":                  files,
		"QuarantinedFiles":       qFiles,
	})
}


// User Delete Own File
func (s *Server) handleUserDeleteFile(w http.ResponseWriter, r *http.Request) {
	sess, person, _ := s.getSession(r)
	if sess == nil || person == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	fileID := strings.TrimPrefix(r.URL.Path, "/files/delete/")
	var f models.FileRecord
	err := s.db.QueryRow("SELECT id, person_id, stored_path, protected FROM files WHERE id = ?", fileID).Scan(&f.ID, &f.PersonID, &f.StoredPath, &f.Protected)
	if err != nil || f.PersonID != person.ID || f.Protected {
		http.Error(w, "Forbidden or not found", http.StatusForbidden)
		return
	}

	_ = s.sm.DeleteFile(f.StoredPath)
	_, _ = s.db.Exec("DELETE FROM files WHERE id = ?", f.ID)
	s.auditLog.Log("person", person.ID, "delete_file", "file", fileID, netutils.GetClientIP(r), "User deleted file")

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Chunked Upload API - Step 1: POST /api/uploads
func (s *Server) handleUploadCreate(w http.ResponseWriter, r *http.Request) {
	sess, person, _ := s.getSession(r)
	if sess == nil || person == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	isLocal := s.netChecker.IsLocal(r)
	clientIP := netutils.GetClientIP(r)

	// Rate limit check for Upload Create
	limitPerH := 20
	if isLocal {
		limitPerH = 100
	}
	if !s.rateLimiter.AllowTokenBucket("up_create_"+clientIP, limitPerH, limitPerH) {
		http.Error(w, `{"error":"Слишком много запросов на загрузку"}`, http.StatusTooManyRequests)
		return
	}

	var req struct {
		Filename    string `json:"filename"`
		Size        int64  `json:"size"`
		ContentType string `json:"content_type"`
		ExpiryDays  int    `json:"expiry_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	filename := storage.SanitizeFilename(req.Filename)

	// 1. Max File Size Check
	if req.Size > person.MaxFileSizeBytes {
		http.Error(w, `{"error":"Размер файла превышает максимально допустимый"}`, http.StatusBadRequest)
		return
	}

	// 2. Max Concurrent Uploads Check
	var activeCount int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM uploads WHERE person_id = ? AND status IN ('reserved', 'uploading')", person.ID).Scan(&activeCount)
	if activeCount >= person.MaxConcurrentUploads {
		http.Error(w, `{"error":"Превышено количество одновременных загрузок"}`, http.StatusBadRequest)
		return
	}

	// 3. Storage Quota Check (Strict: ready files + reserved uploads + declared_size)
	var currentStorageUsed int64
	_ = s.db.QueryRow("SELECT COALESCE(SUM(size), 0) FROM files WHERE person_id = ? AND status = 'ready'", person.ID).Scan(&currentStorageUsed)
	var reservedUploadsUsed int64
	_ = s.db.QueryRow("SELECT COALESCE(SUM(declared_size - received_bytes), 0) FROM uploads WHERE person_id = ? AND status IN ('reserved', 'uploading')", person.ID).Scan(&reservedUploadsUsed)

	if (currentStorageUsed + reservedUploadsUsed + req.Size) > person.StorageQuotaBytes {
		http.Error(w, `{"error":"Недостаточно места в вашей квоте хранилища"}`, http.StatusBadRequest)
		return
	}

	// 4. Monthly Traffic Quota Check (Grace Rule)
	canUpload, err := s.tm.CanUpload(person.ID, person.MonthlyUploadLimitBytes, req.Size, person.IgnoreTrafficQuota, isLocal)
	if err != nil || !canUpload {
		http.Error(w, `{"error":"Превышен месячный лимит трафика загрузки"}`, http.StatusForbidden)
		return
	}

	// 5. Global Disk Space Reserve Check
	if err := s.sm.CheckDiskSpaceForNewUpload(req.Size); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInsufficientStorage)
		return
	}

	uploadID := auth.GenerateRandomID(16)
	secret := auth.GenerateRandomToken(32)
	secretHash := auth.HashWithSalt(secret, s.cfg.Secrets.IPHashSalt)

	reservationExpires := time.Now().UTC().Add(24 * time.Hour) // Dynamic reservation TTL
	ipHash := auth.HashWithSalt(clientIP, s.cfg.Secrets.IPHashSalt)

	if req.ExpiryDays <= 0 {
		req.ExpiryDays = 14
	}

	_, err = s.db.Exec(`
		INSERT INTO uploads (id, person_id, session_id, upload_secret_hash, original_name, declared_size, received_bytes, status, expiry_days, reservation_expires_at, created_at, client_ip_hash)
		VALUES (?, ?, ?, ?, ?, ?, 0, 'reserved', ?, ?, ?, ?)
	`, uploadID, person.ID, sess.ID, secretHash, filename, req.Size, req.ExpiryDays, reservationExpires, time.Now().UTC(), ipHash)

	if err != nil {
		http.Error(w, `{"error":"Failed to create upload"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"upload_id":     uploadID,
		"upload_secret": secret,
	})
}

// Chunked Upload API - Step 2: HEAD / PATCH / DELETE / POST complete
func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/uploads/")
	parts := strings.Split(path, "/")
	uploadID := parts[0]

	var u models.Upload
	err := s.db.QueryRow(`
		SELECT id, person_id, session_id, upload_secret_hash, original_name, declared_size, received_bytes, status, expiry_days, reservation_expires_at
		FROM uploads WHERE id = ?
	`, uploadID).Scan(&u.ID, &u.PersonID, &u.SessionID, &u.UploadSecretHash, &u.OriginalName, &u.DeclaredSize, &u.ReceivedBytes, &u.Status, &u.ExpiryDays, &u.ReservationExpiresAt)

	if err != nil {
		http.Error(w, `{"error":"Upload not found"}`, http.StatusNotFound)
		return
	}

	secret := r.Header.Get("X-Upload-Secret")
	if auth.HashWithSalt(secret, s.cfg.Secrets.IPHashSalt) != u.UploadSecretHash {
		http.Error(w, `{"error":"Invalid upload secret"}`, http.StatusForbidden)
		return
	}

	clientIP := netutils.GetClientIP(r)
	isLocal := s.netChecker.IsLocal(r)

	// DELETE -> Cancel Upload
	if r.Method == "DELETE" {
		s.sm.DeletePartFile(uploadID)
		_, _ = s.db.Exec("UPDATE uploads SET status = 'canceled' WHERE id = ?", uploadID)
		if u.ReceivedBytes > 0 {
			_ = s.tm.RecordUploadAborted(u.PersonID, u.ReceivedBytes, isLocal)
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// HEAD -> Query offset
	if r.Method == "HEAD" {
		w.Header().Set("Upload-Offset", fmt.Sprintf("%d", u.ReceivedBytes))
		w.WriteHeader(http.StatusOK)
		return
	}

	// POST /complete -> Finalize Upload
	if r.Method == "POST" && len(parts) > 1 && parts[1] == "complete" {
		fileID := auth.GenerateRandomID(16)
		finalPath, err := s.sm.FinalizeUpload(uploadID, fileID)
		if err != nil {
			http.Error(w, `{"error":"Failed to finalize upload"}`, http.StatusInternalServerError)
			return
		}

		// Check for suspicious extension / quarantine
		status := models.FileStatusReady
		flagged := false
		flagReason := ""

		ext := strings.ToLower(filepath.Ext(u.OriginalName))
		if strings.HasPrefix(ext, ".") {
			ext = ext[1:]
		}

		for _, suspExt := range s.cfg.SuspiciousExtensions {
			if ext == strings.ToLower(suspExt) {
				status = models.FileStatusQuarantined
				flagged = true
				flagReason = fmt.Sprintf("Подозрительное расширение .%s", ext)
				break
			}
		}

		// Double extension check (e.g. file.pdf.exe)
		if !flagged && strings.Count(u.OriginalName, ".") > 1 {
			hasSusp := false
			parts := strings.Split(strings.ToLower(u.OriginalName), ".")
			for _, part := range parts[1:] {
				for _, suspExt := range s.cfg.SuspiciousExtensions {
					if part == strings.ToLower(suspExt) {
						hasSusp = true
						break
					}
				}
				if hasSusp {
					break
				}
			}
			if hasSusp {
				status = models.FileStatusQuarantined
				flagged = true
				flagReason = "Двойное расширение с исполняемым файлом"
			}
		}

		var expiresAt *time.Time
		if u.ExpiryDays > 0 {
			t := time.Now().UTC().Add(time.Duration(u.ExpiryDays) * 24 * time.Hour)
			expiresAt = &t
		}

		var uploaderLabel string
		_ = s.db.QueryRow("SELECT label FROM people WHERE id = ?", u.PersonID).Scan(&uploaderLabel)

		ipHash := auth.HashWithSalt(clientIP, s.cfg.Secrets.IPHashSalt)

		_, err = s.db.Exec(`
			INSERT INTO files (id, person_id, uploader_name, original_name, stored_path, size, content_type, status, flagged, flag_reason, protected, keep_forever, expires_at, created_at, client_ip_hash)
			VALUES (?, ?, ?, ?, ?, ?, 'application/octet-stream', ?, ?, ?, 0, 0, ?, ?, ?)
		`, fileID, u.PersonID, uploaderLabel, u.OriginalName, finalPath, u.ReceivedBytes, string(status), flagged, flagReason, expiresAt, time.Now().UTC(), ipHash)

		if err != nil {
			http.Error(w, `{"error":"Failed to save file record"}`, http.StatusInternalServerError)
			return
		}

		now := time.Now().UTC()
		_, _ = s.db.Exec("UPDATE uploads SET status = 'completed', completed_at = ? WHERE id = ?", now, uploadID)
		_ = s.tm.RecordUploadCompleted(u.PersonID, u.ReceivedBytes, isLocal)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"file_id": fileID, "status": string(status)})
		return
	}

	// PATCH -> Upload Chunk
	if r.Method == "PATCH" {
		if err := s.sm.CheckDiskSpaceCritical(); err != nil {
			http.Error(w, `{"error":"Критический дефицит дискового пространства"}`, http.StatusInsufficientStorage)
			return
		}

		offsetStr := r.URL.Query().Get("offset")
		offset, _ := strconv.ParseInt(offsetStr, 10, 64)
		if offset != u.ReceivedBytes {
			http.Error(w, fmt.Sprintf(`{"error":"Offset mismatch. Expected %d"}`, u.ReceivedBytes), http.StatusBadRequest)
			return
		}

		f, _, err := s.sm.PreparePartFile(uploadID)
		if err != nil {
			http.Error(w, `{"error":"Failed to open part file"}`, http.StatusInternalServerError)
			return
		}
		defer f.Close()

		_, _ = f.Seek(offset, io.SeekStart)

		// Speed limited reader
		limitedReader := s.speedLimit.NewReader(r.Context(), r.Body, !isLocal, true)

		written, err := io.Copy(f, limitedReader)
		if err != nil && err != io.EOF {
			http.Error(w, `{"error":"Failed to write chunk"}`, http.StatusInternalServerError)
			return
		}

		newTotal := u.ReceivedBytes + written
		newTTL := time.Now().UTC().Add(1 * time.Hour) // extend TTL after chunk
		_, _ = s.db.Exec("UPDATE uploads SET received_bytes = ?, status = 'uploading', reservation_expires_at = ? WHERE id = ?", newTotal, newTTL, uploadID)

		w.Header().Set("Upload-Offset", fmt.Sprintf("%d", newTotal))
		w.WriteHeader(http.StatusOK)
		return
	}
}

type speedResponseWriter struct {
	http.ResponseWriter
	writer io.Writer
}

func (sw *speedResponseWriter) Write(p []byte) (int, error) {
	return sw.writer.Write(p)
}

// Download File Stream Handler with HTTP Range
func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	sess, person, admin := s.getSession(r)
	if sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fileID := strings.TrimPrefix(r.URL.Path, "/download/")
	var f models.FileRecord
	err := s.db.QueryRow("SELECT id, person_id, original_name, stored_path, size, content_type, status FROM files WHERE id = ?", fileID).Scan(&f.ID, &f.PersonID, &f.OriginalName, &f.StoredPath, &f.Size, &f.ContentType, &f.Status)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if f.Status == models.FileStatusQuarantined && admin == nil && (person == nil || f.PersonID != person.ID) {
		http.Error(w, "Quarantined file", http.StatusForbidden)
		return
	}

	isLocal := s.netChecker.IsLocal(r)

	// Check download quota if person
	if person != nil {
		canDown, err := s.tm.CanDownload(person.ID, person.MonthlyDownloadLimit, f.Size, person.IgnoreTrafficQuota, isLocal)
		if err != nil || !canDown {
			http.Error(w, "Превышен лимит скачивания на этот месяц", http.StatusForbidden)
			return
		}
	}

	file, err := os.Open(f.StoredPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	safeFilename := strings.ReplaceAll(strings.ReplaceAll(f.OriginalName, `"`, `\"`), "\n", "")
	escapedFilename := url.PathEscape(f.OriginalName)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeFilename, escapedFilename))
	w.Header().Set("Content-Type", "application/octet-stream")

	// Wrap in speed limiter response writer
	speedWriter := s.speedLimit.NewWriter(r.Context(), w, !isLocal, false)
	srw := &speedResponseWriter{ResponseWriter: w, writer: speedWriter}

	http.ServeContent(srw, r, f.OriginalName, time.Now(), file)

	if person != nil {
		_ = s.tm.RecordDownloadCompleted(person.ID, f.Size, isLocal)
	}
}


// Preview Safe Media Types
func isPreviewableType(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	safeExts := map[string]bool{
		".mp4": true, ".webm": true, ".mp3": true, ".ogg": true,
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	}
	return safeExts[ext]
}

func (s *Server) handlePreviewFile(w http.ResponseWriter, r *http.Request) {
	sess, person, admin := s.getSession(r)
	if sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fileID := strings.TrimPrefix(r.URL.Path, "/preview/")
	var f models.FileRecord
	err := s.db.QueryRow("SELECT id, person_id, original_name, stored_path, size, status FROM files WHERE id = ?", fileID).Scan(&f.ID, &f.PersonID, &f.OriginalName, &f.StoredPath, &f.Size, &f.Status)
	if err != nil || !isPreviewableType(f.OriginalName) {
		http.Error(w, "Preview not allowed for this file type", http.StatusBadRequest)
		return
	}

	if f.Status == models.FileStatusQuarantined && admin == nil && (person == nil || f.PersonID != person.ID) {
		http.Error(w, "Quarantined file", http.StatusForbidden)
		return
	}

	ext := filepath.Ext(f.OriginalName)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; media-src 'self'; image-src 'self'; style-src 'unsafe-inline';")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	http.ServeFile(w, r, f.StoredPath)
}

// Multi-file ZIP store mode download
func (s *Server) handleZipDownload(w http.ResponseWriter, r *http.Request) {
	sess, person, _ := s.getSession(r)
	if sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idsParam := r.URL.Query().Get("ids")
	fileIDs := strings.Split(idsParam, ",")

	if len(fileIDs) == 0 || len(fileIDs) > s.cfg.ZipLimits.MaxFiles {
		http.Error(w, "Превышено максимальное число файлов в ZIP архиве", http.StatusBadRequest)
		return
	}

	var totalSize int64
	var validFiles []models.FileRecord

	for _, fileID := range fileIDs {
		var f models.FileRecord
		err := s.db.QueryRow("SELECT id, original_name, stored_path, size, status FROM files WHERE id = ?", strings.TrimSpace(fileID)).Scan(&f.ID, &f.OriginalName, &f.StoredPath, &f.Size, &f.Status)
		if err != nil || f.Status == models.FileStatusQuarantined {
			continue
		}
		totalSize += f.Size
		validFiles = append(validFiles, f)
	}

	isLocal := s.netChecker.IsLocal(r)
	if person != nil {
		canDown, err := s.tm.CanDownload(person.ID, person.MonthlyDownloadLimit, totalSize, person.IgnoreTrafficQuota, isLocal)
		if err != nil || !canDown {
			http.Error(w, "Превышена месячная квота скачивания", http.StatusForbidden)
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="homeshare_archive.zip"`)

	zipWriter := zip.NewWriter(s.speedLimit.NewWriter(r.Context(), w, !isLocal, false))
	defer zipWriter.Close()

	var downloadedBytes int64
	for _, f := range validFiles {
		file, err := os.Open(f.StoredPath)
		if err != nil {
			continue
		}

		header := &zip.FileHeader{
			Name:   f.OriginalName,
			Method: zip.Store, // Store mode without compression
		}
		writer, err := zipWriter.CreateHeader(header)
		if err == nil {
			written, _ := io.Copy(writer, file)
			downloadedBytes += written
		}
		file.Close()
	}

	if person != nil && downloadedBytes > 0 {
		_ = s.tm.RecordDownloadCompleted(person.ID, downloadedBytes, isLocal)
	}
}

// Admin Dashboard
func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	freeBytes, totalBytes, freeInodes, _ := s.sm.GetDiskUsage()
	upBps, downBps := s.speedLimit.GetStats()

	var activeSessions, activeUploads, quarantineCount int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM device_sessions WHERE revoked = 0").Scan(&activeSessions)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM uploads WHERE status IN ('reserved', 'uploading')").Scan(&activeUploads)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM files WHERE status = 'quarantined'").Scan(&quarantineCount)

	type auditItem struct {
		TimeFormatted string
		ActorType     string
		ActorID       int64
		Event         string
		Details       string
	}
	var logs []auditItem
	rows, err := s.db.Query("SELECT time, actor_type, actor_id, event, details FROM audit_logs ORDER BY id DESC LIMIT 10")
	if err == nil {
		for rows.Next() {
			var a models.AuditLog
			_ = rows.Scan(&a.Time, &a.ActorType, &a.ActorID, &a.Event, &a.Details)
			logs = append(logs, auditItem{
				TimeFormatted: a.Time.Format("02.01 15:04:05"),
				ActorType:     a.ActorType,
				ActorID:       a.ActorID,
				Event:         a.Event,
				Details:       a.Details,
			})
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_dashboard.html", map[string]interface{}{
		"Title":                 "Админ Панель",
		"Page":                  "dashboard",
		"IsAdmin":               true,
		"FreeSpaceFormatted":    formatBytes(freeBytes),
		"TotalSpaceFormatted":   formatBytes(totalBytes),
		"FreeInodes":            freeInodes,
		"UploadSpeedFormatted":  formatBps(upBps),
		"DownloadSpeedFormatted": formatBps(downBps),
		"ActiveSessionsCount":  activeSessions,
		"ActiveUploadsCount":   activeUploads,
		"QuarantineCount":      quarantineCount,
		"RecentAuditLogs":      logs,
	})
}

// Admin People List
func (s *Server) handleAdminPeople(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT id, label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes FROM people ORDER BY id DESC")
	type personItem struct {
		ID                     int64
		Label                  string
		Notes                  string
		Enabled                bool
		StorageQuotaFormatted  string
		UploadLimitFormatted   string
		DownloadLimitFormatted string
	}
	var people []personItem
	if err == nil {
		for rows.Next() {
			var p models.Person
			_ = rows.Scan(&p.ID, &p.Label, &p.Notes, &p.Enabled, &p.StorageQuotaBytes, &p.MonthlyUploadLimitBytes, &p.MonthlyDownloadLimit)
			people = append(people, personItem{
				ID:                     p.ID,
				Label:                  p.Label,
				Notes:                  p.Notes,
				Enabled:                p.Enabled,
				StorageQuotaFormatted:  formatBytes(p.StorageQuotaBytes),
				UploadLimitFormatted:   formatBytes(p.MonthlyUploadLimitBytes),
				DownloadLimitFormatted: formatBytes(p.MonthlyDownloadLimit),
			})
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_people.html", map[string]interface{}{
		"Title": "Пользователи", "Page": "people", "IsAdmin": true, "People": people, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminPeopleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	quotaGB, _ := strconv.ParseInt(r.FormValue("storage_quota_gb"), 10, 64)
	upGB, _ := strconv.ParseInt(r.FormValue("monthly_upload_gb"), 10, 64)
	downGB, _ := strconv.ParseInt(r.FormValue("monthly_download_gb"), 10, 64)
	maxSizeGB, _ := strconv.ParseInt(r.FormValue("max_file_size_gb"), 10, 64)

	ignoreTraffic := r.FormValue("ignore_traffic_quota") == "true"
	allowKeepForever := r.FormValue("allow_user_keep_forever") == "true"

	gb := int64(1024 * 1024 * 1024)
	_, _ = s.db.Exec(`
		INSERT INTO people (label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, allow_user_keep_forever, session_idle_days, session_absolute_days, ignore_traffic_quota, created_at)
		VALUES (?, ?, 1, ?, ?, ?, ?, 1, ?, 30, 90, ?, ?)
	`, label, notes, quotaGB*gb, upGB*gb, downGB*gb, maxSizeGB*gb, allowKeepForever, ignoreTraffic, time.Now().UTC())

	http.Redirect(w, r, "/admin/people", http.StatusSeeOther)
}

func (s *Server) handleAdminPeopleDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	personID := strings.TrimPrefix(r.URL.Path, "/admin/people/disable/")
	_, _ = s.db.Exec("UPDATE people SET enabled = 0 WHERE id = ?", personID)
	_, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE person_id = ?", personID)
	_, _ = s.db.Exec("UPDATE uploads SET status = 'canceled' WHERE person_id = ? AND status IN ('reserved', 'uploading')", personID)
	http.Redirect(w, r, "/admin/people", http.StatusSeeOther)
}

func (s *Server) handleAdminPeopleEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	personID := strings.TrimPrefix(r.URL.Path, "/admin/people/enable/")
	_, _ = s.db.Exec("UPDATE people SET enabled = 1 WHERE id = ?", personID)
	http.Redirect(w, r, "/admin/people", http.StatusSeeOther)
}

func (s *Server) handleAdminPeopleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	personID := strings.TrimPrefix(r.URL.Path, "/admin/people/delete/")
	var label string
	_ = s.db.QueryRow("SELECT label FROM people WHERE id = ?", personID).Scan(&label)

	// Keep files as orphaned -> update uploader_name to "Label (deleted)"
	_, _ = s.db.Exec("UPDATE files SET uploader_name = ? WHERE person_id = ?", fmt.Sprintf("%s (deleted)", label), personID)
	_, _ = s.db.Exec("DELETE FROM people WHERE id = ?", personID)

	http.Redirect(w, r, "/admin/people", http.StatusSeeOther)
}

// Admin Invites
func (s *Server) handleAdminInvites(w http.ResponseWriter, r *http.Request) {
	pRows, _ := s.db.Query("SELECT id, label FROM people WHERE enabled = 1")
	var people []models.Person
	for pRows.Next() {
		var p models.Person
		_ = pRows.Scan(&p.ID, &p.Label)
		people = append(people, p)
	}
	pRows.Close()

	rows, err := s.db.Query(`
		SELECT i.id, p.label, i.code_prefix, i.max_activations, i.activations_used, i.expires_at
		FROM invite_codes i JOIN people p ON i.person_id = p.id
		WHERE i.enabled = 1 ORDER BY i.id DESC
	`)

	type inviteItem struct {
		ID                 int64
		PersonLabel        string
		CodePrefix         string
		MaxActivations     int
		ActivationsUsed    int
		ExpiresAtFormatted string
	}
	var invites []inviteItem
	if err == nil {
		for rows.Next() {
			var inv inviteItem
			var exp time.Time
			_ = rows.Scan(&inv.ID, &inv.PersonLabel, &inv.CodePrefix, &inv.MaxActivations, &inv.ActivationsUsed, &exp)
			inv.ExpiresAtFormatted = exp.Format("02.01.2006 15:04")
			invites = append(invites, inv)
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_invites.html", map[string]interface{}{
		"Title": "Инвайты", "Page": "invites", "IsAdmin": true, "People": people, "Invites": invites, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminInvitesCreate(w http.ResponseWriter, r *http.Request) {
	personID, _ := strconv.ParseInt(r.FormValue("person_id"), 10, 64)
	maxActivations, _ := strconv.Atoi(r.FormValue("max_activations"))
	expiresHours, _ := strconv.Atoi(r.FormValue("expires_hours"))

	code := auth.GenerateInviteCode()
	codeHash := auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt)
	codePrefix := auth.FormatCodePrefix(code)

	expiresAt := time.Now().UTC().Add(time.Duration(expiresHours) * time.Hour)

	_, err := s.db.Exec(`
		INSERT INTO invite_codes (person_id, code_hash, code_prefix, enabled, max_activations, activations_used, expires_at, created_at, created_by_admin_id)
		VALUES (?, ?, ?, 1, ?, 0, ?, ?, 1)
	`, personID, codeHash, codePrefix, maxActivations, expiresAt, time.Now().UTC())

	if err != nil {
		http.Error(w, "Failed to create invite", http.StatusInternalServerError)
		return
	}

	// Generate QR Code
	qrPng, _ := auth.GenerateTOTPQRCodePNG("homeshare", code, "homeshare")
	qrBase64 := base64.StdEncoding.EncodeToString(qrPng)

	pRows, _ := s.db.Query("SELECT id, label FROM people WHERE enabled = 1")
	var people []models.Person
	for pRows.Next() {
		var p models.Person
		_ = pRows.Scan(&p.ID, &p.Label)
		people = append(people, p)
	}
	pRows.Close()

	s.renderTemplate(w, "admin_invites.html", map[string]interface{}{
		"Title": "Инвайт создан", "Page": "invites", "IsAdmin": true, "People": people,
		"NewInviteCode": code, "NewInviteQRData": qrBase64, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminInvitesRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	inviteID := strings.TrimPrefix(r.URL.Path, "/admin/invites/revoke/")
	_, _ = s.db.Exec("UPDATE invite_codes SET enabled = 0 WHERE id = ?", inviteID)
	http.Redirect(w, r, "/admin/invites", http.StatusSeeOther)
}

// Admin Sessions
func (s *Server) handleAdminSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT s.id, COALESCE(p.label, 'Admin'), s.name, s.last_used_at, s.last_ip_hash, s.idle_expires_at, s.absolute_expires_at, COALESCE(s.person_id, 0)
		FROM device_sessions s LEFT JOIN people p ON s.person_id = p.id
		WHERE s.revoked = 0 ORDER BY s.last_used_at DESC
	`)


	type sessionItem struct {
		ID                       int64
		PersonLabel              string
		Name                     string
		LastUsedAtFormatted      string
		LastIPHashShort          string
		IdleExpiresFormatted     string
		AbsoluteExpiresFormatted string
		PersonID                 int64
	}

	var sessions []sessionItem
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item sessionItem
			var lastUsed, idleExp time.Time
			var absExp *time.Time
			var ipHash string
			_ = rows.Scan(&item.ID, &item.PersonLabel, &item.Name, &lastUsed, &ipHash, &idleExp, &absExp, &item.PersonID)

			item.LastUsedAtFormatted = lastUsed.Format("02.01 15:04")
			item.IdleExpiresFormatted = idleExp.Format("02.01 15:04")
			item.LastIPHashShort = ipHash[:8] + "..."
			if absExp != nil {
				item.AbsoluteExpiresFormatted = absExp.Format("02.01.2006")
			} else {
				item.AbsoluteExpiresFormatted = "Unlimited"
			}
			sessions = append(sessions, item)
		}
	}

	s.renderTemplate(w, "admin_sessions.html", map[string]interface{}{
		"Title": "Сессии", "Page": "sessions", "IsAdmin": true, "Sessions": sessions, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminSessionsRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, "/admin/sessions/revoke/")
	_, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", sessionID)
	http.Redirect(w, r, "/admin/sessions", http.StatusSeeOther)
}

func (s *Server) handleAdminSessionsRevokeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	personID := strings.TrimPrefix(r.URL.Path, "/admin/sessions/revoke-all/")
	_, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE person_id = ?", personID)
	http.Redirect(w, r, "/admin/sessions", http.StatusSeeOther)
}

// Admin Files
func (s *Server) handleAdminFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")

	query := "SELECT id, uploader_name, original_name, size, status, protected, keep_forever, expires_at, created_at FROM files WHERE 1=1"
	var args []interface{}

	if q != "" {
		query += " AND original_name LIKE ?"
		args = append(args, "%"+q+"%")
	}
	if status == "ready" || status == "quarantined" {
		query += " AND status = ?"
		args = append(args, status)
	} else if status == "protected" {
		query += " AND protected = 1"
	}

	query += " ORDER BY created_at DESC LIMIT 50"

	rows, err := s.db.Query(query, args...)
	type fileItem struct {
		ID                 string
		OriginalName       string
		UploaderName       string
		SizeFormatted      string
		Status             string
		Protected          bool
		KeepForever        bool
		CreatedAtFormatted string
		ExpiresAtFormatted string
	}
	var files []fileItem
	if err == nil {
		for rows.Next() {
			var f models.FileRecord
			_ = rows.Scan(&f.ID, &f.UploaderName, &f.OriginalName, &f.Size, &f.Status, &f.Protected, &f.KeepForever, &f.ExpiresAt, &f.CreatedAt)

			expStr := "Срок не задан"
			if f.ExpiresAt != nil {
				expStr = f.ExpiresAt.Format("02.01.2006 15:04")
			}

			files = append(files, fileItem{
				ID:                 f.ID,
				OriginalName:       f.OriginalName,
				UploaderName:       f.UploaderName,
				SizeFormatted:      formatBytes(f.Size),
				Status:             string(f.Status),
				Protected:          f.Protected,
				KeepForever:        f.KeepForever,
				CreatedAtFormatted: f.CreatedAt.Format("02.01.2006 15:04"),
				ExpiresAtFormatted: expStr,
			})
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_files.html", map[string]interface{}{
		"Title": "Файлы", "Page": "files", "IsAdmin": true, "Files": files, "SearchQuery": q, "StatusFilter": status, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminFilesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fileID := strings.TrimPrefix(r.URL.Path, "/admin/files/delete/")
	var storedPath string
	_ = s.db.QueryRow("SELECT stored_path FROM files WHERE id = ?", fileID).Scan(&storedPath)
	_ = s.sm.DeleteFile(storedPath)
	_, _ = s.db.Exec("DELETE FROM files WHERE id = ?", fileID)
	http.Redirect(w, r, "/admin/files", http.StatusSeeOther)
}

func (s *Server) handleAdminFilesToggleProtected(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fileID := strings.TrimPrefix(r.URL.Path, "/admin/files/toggle-protected/")
	_, _ = s.db.Exec("UPDATE files SET protected = NOT protected WHERE id = ?", fileID)
	http.Redirect(w, r, "/admin/files", http.StatusSeeOther)
}

func (s *Server) handleAdminFilesToggleForever(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fileID := strings.TrimPrefix(r.URL.Path, "/admin/files/toggle-forever/")
	_, _ = s.db.Exec("UPDATE files SET keep_forever = NOT keep_forever WHERE id = ?", fileID)
	http.Redirect(w, r, "/admin/files", http.StatusSeeOther)
}

// Admin Quarantine
func (s *Server) handleAdminQuarantine(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT id, uploader_name, original_name, size, flag_reason, created_at FROM files WHERE status = 'quarantined' ORDER BY created_at DESC")

	type qItem struct {
		ID                 string
		OriginalName       string
		UploaderName       string
		SizeFormatted      string
		FlagReason         string
		CreatedAtFormatted string
	}
	var files []qItem
	if err == nil {
		for rows.Next() {
			var f models.FileRecord
			_ = rows.Scan(&f.ID, &f.UploaderName, &f.OriginalName, &f.Size, &f.FlagReason, &f.CreatedAt)
			files = append(files, qItem{
				ID:                 f.ID,
				OriginalName:       f.OriginalName,
				UploaderName:       f.UploaderName,
				SizeFormatted:      formatBytes(f.Size),
				FlagReason:         f.FlagReason,
				CreatedAtFormatted: f.CreatedAt.Format("02.01.2006 15:04"),
			})
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_quarantine.html", map[string]interface{}{
		"Title": "Карантин", "Page": "quarantine", "IsAdmin": true, "Files": files, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminQuarantineApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fileID := strings.TrimPrefix(r.URL.Path, "/admin/quarantine/approve/")
	_, _ = s.db.Exec("UPDATE files SET status = 'ready', flagged = 0, flag_reason = '' WHERE id = ?", fileID)
	http.Redirect(w, r, "/admin/quarantine", http.StatusSeeOther)
}

// Admin Traffic
func (s *Server) handleAdminTraffic(w http.ResponseWriter, r *http.Request) {
	currentMonth := traffic.GetCurrentMonth()

	rows, err := s.db.Query(`
		SELECT p.id, p.label, p.monthly_upload_limit_bytes, p.monthly_download_limit_bytes,
		       t.upload_completed_bytes, t.upload_aborted_bytes, t.download_completed_bytes, t.download_aborted_bytes
		FROM people p
		LEFT JOIN traffic_counters t ON p.id = t.person_id AND t.month = ?
		WHERE p.enabled = 1
	`, currentMonth)

	type trafficItem struct {
		PersonID                  int64
		PersonLabel               string
		UploadCompletedFormatted  string
		UploadAbortedFormatted    string
		UploadEffectiveFormatted  string
		UploadLimitFormatted      string
		DownloadCompletedFormatted string
		DownloadAbortedFormatted   string
		DownloadEffectiveFormatted string
		DownloadLimitFormatted     string
	}

	var list []trafficItem
	if err == nil {
		for rows.Next() {
			var item trafficItem
			var upComp, upAbort, downComp, downAbort sql.NullInt64
			var upLimit, downLimit int64
			_ = rows.Scan(&item.PersonID, &item.PersonLabel, &upLimit, &downLimit, &upComp, &upAbort, &downComp, &downAbort)

			effUp := traffic.CalculateEffectiveUsed(upComp.Int64, upAbort.Int64, upLimit, true)
			effDown := traffic.CalculateEffectiveUsed(downComp.Int64, downAbort.Int64, downLimit, false)

			item.UploadCompletedFormatted = formatBytes(upComp.Int64)
			item.UploadAbortedFormatted = formatBytes(upAbort.Int64)
			item.UploadEffectiveFormatted = formatBytes(effUp)
			item.UploadLimitFormatted = formatBytes(upLimit)

			item.DownloadCompletedFormatted = formatBytes(downComp.Int64)
			item.DownloadAbortedFormatted = formatBytes(downAbort.Int64)
			item.DownloadEffectiveFormatted = formatBytes(effDown)
			item.DownloadLimitFormatted = formatBytes(downLimit)

			list = append(list, item)
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_traffic.html", map[string]interface{}{
		"Title": "Трафик", "Page": "traffic", "IsAdmin": true, "CurrentMonth": currentMonth, "CurrentTraffic": list, "CSRFToken": s.generateCSRFToken(),
	})
}

func (s *Server) handleAdminTrafficReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	personIDStr := strings.TrimPrefix(r.URL.Path, "/admin/traffic/reset/")
	personID, _ := strconv.ParseInt(personIDStr, 10, 64)
	_ = s.tm.ResetCurrentMonth(personID)
	http.Redirect(w, r, "/admin/traffic", http.StatusSeeOther)
}

// Admin Settings
func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT key, type, reason, expires_at FROM rate_limit_locks WHERE expires_at > ?", time.Now().UTC())

	type lockItem struct {
		Key                string
		Type               string
		Reason             string
		ExpiresAtFormatted string
	}
	var locks []lockItem
	if err == nil {
		for rows.Next() {
			var l lockItem
			var exp time.Time
			_ = rows.Scan(&l.Key, &l.Type, &l.Reason, &exp)
			l.ExpiresAtFormatted = exp.Format("02.01 15:04")
			locks = append(locks, l)
		}
		rows.Close()
	}

	s.renderTemplate(w, "admin_settings.html", map[string]interface{}{
		"Title": "Настройки", "Page": "settings", "IsAdmin": true, "Config": s.cfg, "Locks": locks,
		"SuspiciousExtensionsStr": strings.Join(s.cfg.SuspiciousExtensions, ", "), "CSRFToken": s.generateCSRFToken(),
	})
}


func (s *Server) handleAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	upMbps, _ := strconv.Atoi(r.FormValue("external_upload_mbps"))
	downMbps, _ := strconv.Atoi(r.FormValue("external_download_mbps"))
	burstMB, _ := strconv.Atoi(r.FormValue("burst_mb"))
	zipMaxFiles, _ := strconv.Atoi(r.FormValue("zip_max_files"))
	zipMaxGB, _ := strconv.ParseInt(r.FormValue("zip_max_total_gb"), 10, 64)
	suspStr := r.FormValue("suspicious_extensions")

	s.speedLimit.UpdateLimits(upMbps, downMbps, burstMB)

	var newSusp []string
	for _, ext := range strings.Split(suspStr, ",") {
		trimmed := strings.TrimSpace(ext)
		if trimmed != "" {
			newSusp = append(newSusp, trimmed)
		}
	}
	s.cfg.SuspiciousExtensions = newSusp
	s.cfg.ZipLimits.MaxFiles = zipMaxFiles
	s.cfg.ZipLimits.MaxTotalGB = zipMaxGB

	if err := config.SaveConfig(s.cfg, s.configPath); err != nil {
		log.Printf("[Settings Error] Failed to save config to %s: %v", s.configPath, err)
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (s *Server) handleAdminLocksClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/admin/locks/clear/")
	_ = s.rateLimiter.Unlock(key)
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (s *Server) handleAdminLocksClearAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = s.rateLimiter.ClearAllLocks()
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (s *Server) generateCSRFToken() string {
	return auth.GenerateRandomToken(16)
}

func formatBytes(b int64) string {
	if b == 0 {
		return "0 B"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatBps(bps int64) string {
	return formatBytes(bps) + "/s"
}

// --- JSON API Handlers for React SPA & API Clients ---

func (s *Server) handleAPIAuthLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
		TOTP     string `json:"totp"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON body"})
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "admin"
	}
	totpCode := strings.TrimSpace(req.TOTPCode)
	if totpCode == "" {
		totpCode = strings.TrimSpace(req.TOTP)
	}

	clientIP := netutils.GetClientIP(r)
	lockKey := fmt.Sprintf("admin_lock_%s_%s", username, clientIP)
	if locked, remaining, reason := s.rateLimiter.IsLocked(lockKey); locked {
		s.securityLog.LogEvent("admin_login_failed", clientIP, "locked username="+username)
		ratelimit.SetRetryAfterHeader(w, int(remaining.Seconds()))
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Вход заблокирован: %s", reason)})
		return
	}

	var admin models.AdminUser
	err := s.db.QueryRow("SELECT id, username, password_hash, totp_secret, totp_enabled FROM admin_users WHERE username = ?", username).Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &admin.TOTPSecret, &admin.TOTPEnabled)
	if err != nil {
		s.securityLog.LogEvent("admin_login_failed", clientIP, "user not found "+username)
		if !s.rateLimiter.AllowTokenBucket("admin_fail_"+clientIP, 5, 5) {
			_ = s.rateLimiter.Lock(lockKey, "admin_failed", "Слишком много неудачных попыток входа", 15*time.Minute)
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Неверное имя пользователя, пароль или TOTP-код"})
		return
	}

	validPass, err := auth.VerifyPassword(req.Password, admin.PasswordHash)
	if !validPass || err != nil {
		s.securityLog.LogEvent("admin_login_failed", clientIP, "wrong password "+username)
		if !s.rateLimiter.AllowTokenBucket("admin_fail_"+clientIP, 5, 5) {
			_ = s.rateLimiter.Lock(lockKey, "admin_failed", "Слишком много неудачных попыток входа", 15*time.Minute)
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Неверное имя пользователя, пароль или TOTP-код"})
		return
	}

	if admin.TOTPEnabled && !auth.ValidateTOTP(admin.TOTPSecret, totpCode) {
		s.securityLog.LogEvent("admin_totp_failed", clientIP, "invalid totp "+username)
		if !s.rateLimiter.AllowTokenBucket("admin_fail_"+clientIP, 5, 5) {
			_ = s.rateLimiter.Lock(lockKey, "admin_totp_failed", "Слишком много неудачных попыток входа", 15*time.Minute)
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Неверный 6-значный TOTP-код"})
		return
	}

	_ = s.rateLimiter.Unlock(lockKey)

	token := auth.GenerateRandomToken(32)
	tokenHash := auth.HashWithSalt(token, s.cfg.Secrets.SessionSecret)

	now := time.Now().UTC()
	idleExpires := now.Add(12 * time.Hour)
	absExpires := now.Add(7 * 24 * time.Hour)

	ipHash := auth.HashWithSalt(clientIP, s.cfg.Secrets.IPHashSalt)
	uaHash := auth.HashWithSalt(r.UserAgent(), s.cfg.Secrets.IPHashSalt)

	_, err = s.db.Exec(`
		INSERT INTO device_sessions (person_id, admin_id, is_admin, name, session_token_hash, created_at, last_used_at, last_ip_hash, last_user_agent_hash, idle_expires_at, absolute_expires_at, revoked)
		VALUES (NULL, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`, admin.ID, "Admin Session API", tokenHash, now, now, ipHash, uaHash, idleExpires, absExpires)

	if err != nil {
		log.Printf("[Session Error] Failed to insert admin session: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create session"})
		return
	}

	s.auditLog.Log("admin", admin.ID, "admin_login_api", "admin_user", fmt.Sprintf("%d", admin.ID), clientIP, "Admin logged in via API")

	http.SetCookie(w, &http.Cookie{
		Name:     "homeshare_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"token":  token,
		"role":   "admin",
		"user": map[string]string{
			"username": admin.Username,
			"role":     "admin",
		},
	})
}

func (s *Server) handleAPIAuthMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sess, person, admin := s.getSession(r)
	if sess == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"role":          "user",
		})
		return
	}

	if sess.IsAdmin && admin != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": true,
			"role":          "admin",
			"username":      admin.Username,
		})
		return
	}

	username := ""
	if person != nil {
		username = person.Label
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		"role":          "user",
		"username":      username,
	})
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var usedStorage int64
	_ = s.db.QueryRow("SELECT COALESCE(SUM(size), 0) FROM files WHERE status = 'ready'").Scan(&usedStorage)

	var filesCount, quarantineCount int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM files WHERE status = 'ready'").Scan(&filesCount)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM files WHERE status = 'quarantined'").Scan(&quarantineCount)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"service":          "lares",
		"version":          "1.24.0",
		"uptime":           "12d 4h 18m",
		"active_transfers": 0,
		"files_count":      filesCount,
		"storage_used":     usedStorage,
		"storage_total":    s.cfg.StorageDefaults.QuotaBytes,
		"max_file_size":    s.cfg.StorageDefaults.MaxFileSize,
		"ratelimit_status": "норма",
		"quarantine_count": quarantineCount,
	})
}

func (s *Server) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, person, admin := s.getSession(r)

	query := "SELECT id, uploader_name, original_name, stored_path, size, content_type, status, flagged, flag_reason, created_at, expires_at FROM files WHERE 1=1"
	var args []interface{}

	if admin == nil {
		if person != nil {
			query += " AND (status = 'ready' OR person_id = ?)"
			args = append(args, person.ID)
		} else {
			query += " AND status = 'ready'"
		}
	}

	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var f models.FileRecord
		var expAt *time.Time
		var flagReason sql.NullString
		_ = rows.Scan(&f.ID, &f.UploaderName, &f.OriginalName, &f.StoredPath, &f.Size, &f.ContentType, &f.Status, &f.Flagged, &flagReason, &f.CreatedAt, &expAt)

		f.FlagReason = flagReason.String
		f.ExpiresAt = expAt

		item := map[string]interface{}{
			"id":             f.ID,
			"original_name":  f.OriginalName,
			"stored_path":    f.StoredPath,
			"size":           f.Size,
			"status":         f.Status,
			"flagged":        f.Flagged,
			"flag_reason":    f.FlagReason,
			"created_at":     f.CreatedAt.Format(time.RFC3339),
			"uploader_label": f.UploaderName,
		}
		if expAt != nil {
			item["expires_at"] = expAt.Format(time.RFC3339)
		}
		list = append(list, item)
	}

	if list == nil {
		list = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleAPIAdminInvites(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sess, _, admin := s.getSession(r)
	if sess == nil || !sess.IsAdmin || admin == nil {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Доступ запрещен"})
		return
	}

	if r.Method == http.MethodGet {
		rows, err := s.db.Query("SELECT code_prefix, max_activations, activations_used, enabled, created_at FROM invite_codes ORDER BY id DESC")
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var list []map[string]interface{}
		for rows.Next() {
			var prefix string
			var maxAct, act int
			var enabled bool
			var createdAt time.Time
			_ = rows.Scan(&prefix, &maxAct, &act, &enabled, &createdAt)
			list = append(list, map[string]interface{}{
				"code":            prefix,
				"max_activations": maxAct,
				"activations":     act,
				"revoked":         !enabled,
				"created_at":      createdAt.Format(time.RFC3339),
			})
		}
		if list == nil {
			list = []map[string]interface{}{}
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	if r.Method == http.MethodPost {
		code := auth.GenerateInviteCode()
		prefix := auth.FormatCodePrefix(code)
		codeHash := auth.HashString(code)

		var personID int64
		_ = s.db.QueryRow("SELECT id FROM people WHERE enabled = 1 ORDER BY id ASC LIMIT 1").Scan(&personID)
		if personID == 0 {
			res, err := s.db.Exec("INSERT INTO people (label, enabled, created_at) VALUES (?, 1, ?)", "Standard User", time.Now().UTC())
			if err == nil {
				personID, _ = res.LastInsertId()
			}
		}

		_, err := s.db.Exec(`
			INSERT INTO invite_codes (person_id, code_hash, code_prefix, max_activations, activations_used, enabled, created_at)
			VALUES (?, ?, ?, 5, 0, 1, ?)
		`, personID, codeHash, prefix, time.Now().UTC())

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create invite"})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":            code,
			"max_activations": 5,
			"activations":     0,
			"revoked":         false,
			"created_at":      time.Now().Format(time.RFC3339),
		})
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
	json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
}

func (s *Server) handleAPIAdminSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sess, _, admin := s.getSession(r)
	if sess == nil || !sess.IsAdmin || admin == nil {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Доступ запрещен"})
		return
	}

	if r.Method == http.MethodDelete || (strings.HasPrefix(r.URL.Path, "/api/admin/sessions/") && r.URL.Path != "/api/admin/sessions") {
		sessIDStr := strings.TrimPrefix(r.URL.Path, "/api/admin/sessions/")
		sessID, _ := strconv.ParseInt(sessIDStr, 10, 64)
		if sessID > 0 {
			_, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", sessID)
			json.NewEncoder(w).Encode(map[string]interface{}{"message": "Session revoked", "id": sessID})
			return
		}
	}

	rows, err := s.db.Query(`
		SELECT s.id, COALESCE(p.label, 'Admin'), s.name, s.last_used_at, s.last_ip_hash, s.revoked
		FROM device_sessions s LEFT JOIN people p ON s.person_id = p.id
		ORDER BY s.last_used_at DESC
	`)
	if err != nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int64
		var label, name, ipHash string
		var lastUsed time.Time
		var revoked bool
		_ = rows.Scan(&id, &label, &name, &lastUsed, &ipHash, &revoked)
		statusStr := "Активна"
		if revoked {
			statusStr = "Отозвана"
		}
		ipShort := ipHash
		if len(ipShort) > 8 {
			ipShort = ipShort[:8] + "..."
		}
		list = append(list, map[string]interface{}{
			"id":        id,
			"device":    fmt.Sprintf("%s (%s)", label, name),
			"ip":        ipShort,
			"last_seen": lastUsed.Format(time.RFC3339),
			"status":    statusStr,
			"revoked":   revoked,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleAPIAdminQuarantineApprove(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}
	sess, _, admin := s.getSession(r)
	if sess == nil || !sess.IsAdmin || admin == nil {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Доступ запрещен"})
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/quarantine/"), "/")
	if len(parts) >= 1 {
		fileID := parts[0]
		res, err := s.db.Exec("UPDATE files SET status = 'ready', flagged = 0, flag_reason = '' WHERE id = ?", fileID)
		if err == nil {
			rows, _ := res.RowsAffected()
			if rows > 0 {
				json.NewEncoder(w).Encode(map[string]string{"message": "File quarantine approved", "id": fileID})
				return
			}
		}
	}
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "File not found"})
}

func (s *Server) validateCSRFToken(r *http.Request, sess *models.DeviceSession) bool {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return true
	}

	givenToken := r.FormValue("csrf_token")
	if givenToken == "" {
		givenToken = r.Header.Get("X-CSRF-Token")
	}
	if givenToken == "" {
		return false
	}

	var expectedToken string
	if sess != nil {
		expectedToken = auth.HashWithSalt(fmt.Sprintf("csrf_%d_%s", sess.ID, sess.CreatedAt.Format(time.RFC3339)), s.cfg.Secrets.SessionSecret)[:32]
	} else {
		cookie, err := r.Cookie("homeshare_csrf")
		if err != nil || cookie.Value == "" {
			return false
		}
		expectedToken = cookie.Value
	}

	return subtle.ConstantTimeCompare([]byte(givenToken), []byte(expectedToken)) == 1
}
