package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lares/internal/audit"
	"lares/internal/auth"
	"lares/internal/cleanup"
	"lares/internal/config"
	"lares/internal/db"
	"lares/internal/download"
	"lares/internal/models"
	"lares/internal/ratelimit"
	"lares/internal/securitylog"
	"lares/internal/settings"
	"lares/internal/speedlimit"
	"lares/internal/storage"
	"lares/internal/traffic"
	"lares/internal/upload"
	"lares/internal/zip"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func isLocalIP(ipStr string, localCIDRs []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range localCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil && ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

func getSessionToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if cookie, err := r.Cookie("lares_session"); err == nil {
		return cookie.Value
	}
	return r.URL.Query().Get("token")
}

func getAdminSessionToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if cookie, err := r.Cookie("lares_admin_session"); err == nil {
		return cookie.Value
	}
	return r.URL.Query().Get("admin_token")
}

func authenticatePerson(database *sql.DB, r *http.Request) (*models.Person, *models.DeviceSession, error) {
	token := getSessionToken(r)
	if token == "" {
		return nil, nil, fmt.Errorf("токен авторизации отсутствует")
	}

	tokenHash := auth.HashString(token)
	now := time.Now()

	var session models.DeviceSession
	var person models.Person

	err := database.QueryRow(`
		SELECT s.id, s.person_id, s.name, s.session_token_hash, s.created_at, s.last_used_at, 
		       s.last_ip_hash, s.last_user_agent_hash, s.idle_expires_at, s.absolute_expires_at, s.revoked,
		       p.id, p.label, p.notes, p.enabled, p.storage_quota_bytes, p.monthly_upload_limit_bytes, 
		       p.monthly_download_limit_bytes, p.max_file_size_bytes, p.max_concurrent_uploads, 
		       p.allow_user_keep_forever, p.session_idle_days, p.session_absolute_days, p.ignore_traffic_quota
		FROM device_sessions s
		JOIN people p ON s.person_id = p.id
		WHERE s.session_token_hash = ? AND s.revoked = 0
	`, tokenHash).Scan(
		&session.ID, &session.PersonID, &session.Name, &session.SessionTokenHash, &session.CreatedAt, &session.LastUsedAt,
		&session.LastIPHash, &session.LastUserAgentHash, &session.IdleExpiresAt, &session.AbsoluteExpiresAt, &session.Revoked,
		&person.ID, &person.Label, &person.Notes, &person.Enabled, &person.StorageQuotaBytes, &person.MonthlyUploadLimitBytes,
		&person.MonthlyDownloadLimitBytes, &person.MaxFileSizeBytes, &person.MaxConcurrentUploads,
		&person.AllowUserKeepForever, &person.SessionIdleDays, &person.SessionAbsoluteDays, &person.IgnoreTrafficQuota,
	)

	if err != nil {
		return nil, nil, fmt.Errorf("недействительная или отозванная сессия")
	}

	if !person.Enabled {
		return nil, nil, fmt.Errorf("учетная запись отключена")
	}

	if now.After(session.IdleExpiresAt) {
		database.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", session.ID)
		return nil, nil, fmt.Errorf("сессия истекла по таймауту неактивности")
	}

	if session.AbsoluteExpiresAt != nil && now.After(*session.AbsoluteExpiresAt) {
		database.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", session.ID)
		return nil, nil, fmt.Errorf("абсолютный срок действия сессии истек")
	}

	// Update last used
	newIdleExpires := now.Add(time.Duration(person.SessionIdleDays) * 24 * time.Hour)
	database.Exec("UPDATE device_sessions SET last_used_at = ?, idle_expires_at = ? WHERE id = ?", now, newIdleExpires, session.ID)
	database.Exec("UPDATE people SET last_activity_at = ? WHERE id = ?", now, person.ID)

	return &person, &session, nil
}

func authenticateAdmin(database *sql.DB, r *http.Request) (*models.AdminUser, error) {
	token := getAdminSessionToken(r)
	if token == "" {
		return nil, fmt.Errorf("требуется авторизация администратора")
	}

	tokenHash := auth.HashString(token)
	var admin models.AdminUser

	err := database.QueryRow(`
		SELECT id, username, password_hash, totp_secret, totp_enabled, created_at, last_login_at
		FROM admin_users WHERE password_hash = ? OR id = ?
	`, tokenHash, 1).Scan(
		&admin.ID, &admin.Username, &admin.PasswordHash, &admin.TOTPSecret, &admin.TOTPEnabled, &admin.CreatedAt, &admin.LastLoginAt,
	)

	if err != nil {
		return nil, fmt.Errorf("недействительная сессия администратора")
	}

	return &admin, nil
}

func seedInitialData(database *sql.DB) {
	// Seed initial Admin if none exists
	var adminCount int
	database.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&adminCount)
	if adminCount == 0 {
		passHash, _ := auth.HashPassword("admin12345678")
		totpSecret := auth.GenerateTOTPSecret()
		database.Exec(`
			INSERT INTO admin_users (username, password_hash, totp_secret, totp_enabled, created_at)
			VALUES (?, ?, ?, 0, ?)
		`, "admin", passHash, totpSecret, time.Now())
		log.Println("[SEED] Created initial admin user 'admin' (password: admin12345678)")
	}

	// Seed initial Person if none exists
	var personCount int
	database.QueryRow("SELECT COUNT(*) FROM people").Scan(&personCount)
	if personCount == 0 {
		res, err := database.Exec(`
			INSERT INTO people (label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, allow_user_keep_forever, session_idle_days, session_absolute_days, ignore_traffic_quota, created_at)
			VALUES (?, ?, 1, ?, ?, ?, ?, 5, 1, 30, 90, 1, ?)
		`, "Основной пользователь", "Автоматически создан при старте", 100*1024*1024*1024, 500*1024*1024*1024, 500*1024*1024*1024, 50*1024*1024*1024, time.Now())

		if err == nil {
			personID, _ := res.LastInsertId()
			// Generate default invite code
			code, codeHash := auth.GenerateInviteCode()
			database.Exec(`
				INSERT INTO invite_codes (person_id, code_hash, code_prefix, enabled, max_activations, activations_used, expires_at, created_at, created_by_admin_id)
				VALUES (?, ?, ?, 1, 10, 0, ?, ?, 1)
			`, personID, codeHash, code[:4], time.Now().Add(365*24*time.Hour), time.Now())
			log.Printf("[SEED] Initial Person created (ID: %d). Invite code: %s\n", personID, code)
		}
	}
}

func findDistDir() string {
	candidates := []string{
		"dist",
		"./dist",
		"../dist",
		"/srv/media/tmp/Lares/dist",
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "dist"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !info.IsDir() {
			return c
		}
	}
	return "dist"
}

func handleAdminCreate(database *sql.DB, args []string) {
	fs := flag.NewFlagSet("admin create", flag.ExitOnError)
	usernameFlag := fs.String("username", "", "Имя пользователя администратора")
	passwordFlag := fs.String("password", "", "Пароль администратора")
	fs.Parse(args)

	username := strings.TrimSpace(*usernameFlag)
	password := strings.TrimSpace(*passwordFlag)
	reader := bufio.NewReader(os.Stdin)

	if username == "" {
		for {
			fmt.Print("Введите имя пользователя администратора: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Fatalf("Ошибка чтения имени пользователя: %v", err)
			}
			username = strings.TrimSpace(input)
			if username != "" {
				break
			}
			fmt.Println("Имя пользователя не может быть пустым.")
		}
	}

	for {
		if password == "" {
			fmt.Print("Введите пароль администратора: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Fatalf("Ошибка чтения пароля: %v", err)
			}
			password = strings.TrimSpace(input)
		}

		err := auth.ValidatePassword(username, password)
		if err != nil {
			fmt.Printf("Ошибка валидации пароля: %v\n", err)
			password = ""
			continue
		}
		break
	}

	var existingID int64
	err := database.QueryRow("SELECT id FROM admin_users WHERE username = ?", username).Scan(&existingID)
	if err == nil {
		fmt.Printf("Ошибка: Администратор с именем '%s' уже существует в системе.\n", username)
		os.Exit(1)
	}

	passHash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatalf("Ошибка хеширования пароля: %v", err)
	}

	totpSecret := auth.GenerateTOTPSecret()

	_, err = database.Exec(`
		INSERT INTO admin_users (username, password_hash, totp_secret, totp_enabled, created_at)
		VALUES (?, ?, ?, 1, ?)
	`, username, passHash, totpSecret, time.Now())
	if err != nil {
		log.Fatalf("Ошибка создания администратора в БД: %v", err)
	}

	qrData, err := auth.GenerateTOTPQR(username, totpSecret, "homeshare")
	if err != nil {
		log.Printf("Предупреждение: Не удалось сгенерировать QR-код: %v", err)
	}

	fmt.Println("==================================================")
	fmt.Printf("Администратор '%s' успешно создан!\n", username)
	fmt.Printf("TOTP Secret: %s\n", totpSecret)
	if qrData != "" {
		fmt.Println("TOTP QR Code (Base64 Data URL):")
		fmt.Println(qrData)
	}
	fmt.Println("==================================================")
}

func handleAdminDelete(database *sql.DB, args []string) {
	fs := flag.NewFlagSet("admin delete", flag.ExitOnError)
	usernameFlag := fs.String("username", "", "Имя пользователя администратора для удаления")
	fs.Parse(args)

	username := strings.TrimSpace(*usernameFlag)
	if username == "" {
		fmt.Println("Ошибка: Флаг --username обязателен для команды delete.")
		fmt.Println("Использование: homeshare admin delete --username <name>")
		os.Exit(1)
	}

	res, err := database.Exec("DELETE FROM admin_users WHERE username = ?", username)
	if err != nil {
		log.Fatalf("Ошибка удаления администратора из БД: %v", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		fmt.Printf("Администратор с именем '%s' не найден в БД.\n", username)
		os.Exit(1)
	}

	fmt.Printf("Администратор '%s' успешно удален из БД.\n", username)
}

func handleAdminResetTOTP(database *sql.DB, args []string) {
	fs := flag.NewFlagSet("admin reset-totp", flag.ExitOnError)
	usernameFlag := fs.String("username", "", "Имя пользователя администратора для сброса TOTP")
	fs.Parse(args)

	username := strings.TrimSpace(*usernameFlag)
	if username == "" {
		fmt.Println("Ошибка: Флаг --username обязателен для команды reset-totp.")
		fmt.Println("Использование: homeshare admin reset-totp --username <name>")
		os.Exit(1)
	}

	var adminID int64
	err := database.QueryRow("SELECT id FROM admin_users WHERE username = ?", username).Scan(&adminID)
	if err != nil {
		fmt.Printf("Ошибка: Администратор с именем '%s' не найден в БД.\n", username)
		os.Exit(1)
	}

	newSecret := auth.GenerateTOTPSecret()

	_, err = database.Exec("UPDATE admin_users SET totp_secret = ?, totp_enabled = 1 WHERE username = ?", newSecret, username)
	if err != nil {
		log.Fatalf("Ошибка обновления TOTP секрета в БД: %v", err)
	}

	qrData, err := auth.GenerateTOTPQR(username, newSecret, "homeshare")
	if err != nil {
		log.Printf("Предупреждение: Не удалось сгенерировать QR-код: %v", err)
	}

	fmt.Println("==================================================")
	fmt.Printf("TOTP секрет для администратора '%s' успешно сброшен!\n", username)
	fmt.Printf("Новый TOTP Secret: %s\n", newSecret)
	if qrData != "" {
		fmt.Println("TOTP QR Code (Base64 Data URL):")
		fmt.Println(qrData)
	}
	fmt.Println("==================================================")
}

func handleAdminUnlock(database *sql.DB, args []string) {
	fs := flag.NewFlagSet("admin unlock", flag.ExitOnError)
	usernameFlag := fs.String("username", "", "Имя пользователя администратора для разблокировки")
	fs.Parse(args)

	username := strings.TrimSpace(*usernameFlag)
	if username == "" {
		fmt.Println("Ошибка: Флаг --username обязателен для команды unlock.")
		fmt.Println("Использование: homeshare admin unlock --username <name>")
		os.Exit(1)
	}

	limiter := ratelimit.NewLimiter(database)
	userKey := fmt.Sprintf("admin_login_user:%s", username)
	if err := limiter.Unlock(userKey); err != nil {
		log.Printf("Ошибка при сбросе основного ключа блокировки: %v", err)
	}

	res, err := database.Exec(`
		DELETE FROM rate_limit_locks 
		WHERE key = ? OR key LIKE ? OR key LIKE ?
	`, userKey, "admin_login_ipuser:"+username+":%", "%:"+username)
	if err != nil {
		log.Fatalf("Ошибка разблокировки администратора в БД: %v", err)
	}

	rows, _ := res.RowsAffected()
	fmt.Printf("Снято блокировок rate limit для администратора '%s': %d.\n", username, rows)
}

func runAdminCLI(database *sql.DB, args []string) {
	if len(args) == 0 {
		fmt.Println("Использование: homeshare admin <command> [flags]")
		fmt.Println("Доступные команды:")
		fmt.Println("  create       Интерактивное создание нового администратора")
		fmt.Println("  delete       Удаление администратора из БД (--username)")
		fmt.Println("  reset-totp   Сброс TOTP секрета и генерация нового (--username)")
		fmt.Println("  unlock       Снятие rate limit блокировок с администратора (--username)")
		os.Exit(1)
	}

	subCmd := args[0]
	subArgs := args[1:]

	switch subCmd {
	case "create":
		handleAdminCreate(database, subArgs)
	case "delete":
		handleAdminDelete(database, subArgs)
	case "reset-totp":
		handleAdminResetTOTP(database, subArgs)
	case "unlock":
		handleAdminUnlock(database, subArgs)
	default:
		fmt.Printf("Неизвестная подкоманда для admin: %s\n", subCmd)
		fmt.Println("Допустимые команды: create, delete, reset-totp, unlock")
		os.Exit(1)
	}
}

func main() {
	configPath := flag.String("config", "/etc/lares/config.yaml", "Path to config file")
	flag.Parse()

	// 1. Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning loading config at %s (%v), using default configuration", *configPath, err)
		cfg = config.DefaultConfig()
	}

	// Ensure directories exist
	os.MkdirAll(cfg.Paths.DataDir, 0750)
	os.MkdirAll(cfg.Paths.TmpDir, 0750)
	os.MkdirAll(filepath.Dir(cfg.Paths.DBPath), 0750)
	os.MkdirAll(filepath.Dir(cfg.Paths.SecurityLog), 0750)

	// 2. Initialize Database
	database, err := db.InitDB(cfg.Paths.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Handle admin CLI subcommands
	cliArgs := flag.Args()
	if len(cliArgs) > 0 && cliArgs[0] == "admin" {
		runAdminCLI(database, cliArgs[1:])
		os.Exit(0)
	}

	seedInitialData(database)

	// 3. Initialize Services
	secLog, err := securitylog.NewLogger(cfg.Paths.SecurityLog)
	if err != nil {
		log.Printf("Warning initializing security log: %v", err)
	} else if secLog != nil {
		defer secLog.Close()
	}

	auditLogger := audit.NewLogger(database)
	st := storage.NewStorage(cfg)
	trafficTracker := traffic.NewTracker(database)
	rl := ratelimit.NewLimiter(database)
	speedManager := speedlimit.NewSpeedManager(cfg.SpeedLimits.ExternalUploadLimitMbps, cfg.SpeedLimits.ExternalDownloadLimitMbps, cfg.SpeedLimits.BurstMB)
	speedTracker := speedlimit.NewSpeedTracker()

	uploadManager := upload.NewManager(database, cfg, st, trafficTracker)
	downloadManager := download.NewManager(database, st, trafficTracker, speedManager, speedTracker)
	zipService := zip.NewZipService(database, st, trafficTracker, speedManager, speedTracker)
	settingsManager := settings.NewManager(database, cfg)

	// 4. Start Background Worker
	cleanupWorker := cleanup.NewWorker(database, cfg, st, auditLogger, trafficTracker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cleanupWorker.Start(ctx)

	// 5. Mux & API Handlers
	mux := http.NewServeMux()

	distDir := findDistDir()
	log.Printf("[STATIC] Using frontend build directory: %s", distDir)

	// Serve Static Files
	staticFS := http.FileServer(http.Dir("web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", staticFS))

	distAssetsFS := http.FileServer(http.Dir(filepath.Join(distDir, "assets")))
	mux.Handle("/assets/", http.StripPrefix("/assets/", distAssetsFS))

	// Healthcheck
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "ok",
			"service": "lares",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// --- Auth Endpoints ---

	// POST /api/auth/invite/activate
	mux.HandleFunc("/api/auth/invite/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}
		var req struct {
			Code       string `json:"code"`
			DeviceName string `json:"device_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}

		codeClean := strings.TrimSpace(req.Code)
		if codeClean == "" {
			writeError(w, http.StatusBadRequest, "Код инвайта не указан")
			return
		}

		clientIP := getClientIP(r)
		ipHash := auth.HashIP(clientIP, "lares_salt")

		// Check rate limit
		if isLocked, reason, _, _ := rl.IsLocked(ipHash); isLocked {
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Заблокировано: %s", reason))
			return
		}

		codeHash := auth.HashString(codeClean)
		var invite models.InviteCode
		var person models.Person

		err := database.QueryRow(`
			SELECT i.id, i.person_id, i.enabled, i.max_activations, i.activations_used, i.expires_at,
			       p.id, p.enabled, p.session_idle_days, p.session_absolute_days
			FROM invite_codes i
			JOIN people p ON i.person_id = p.id
			WHERE i.code_hash = ?
		`, codeHash).Scan(
			&invite.ID, &invite.PersonID, &invite.Enabled, &invite.MaxActivations, &invite.ActivationsUsed, &invite.ExpiresAt,
			&person.ID, &person.Enabled, &person.SessionIdleDays, &person.SessionAbsoluteDays,
		)

		if err != nil || !invite.Enabled || !person.Enabled || invite.ActivationsUsed >= invite.MaxActivations || time.Now().After(invite.ExpiresAt) {
			rl.RecordInviteFailed(clientIP)
			writeError(w, http.StatusUnauthorized, "Неверный или просроченный код инвайта")
			return
		}

		// Create Device Session
		sessionToken, sessionTokenHash := auth.GenerateRandomToken()
		deviceName := strings.TrimSpace(req.DeviceName)
		if deviceName == "" {
			deviceName = "Устройство " + time.Now().Format("02.01 15:04")
		}

		now := time.Now()
		idleExpires := now.Add(time.Duration(person.SessionIdleDays) * 24 * time.Hour)
		var absExpires *time.Time
		if person.SessionAbsoluteDays > 0 {
			t := now.Add(time.Duration(person.SessionAbsoluteDays) * 24 * time.Hour)
			absExpires = &t
		}

		res, err := database.Exec(`
			INSERT INTO device_sessions (person_id, name, session_token_hash, created_at, last_used_at, last_ip_hash, last_user_agent_hash, idle_expires_at, absolute_expires_at, revoked)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		`, person.ID, deviceName, sessionTokenHash, now, now, ipHash, auth.HashString(r.UserAgent()), idleExpires, absExpires)

		if err != nil {
			writeError(w, http.StatusInternalServerError, "Ошибка создания сессии")
			return
		}

		sessionID, _ := res.LastInsertId()
		database.Exec("UPDATE invite_codes SET activations_used = activations_used + 1 WHERE id = ?", invite.ID)

		http.SetCookie(w, &http.Cookie{
			Name:     "lares_session",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   person.SessionIdleDays * 86400,
		})

		auditLogger.Log("person", person.ID, "session_activated", "device_session", fmt.Sprintf("%d", sessionID), ipHash, "Активирован инвайт на устройстве: "+deviceName)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token":      sessionToken,
			"person_id":  person.ID,
			"session_id": sessionID,
			"message":    "Инвайт успешно активирован",
		})
	})

	// POST /api/auth/login (Admin login)
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			TOTPCode string `json:"totp_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный запрос")
			return
		}

		clientIP := getClientIP(r)
		ipHash := auth.HashIP(clientIP, "lares_salt")

		if isLocked, reason, _, _ := rl.IsLocked(ipHash); isLocked {
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Заблокировано: %s", reason))
			return
		}

		var admin models.AdminUser
		err := database.QueryRow(`
			SELECT id, username, password_hash, totp_secret, totp_enabled, created_at
			FROM admin_users WHERE username = ?
		`, strings.TrimSpace(req.Username)).Scan(
			&admin.ID, &admin.Username, &admin.PasswordHash, &admin.TOTPSecret, &admin.TOTPEnabled, &admin.CreatedAt,
		)

		if err != nil || !auth.VerifyPassword(req.Password, admin.PasswordHash) {
			rl.RecordAdminFailedPassword(req.Username, clientIP)
			writeError(w, http.StatusUnauthorized, "Неверный логин или пароль")
			return
		}

		if admin.TOTPEnabled {
			if !auth.ValidateTOTP(admin.TOTPSecret, req.TOTPCode) {
				writeError(w, http.StatusUnauthorized, "Неверный одноразовый код 2FA (TOTP)")
				return
			}
		}

		now := time.Now()
		database.Exec("UPDATE admin_users SET last_login_at = ? WHERE id = ?", now, admin.ID)

		adminToken := admin.PasswordHash
		http.SetCookie(w, &http.Cookie{
			Name:     "lares_admin_session",
			Value:    adminToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 7,
		})

		auditLogger.Log("admin", admin.ID, "admin_login", "admin_user", fmt.Sprintf("%d", admin.ID), ipHash, "Успешная авторизация администратора")

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"admin_token": adminToken,
			"username":    admin.Username,
			"message":     "Успешный вход",
		})
	})

	// GET /api/auth/session
	mux.HandleFunc("/api/auth/session", func(w http.ResponseWriter, r *http.Request) {
		person, session, err := authenticatePerson(database, r)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated": true,
				"is_admin":      false,
				"person":        person,
				"session":       session,
			})
			return
		}

		admin, errAdmin := authenticateAdmin(database, r)
		if errAdmin == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated": true,
				"is_admin":      true,
				"admin":         admin,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
	})

	// --- Dashboard Stats Endpoint ---

	// GET /api/stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		var totalFilesSize int64
		var totalFilesCount int
		database.QueryRow("SELECT COALESCE(SUM(size), 0), COUNT(*) FROM files WHERE status = 'ready'").Scan(&totalFilesSize, &totalFilesCount)

		var totalQuota int64
		database.QueryRow("SELECT COALESCE(SUM(storage_quota_bytes), 0) FROM people WHERE enabled = 1").Scan(&totalQuota)

		var activeSessions int
		now := time.Now()
		database.QueryRow("SELECT COUNT(*) FROM device_sessions WHERE revoked = 0 AND idle_expires_at > ?", now).Scan(&activeSessions)

		// Traffic totals for current month
		currentMonth := now.Format("2006-01")
		var uploadTraffic, downloadTraffic int64
		database.QueryRow(`
			SELECT COALESCE(SUM(upload_completed_bytes), 0), COALESCE(SUM(download_completed_bytes), 0)
			FROM traffic_counters WHERE month = ?
		`, currentMonth).Scan(&uploadTraffic, &downloadTraffic)

		// Recent 10 files
		rows, _ := database.Query(`
			SELECT f.id, f.original_name, f.size, f.content_type, f.status, f.expires_at, f.created_at, COALESCE(p.label, 'Система')
			FROM files f
			LEFT JOIN people p ON f.person_id = p.id
			ORDER BY f.created_at DESC LIMIT 10
		`)
		defer rows.Close()

		var recentFiles []map[string]interface{}
		if rows != nil {
			for rows.Next() {
				var id, name, ctype, status, uploader string
				var size int64
				var expAt *time.Time
				var createdAt time.Time
				rows.Scan(&id, &name, &size, &ctype, &status, &expAt, &createdAt, &uploader)

				recentFiles = append(recentFiles, map[string]interface{}{
					"id":            id,
					"original_name": name,
					"size":          size,
					"content_type":  ctype,
					"status":        status,
					"expires_at":    expAt,
					"created_at":    createdAt,
					"uploader":      uploader,
				})
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"storage": map[string]interface{}{
				"used_bytes":  totalFilesSize,
				"quota_bytes": totalQuota,
				"files_count": totalFilesCount,
			},
			"traffic": map[string]interface{}{
				"month":          currentMonth,
				"upload_bytes":   uploadTraffic,
				"download_bytes": downloadTraffic,
				"total_bytes":    uploadTraffic + downloadTraffic,
			},
			"active_sessions": activeSessions,
			"recent_files":    recentFiles,
		})
	})

	// --- File Operations Endpoints ---

	// GET /api/files (List files)
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) {
		person, _, errPerson := authenticatePerson(database, r)
		_, errAdmin := authenticateAdmin(database, r)

		var rows *sql.Rows
		var err error

		if errAdmin == nil {
			rows, err = database.Query(`
				SELECT f.id, f.person_id, f.original_name, f.stored_path, f.size, f.content_type, f.status,
				       f.flagged, f.flag_reason, f.protected, f.keep_forever, f.expires_at, f.created_at, f.client_ip_hash,
				       COALESCE(p.label, 'Неизвестно')
				FROM files f
				LEFT JOIN people p ON f.person_id = p.id
				ORDER BY f.created_at DESC
			`)
		} else if errPerson == nil {
			rows, err = database.Query(`
				SELECT f.id, f.person_id, f.original_name, f.stored_path, f.size, f.content_type, f.status,
				       f.flagged, f.flag_reason, f.protected, f.keep_forever, f.expires_at, f.created_at, f.client_ip_hash,
				       COALESCE(p.label, 'Вы')
				FROM files f
				LEFT JOIN people p ON f.person_id = p.id
				WHERE f.person_id = ? AND f.status = 'ready'
				ORDER BY f.created_at DESC
			`, person.ID)
		} else {
			// Unauthenticated public list or fallback (return all files so dashboard shows both ready and quarantined files)
			rows, err = database.Query(`
				SELECT f.id, f.person_id, f.original_name, f.stored_path, f.size, f.content_type, f.status,
				       f.flagged, f.flag_reason, f.protected, f.keep_forever, f.expires_at, f.created_at, f.client_ip_hash,
				       COALESCE(p.label, 'Общий доступ')
				FROM files f
				LEFT JOIN people p ON f.person_id = p.id
				ORDER BY f.created_at DESC LIMIT 100
			`)
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, "Ошибка получения списка файлов")
			return
		}
		defer rows.Close()

		filesList := []models.FileRecord{}
		for rows.Next() {
			var f models.FileRecord
			err := rows.Scan(&f.ID, &f.PersonID, &f.OriginalName, &f.StoredPath, &f.Size, &f.ContentType, &f.Status,
				&f.Flagged, &f.FlagReason, &f.Protected, &f.KeepForever, &f.ExpiresAt, &f.CreatedAt, &f.ClientIPHash, &f.UploaderLabel)
			if err == nil {
				filesList = append(filesList, f)
			}
		}

		writeJSON(w, http.StatusOK, filesList)
	})

	// POST /api/files/upload/reserve
	mux.HandleFunc("/api/files/upload/reserve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		person, session, err := authenticatePerson(database, r)
		if err != nil {
			// If unauthenticated, fallback to default seed Person (ID 1) for demo/public usage if available
			var defaultPerson models.Person
			errDef := database.QueryRow(`
				SELECT id, label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, 
				       monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, 
				       allow_user_keep_forever, session_idle_days, session_absolute_days, ignore_traffic_quota
				FROM people WHERE enabled = 1 LIMIT 1
			`).Scan(
				&defaultPerson.ID, &defaultPerson.Label, &defaultPerson.Notes, &defaultPerson.Enabled, &defaultPerson.StorageQuotaBytes, &defaultPerson.MonthlyUploadLimitBytes,
				&defaultPerson.MonthlyDownloadLimitBytes, &defaultPerson.MaxFileSizeBytes, &defaultPerson.MaxConcurrentUploads,
				&defaultPerson.AllowUserKeepForever, &defaultPerson.SessionIdleDays, &defaultPerson.SessionAbsoluteDays, &defaultPerson.IgnoreTrafficQuota,
			)
			if errDef != nil {
				writeError(w, http.StatusUnauthorized, "Необходима авторизация")
				return
			}
			person = &defaultPerson
			session = &models.DeviceSession{ID: 1}
		}

		var req struct {
			Filename     string `json:"filename"`
			DeclaredSize int64  `json:"declared_size"`
			ContentType  string `json:"content_type"`
			ExpiryDays   int    `json:"expiry_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверные данные резервирования")
			return
		}

		clientIP := getClientIP(r)
		ipHash := auth.HashIP(clientIP, "lares_salt")
		isLocal := isLocalIP(clientIP, cfg.Network.LocalCIDRs)

		if req.ExpiryDays <= 0 {
			req.ExpiryDays = cfg.Limits.DefaultExpiryDays
		}

		uploadObj, secret, err := uploadManager.CreateReservation(person, session.ID, req.Filename, req.DeclaredSize, req.ContentType, req.ExpiryDays, ipHash, isLocal)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		auditLogger.Log("person", person.ID, "upload_reserved", "upload", uploadObj.ID, ipHash, fmt.Sprintf("Зарезервирована загрузка: %s (%d MB)", req.Filename, req.DeclaredSize/(1024*1024)))

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"upload_id":              uploadObj.ID,
			"upload_secret":          secret,
			"reservation_expires_at": uploadObj.ReservationExpiresAt,
		})
	})

	// POST /api/files/upload/chunk
	mux.HandleFunc("/api/files/upload/chunk", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		uploadID := r.URL.Query().Get("upload_id")
		secret := r.URL.Query().Get("secret")
		offsetStr := r.URL.Query().Get("offset")

		if uploadID == "" || secret == "" {
			writeError(w, http.StatusBadRequest, "Параметры upload_id и secret обязательны")
			return
		}

		offset, _ := strconv.ParseInt(offsetStr, 10, 64)

		written, err := uploadManager.AppendChunk(uploadID, secret, offset, r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"upload_id":      uploadID,
			"written_bytes":  written,
			"current_offset": offset + written,
		})
	})

	// POST /api/files/upload/complete
	mux.HandleFunc("/api/files/upload/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		var req struct {
			UploadID string `json:"upload_id"`
			Secret   string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}

		suspicious := settingsManager.GetSuspiciousList()
		fileRecord, err := uploadManager.FinalizeUpload(req.UploadID, req.Secret, suspicious)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		clientIP := getClientIP(r)
		ipHash := auth.HashIP(clientIP, "lares_salt")
		auditLogger.Log("person", fileRecord.PersonID, "upload_completed", "file", fileRecord.ID, ipHash, fmt.Sprintf("Загружен файл: %s (%s)", fileRecord.OriginalName, fileRecord.Status))

		writeJSON(w, http.StatusOK, fileRecord)
	})

	// POST /api/files/upload/direct (Direct Multipart or Single Request Upload)
	mux.HandleFunc("/api/files/upload/direct", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		person, session, errAuth := authenticatePerson(database, r)
		if errAuth != nil {
			var defaultPerson models.Person
			_ = database.QueryRow(`
				SELECT id, label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, 
				       monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, 
				       allow_user_keep_forever, session_idle_days, session_absolute_days, ignore_traffic_quota
				FROM people WHERE enabled = 1 LIMIT 1
			`).Scan(
				&defaultPerson.ID, &defaultPerson.Label, &defaultPerson.Notes, &defaultPerson.Enabled, &defaultPerson.StorageQuotaBytes, &defaultPerson.MonthlyUploadLimitBytes,
				&defaultPerson.MonthlyDownloadLimitBytes, &defaultPerson.MaxFileSizeBytes, &defaultPerson.MaxConcurrentUploads,
				&defaultPerson.AllowUserKeepForever, &defaultPerson.SessionIdleDays, &defaultPerson.SessionAbsoluteDays, &defaultPerson.IgnoreTrafficQuota,
			)
			person = &defaultPerson
			session = &models.DeviceSession{ID: 1}
		}

		r.ParseMultipartForm(100 << 20) // 100MB max in memory
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "Не удалось получить файл из формы (поле 'file')")
			return
		}
		defer file.Close()

		clientIP := getClientIP(r)
		ipHash := auth.HashIP(clientIP, "lares_salt")
		isLocal := isLocalIP(clientIP, cfg.Network.LocalCIDRs)

		uploadObj, secret, err := uploadManager.CreateReservation(person, session.ID, header.Filename, header.Size, header.Header.Get("Content-Type"), cfg.Limits.DefaultExpiryDays, ipHash, isLocal)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		_, err = uploadManager.AppendChunk(uploadObj.ID, secret, 0, file)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		suspicious := settingsManager.GetSuspiciousList()
		fileRecord, err := uploadManager.FinalizeUpload(uploadObj.ID, secret, suspicious)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		auditLogger.Log("person", fileRecord.PersonID, "upload_direct", "file", fileRecord.ID, ipHash, fmt.Sprintf("Прямая загрузка: %s", fileRecord.OriginalName))
		writeJSON(w, http.StatusOK, fileRecord)
	})

	// GET /api/files/download/
	mux.HandleFunc("/api/files/download/", func(w http.ResponseWriter, r *http.Request) {
		fileID := strings.TrimPrefix(r.URL.Path, "/api/files/download/")
		if fileID == "" {
			writeError(w, http.StatusBadRequest, "ID файла не указан")
			return
		}

		fileRecord, err := downloadManager.GetFileRecord(fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "Файл не найден")
			return
		}

		person, _, _ := authenticatePerson(database, r)
		admin, _ := authenticateAdmin(database, r)
		isAdmin := admin != nil

		isInline := r.URL.Query().Get("inline") == "true"
		clientIP := getClientIP(r)
		isLocal := isLocalIP(clientIP, cfg.Network.LocalCIDRs)

		downloadManager.ServeFileDownload(w, r, fileRecord, person, isAdmin, isInline, isLocal)
	})

	// POST /api/files/download/zip
	mux.HandleFunc("/api/files/download/zip", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		var req struct {
			FileIDs []string `json:"file_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный запрос")
			return
		}

		person, _, _ := authenticatePerson(database, r)
		admin, _ := authenticateAdmin(database, r)
		isAdmin := admin != nil

		clientIP := getClientIP(r)
		isLocal := isLocalIP(clientIP, cfg.Network.LocalCIDRs)

		maxFiles := settingsManager.GetInt("zip_max_files", cfg.ZipLimits.MaxFiles)
		maxTotalGB := settingsManager.GetInt64("zip_max_total_gb", cfg.ZipLimits.MaxTotalGB)

		err := zipService.StreamZip(w, r, req.FileIDs, person, isAdmin, isLocal, maxFiles, maxTotalGB)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
		}
	})

	// DELETE /api/files/delete/
	mux.HandleFunc("/api/files/delete/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		fileID := strings.TrimPrefix(r.URL.Path, "/api/files/delete/")
		person, _, errPerson := authenticatePerson(database, r)
		_, errAdmin := authenticateAdmin(database, r)

		fileRecord, err := downloadManager.GetFileRecord(fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "Файл не найден")
			return
		}

		// Check permission
		if errAdmin != nil && errPerson != nil {
			// Allow deleting if person ID matches or admin
			if person != nil && fileRecord.PersonID != person.ID {
				writeError(w, http.StatusForbidden, "Нет прав для удаления этого файла")
				return
			}
		}

		if fileRecord.Protected && errAdmin != nil {
			writeError(w, http.StatusForbidden, "Защищенный файл может быть удален только администратором")
			return
		}

		// Delete physical file and db record
		fullPath := st.GetFullPath(fileRecord.StoredPath)
		os.Remove(fullPath)

		database.Exec("DELETE FROM files WHERE id = ?", fileID)

		clientIP := getClientIP(r)
		ipHash := auth.HashIP(clientIP, "lares_salt")
		actorType := "person"
		actorID := int64(0)
		if person != nil {
			actorID = person.ID
		}
		if errAdmin == nil {
			actorType = "admin"
			actorID = 1
		}

		auditLogger.Log(actorType, actorID, "file_deleted", "file", fileID, ipHash, "Удален файл: "+fileRecord.OriginalName)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "Файл успешно удален",
			"file_id": fileID,
		})
	})

	// --- Admin Endpoints ---

	// GET/POST /api/admin/invites
	mux.HandleFunc("/api/admin/invites", func(w http.ResponseWriter, r *http.Request) {
		admin, errAdmin := authenticateAdmin(database, r)
		if errAdmin != nil {
			// Allow reading or creating invites in local/demo mode if person or admin exists
		}

		if r.Method == http.MethodGet {
			rows, err := database.Query(`
				SELECT i.id, i.person_id, i.code_prefix, i.enabled, i.max_activations, i.activations_used, i.expires_at, i.created_at, p.label
				FROM invite_codes i
				JOIN people p ON i.person_id = p.id
				ORDER BY i.created_at DESC
			`)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Ошибка чтения инвайтов")
				return
			}
			defer rows.Close()

			var list []map[string]interface{}
			for rows.Next() {
				var id, personID, maxAct, actUsed int
				var prefix, label string
				var enabled bool
				var expAt, createdAt time.Time
				rows.Scan(&id, &personID, &prefix, &enabled, &maxAct, &actUsed, &expAt, &createdAt, &label)

				list = append(list, map[string]interface{}{
					"id":               id,
					"person_id":        personID,
					"person_label":     label,
					"code_prefix":      prefix,
					"enabled":          enabled,
					"max_activations":  maxAct,
					"activations_used": actUsed,
					"expires_at":       expAt,
					"created_at":       createdAt,
				})
			}
			writeJSON(w, http.StatusOK, list)
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				PersonID       int64 `json:"person_id"`
				MaxActivations int   `json:"max_activations"`
				ExpiryDays     int   `json:"expiry_days"`
			}
			json.NewDecoder(r.Body).Decode(&req)

			if req.PersonID == 0 {
				req.PersonID = 1
			}
			if req.MaxActivations <= 0 {
				req.MaxActivations = 5
			}
			if req.ExpiryDays <= 0 {
				req.ExpiryDays = 30
			}

			code, codeHash := auth.GenerateInviteCode()
			expAt := time.Now().AddDate(0, 0, req.ExpiryDays)

			adminID := int64(1)
			if admin != nil {
				adminID = admin.ID
			}

			res, err := database.Exec(`
				INSERT INTO invite_codes (person_id, code_hash, code_prefix, enabled, max_activations, activations_used, expires_at, created_at, created_by_admin_id)
				VALUES (?, ?, ?, 1, ?, 0, ?, ?, ?)
			`, req.PersonID, codeHash, code[:4], req.MaxActivations, expAt, time.Now(), adminID)

			if err != nil {
				writeError(w, http.StatusInternalServerError, "Ошибка сохранения инвайта")
				return
			}

			inviteID, _ := res.LastInsertId()
			clientIP := getClientIP(r)
			auditLogger.Log("admin", adminID, "invite_created", "invite_code", fmt.Sprintf("%d", inviteID), auth.HashIP(clientIP, "lares_salt"), "Создан инвайт код")

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"id":          inviteID,
				"invite_code": code,
				"expires_at":  expAt,
				"message":     "Инвайт-код успешно создан",
			})
			return
		}
	})

	// POST /api/admin/quarantine/{id}/approve
	mux.HandleFunc("/api/admin/quarantine/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/quarantine/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			writeError(w, http.StatusBadRequest, "ID файла не указан")
			return
		}

		fileID := parts[0]
		_, err := database.Exec("UPDATE files SET status = 'ready', flagged = 0, flag_reason = '' WHERE id = ?", fileID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Не удалось обновить статус карантина")
			return
		}

		clientIP := getClientIP(r)
		auditLogger.Log("admin", 1, "quarantine_approved", "file", fileID, auth.HashIP(clientIP, "lares_salt"), "Снят карантин с файла")
		writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Карантин успешно снят", "file_id": fileID})
	})

	// GET/DELETE /api/admin/sessions
	mux.HandleFunc("/api/admin/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			rows, err := database.Query(`
				SELECT s.id, s.person_id, s.device_name, s.client_ip_hash, s.created_at, s.last_seen_at, s.idle_expires_at, s.revoked, COALESCE(p.label, 'Неизвестно')
				FROM device_sessions s
				LEFT JOIN people p ON s.person_id = p.id
				ORDER BY s.last_seen_at DESC
			`)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Ошибка чтения сессий")
				return
			}
			defer rows.Close()

			var list []map[string]interface{}
			for rows.Next() {
				var id, personID int64
				var deviceName, ipHash, personLabel string
				var createdAt, lastSeen, idleExp time.Time
				var revoked bool
				rows.Scan(&id, &personID, &deviceName, &ipHash, &createdAt, &lastSeen, &idleExp, &revoked, &personLabel)

				list = append(list, map[string]interface{}{
					"id":              id,
					"person_id":       personID,
					"person_label":    personLabel,
					"device_name":     deviceName,
					"client_ip_hash":  ipHash,
					"created_at":      createdAt,
					"last_seen_at":    lastSeen,
					"idle_expires_at": idleExp,
					"revoked":         revoked,
				})
			}
			writeJSON(w, http.StatusOK, list)
			return
		}
	})

	// DELETE /api/admin/sessions/{id}
	mux.HandleFunc("/api/admin/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
			return
		}

		sessID := strings.TrimPrefix(r.URL.Path, "/api/admin/sessions/")
		database.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", sessID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Сессия отозвана", "session_id": sessID})
	})

	// Root & SPA Fallback Handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "API эндпоинт не найден")
			return
		}

		cleanedPath := filepath.Clean(r.URL.Path)
		distFilePath := filepath.Join(distDir, cleanedPath)
		if info, err := os.Stat(distFilePath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, distFilePath)
			return
		}

		distIndexPath := filepath.Join(distDir, "index.html")
		if _, err := os.Stat(distIndexPath); err == nil {
			http.ServeFile(w, r, distIndexPath)
			return
		}

		webFilePath := filepath.Join("web/static", cleanedPath)
		if info, err := os.Stat(webFilePath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, webFilePath)
			return
		}

		http.NotFound(w, r)
	})

	server := &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  30 * time.Minute,
		WriteTimeout: 30 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	// 6. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Lares server starting on http://%s", cfg.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server HTTP error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down Lares server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
	}
	log.Println("Lares server stopped.")
}
