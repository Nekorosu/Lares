package api

import (
	"context"
	"database/sql"
	"fmt"
	"lares/internal/auth"
	"lares/internal/models"
	"lares/internal/netutils"
	"net/http"
	"strings"
	"time"
)

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	page := "login"
	if r.URL.Path == "/admin/login" {
		page = "admin_login"
	}
	s.render(w, r, page, nil)
}
func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	sess, p, a := s.getSession(r)
	if sess == nil {
		jsonOut(w, 200, map[string]any{"authenticated": false})
		return
	}
	role, name := "user", ""
	var id int64
	if p != nil {
		name = p.Label
		id = p.ID
	}
	if a != nil {
		role = "admin"
		name = a.Username
	}
	jsonOut(w, 200, map[string]any{"authenticated": true, "role": role, "username": name, "person_id": id})
}
func (s *Server) loginLocked(w http.ResponseWriter, r *http.Request, keys ...string) bool {
	for _, k := range keys {
		if locked, d, _ := s.rateLimiter.IsLocked(k); locked {
			s.limited(w, r, k, d)
			return true
		}
	}
	return false
}
func (s *Server) authFailure(r *http.Request, event, username string) error {
	ip := netutils.GetClientIP(r)
	hash := auth.HashWithSalt(ip, s.cfg.Secrets.IPHashSalt)
	s.securityLog.LogEvent(event, ip, "неудачный вход")
	type rule struct {
		key              string
		window, duration time.Duration
		count            int
	}
	rules := []rule{}
	if event == "invite_failed" {
		rules = []rule{{"invite:ip:" + hash, time.Hour, 24 * time.Hour, 30}, {"invite:short:" + hash, 15 * time.Minute, time.Hour, 5}}
	} else {
		rules = []rule{{"admin:" + username + ":" + event + ":" + hash, 15 * time.Minute, 15 * time.Minute, 5}, {"admin:" + username + ":all", time.Hour, time.Hour, 20}}
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	for _, v := range rules {
		eventKey := v.key
		distributed := strings.HasSuffix(v.key, ":all")
		if distributed {
			eventKey += ":" + hash
		}
		if _, e = tx.ExecContext(r.Context(), "INSERT INTO request_events(key,time) VALUES(?,?)", eventKey, now); e != nil {
			return e
		}
		var n int
		distinct := 1
		if distributed {
			prefix := v.key + ":"
			e = tx.QueryRowContext(r.Context(), "SELECT count(*),count(DISTINCT key) FROM request_events WHERE substr(key,1,?)=? AND time>?", len(prefix), prefix, now.Add(-v.window)).Scan(&n, &distinct)
		} else {
			e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM request_events WHERE key=? AND time>?", v.key, now.Add(-v.window)).Scan(&n)
		}
		if e != nil {
			return e
		}
		if distributed && distinct < 2 {
			continue
		}
		if n >= v.count {
			if _, e = tx.ExecContext(r.Context(), "INSERT INTO rate_limit_locks(key,type,reason,expires_at,created_at) VALUES(?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET expires_at=excluded.expires_at", v.key, event, "Неудачные попытки входа", now.Add(v.duration), now); e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return s.audit(r, event, "authentication", "", "Неудачный вход")
}
func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp_code"`
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if decode(w, r, &req) != nil {
			fail(w, r, 400, "Неверный запрос")
			return
		}
	} else {
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
		req.TOTP = r.FormValue("totp_code")
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) > 128 || len(req.Password) > 1024 {
		fail(w, r, 400, "Неверные учётные данные")
		return
	}
	ipHash := auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt)
	prefix := "admin:" + req.Username
	if s.loginLocked(w, r, prefix+":all", prefix+":admin_login_failed:"+ipHash, prefix+":admin_totp_failed:"+ipHash) {
		return
	}
	var a models.AdminUser
	var usedStep int64
	e := s.db.QueryRowContext(r.Context(), "SELECT id,username,password_hash,totp_secret,last_totp_step FROM admin_users WHERE username=?", req.Username).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.TOTPSecret, &usedStep)
	valid := false
	if e == nil {
		valid, e = auth.VerifyPassword(req.Password, a.PasswordHash)
	}
	if !valid || e != nil {
		if e = s.authFailure(r, "admin_login_failed", req.Username); e != nil {
			fail(w, r, 503, "Ошибка проверки входа")
			return
		}
		fail(w, r, 401, "Неверные учётные данные")
		return
	}
	step := auth.TOTPStep(a.TOTPSecret, strings.TrimSpace(req.TOTP))
	if step == 0 || step <= usedStep {
		if e = s.authFailure(r, "admin_totp_failed", req.Username); e != nil {
			fail(w, r, 503, "Ошибка проверки входа")
			return
		}
		fail(w, r, 401, "Неверный или уже использованный код TOTP")
		return
	}
	token := auth.GenerateRandomToken(32)
	now := time.Now().UTC()
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, r, 503, "Ошибка входа")
		return
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(r.Context(), "UPDATE admin_users SET last_totp_step=?,last_login_at=? WHERE id=? AND last_totp_step<?", step, now, a.ID, step)
	if e != nil {
		fail(w, r, 503, "Ошибка входа")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		fail(w, r, 401, "Код TOTP уже использован")
		return
	}
	if e = s.insertSession(r.Context(), tx, nil, &a.ID, "Администратор", token, now.Add(time.Duration(s.cfg.SessionDefaults.AdminIdleHours)*time.Hour), now.Add(time.Duration(s.cfg.SessionDefaults.AdminAbsoluteDays)*24*time.Hour), r); e != nil {
		fail(w, r, 503, "Ошибка создания сессии")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, r, 503, "Ошибка сохранения сессии")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, &identity{Admin: &a}))
	if e = s.audit(r, "admin_login", "admin", fmt.Sprint(a.ID), "Вход администратора"); e != nil {
		fail(w, r, 503, "Ошибка журнала")
		return
	}
	s.sessionCookie(w, r, token)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		jsonOut(w, 200, map[string]string{"role": "admin"})
	} else {
		http.Redirect(w, r, "/admin/dashboard", 303)
	}
}
func (s *Server) insertSession(ctx context.Context, tx *sql.Tx, pid, aid *int64, name, token string, idle time.Time, absolute any, r *http.Request) error {
	now := time.Now().UTC()
	_, e := tx.ExecContext(ctx, `INSERT INTO device_sessions(person_id,admin_id,is_admin,name,session_token_hash,created_at,last_used_at,last_ip_hash,last_user_agent_hash,idle_expires_at,absolute_expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, pid, aid, aid != nil, name, auth.HashWithSalt(token, s.cfg.Secrets.SessionSecret), now, now, auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt), auth.HashWithSalt(r.UserAgent(), s.cfg.Secrets.IPHashSalt), idle, absolute)
	return e
}
func (s *Server) sessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: "homeshare_session", Value: token, Path: "/", HttpOnly: true, Secure: netutils.IsHTTPS(r), SameSite: http.SameSiteLaxMode})
}
func (s *Server) inviteLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code   string `json:"code"`
		Device string `json:"device_name"`
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if decode(w, r, &req) != nil {
			fail(w, r, 400, "Неверный запрос")
			return
		}
	} else {
		req.Code = r.FormValue("invite_code")
		req.Device = r.FormValue("device_name")
	}
	hash := auth.HashWithSalt(netutils.GetClientIP(r), s.cfg.Secrets.IPHashSalt)
	if s.loginLocked(w, r, "invite:ip:"+hash, "invite:short:"+hash) {
		return
	}
	code := auth.NormalizeInviteCode(req.Code)
	if len(code) > 128 {
		code = "invalid"
	}
	now := time.Now().UTC()
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, r, 503, "Ошибка активации")
		return
	}
	defer tx.Rollback()
	var id, pid int64
	var idle, absolute int
	e = tx.QueryRowContext(r.Context(), `SELECT i.id,i.person_id,p.session_idle_days,p.session_absolute_days FROM invite_codes i JOIN people p ON p.id=i.person_id WHERE i.code_hash=? AND i.enabled=1 AND i.activations_used<i.max_activations AND i.expires_at>? AND p.enabled=1 AND p.is_orphan=0`, auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt), now).Scan(&id, &pid, &idle, &absolute)
	if e != nil {
		tx.Rollback()
		if e = s.authFailure(r, "invite_failed", ""); e != nil {
			fail(w, r, 503, "Ошибка проверки входа")
			return
		}
		fail(w, r, 400, "Неверный, истёкший или использованный инвайт")
		return
	}
	res, e := tx.ExecContext(r.Context(), "UPDATE invite_codes SET activations_used=activations_used+1,enabled=(activations_used+1<max_activations) WHERE id=? AND activations_used<max_activations AND enabled=1", id)
	if e != nil {
		fail(w, r, 503, "Ошибка активации")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		fail(w, r, 400, "Инвайт уже использован")
		return
	}
	var abs any
	if absolute > 0 {
		abs = now.Add(time.Duration(absolute) * 24 * time.Hour)
	}
	name := strings.TrimSpace(req.Device)
	if name == "" {
		name = "Браузер"
	}
	name = string([]rune(name)[:min(len([]rune(name)), 80)])
	token := auth.GenerateRandomToken(32)
	if e = s.insertSession(r.Context(), tx, &pid, nil, name, token, now.Add(time.Duration(idle)*24*time.Hour), abs, r); e != nil {
		fail(w, r, 503, "Ошибка создания сессии")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, r, 503, "Ошибка сохранения")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, &identity{Person: &models.Person{ID: pid}}))
	if e = s.audit(r, "invite_activated", "invite", fmt.Sprint(id), "Устройство добавлено"); e != nil {
		fail(w, r, 503, "Ошибка журнала")
		return
	}
	s.sessionCookie(w, r, token)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		jsonOut(w, 200, map[string]string{"status": "ok"})
	} else {
		http.Redirect(w, r, "/", 303)
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sess, _, _ := s.getSession(r)
	if _, e := s.db.ExecContext(r.Context(), "UPDATE device_sessions SET revoked=1 WHERE id=?", sess.ID); e != nil {
		fail(w, r, 503, "Ошибка выхода")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "homeshare_session", Value: "", Path: "/", HttpOnly: true, Secure: netutils.IsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if e := s.audit(r, "logout", "session", fmt.Sprint(sess.ID), "Выход с устройства"); e != nil {
		s.logError(e)
	}
	http.Redirect(w, r, "/login", 303)
}
