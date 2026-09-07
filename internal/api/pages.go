package api

import (
	"context"
	"database/sql"
	"fmt"
	"lares/internal/traffic"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) query(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	result := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if e = rows.Scan(ptrs...); e != nil {
			return nil, e
		}
		row := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			row[c] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
func pagination(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if p < 1 {
		p = 1
	}
	if p > 1000000 {
		p = 1000000
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return p, size
}
func (s *Server) fileList(r *http.Request, admin bool) ([]map[string]any, error) {
	page, size := pagination(r)
	q := `SELECT id,person_id,uploader_name,original_name,size,content_type,status,flagged,flag_reason,protected,keep_forever,expires_at,created_at FROM files WHERE (keep_forever=1 OR expires_at IS NULL OR expires_at>?)`
	args := []any{time.Now().UTC()}
	_, p, _ := s.getSession(r)
	if !admin {
		q += " AND (status='ready' OR person_id=?)"
		args = append(args, p.ID)
	}
	if search := r.URL.Query().Get("q"); search != "" {
		q += " AND original_name LIKE ? ESCAPE '\\'"
		search = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search)
		args = append(args, "%"+search+"%")
	}
	switch status := r.URL.Query().Get("status"); status {
	case "ready", "quarantined":
		q += " AND status=?"
		args = append(args, status)
	case "flagged":
		q += " AND flagged=1"
	case "protected":
		q += " AND protected=1"
	}
	order := map[string]string{"name": "original_name", "size": "size", "date": "created_at"}[r.URL.Query().Get("sort")]
	if order == "" {
		order = "created_at"
	}
	direction := " DESC"
	if r.URL.Query().Get("direction") == "asc" {
		direction = " ASC"
	}
	q += " ORDER BY " + order + direction + ",id LIMIT ? OFFSET ?"
	args = append(args, size, (page-1)*size)
	rows, e := s.query(r.Context(), q, args...)
	if e != nil {
		return nil, e
	}
	for _, f := range rows {
		f["preview"] = safeMedia(fmt.Sprint(f["original_name"]), fmt.Sprint(f["content_type"]))
		f["can_delete"] = admin || (p != nil && fmt.Sprint(f["person_id"]) == fmt.Sprint(p.ID) && !dbBool(f["protected"]))
	}
	return rows, nil
}
func dbBool(v any) bool { return v == true || fmt.Sprint(v) == "1" }
func (s *Server) pageData(r *http.Request) map[string]any {
	p, n := pagination(r)
	q := r.URL.Query()
	prev, next := url.Values{}, url.Values{}
	for k, v := range q {
		prev[k] = v
		next[k] = v
	}
	prev.Set("page", fmt.Sprint(max(1, p-1)))
	next.Set("page", fmt.Sprint(p+1))
	return map[string]any{"PageNumber": p, "Limit": n, "Previous": "?" + prev.Encode(), "Next": "?" + next.Encode(), "Q": q.Get("q"), "Status": q.Get("status"), "Sort": q.Get("sort"), "Direction": q.Get("direction")}
}
func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	_, p, a := s.getSession(r)
	if a != nil {
		http.Redirect(w, r, "/admin/dashboard", 303)
		return
	}
	if !s.requestLimit(w, r, "list") {
		return
	}
	rows, e := s.fileList(r, false)
	if e != nil {
		fail(w, r, 503, "Ошибка списка файлов")
		return
	}
	data := s.pageData(r)
	data["Files"] = rows
	stats, e := s.personStats(r.Context(), p.ID)
	if e != nil {
		fail(w, r, 503, "Ошибка квот")
		return
	}
	data["Stats"] = stats
	s.render(w, r, "home", data)
}
func (s *Server) personStats(ctx context.Context, pid int64) (map[string]any, error) {
	p, e := s.person(ctx, pid)
	if e != nil {
		return nil, e
	}
	var used, reserved, cup, aup, cdown, adown int64
	for _, v := range []struct {
		q    string
		dest *int64
	}{{"SELECT coalesce(sum(size),0) FROM files WHERE person_id=? AND status='ready'", &used}, {"SELECT coalesce(sum(declared_size),0) FROM uploads WHERE person_id=? AND status IN ('reserved','uploading')", &reserved}} {
		if e = s.db.QueryRowContext(ctx, v.q, pid).Scan(v.dest); e != nil {
			return nil, e
		}
	}
	e = s.db.QueryRowContext(ctx, "SELECT upload_completed_bytes,upload_aborted_bytes,download_completed_bytes,download_aborted_bytes FROM traffic_counters WHERE person_id=? AND month=?", pid, traffic.GetCurrentMonth()).Scan(&cup, &aup, &cdown, &adown)
	if e != nil && e != sql.ErrNoRows {
		return nil, e
	}
	return map[string]any{"used": used + reserved, "reserved": reserved, "quota": p.StorageQuotaBytes, "upload": traffic.CalculateEffectiveUsed(cup, aup, p.MonthlyUploadLimitBytes, true), "download": traffic.CalculateEffectiveUsed(cdown, adown, p.MonthlyDownloadLimit, false), "upload_limit": p.MonthlyUploadLimitBytes, "download_limit": p.MonthlyDownloadLimit, "ignore": p.IgnoreTrafficQuota}, nil
}
func (s *Server) filesAPI(w http.ResponseWriter, r *http.Request) {
	if !s.requestLimit(w, r, "list") {
		return
	}
	_, _, a := s.getSession(r)
	rows, e := s.fileList(r, a != nil)
	if e != nil {
		fail(w, r, 503, "Ошибка списка")
		return
	}
	jsonOut(w, 200, rows)
}
func (s *Server) statsAPI(w http.ResponseWriter, r *http.Request) {
	_, p, a := s.getSession(r)
	if a != nil {
		data, e := s.dashboard(r)
		if e != nil {
			fail(w, r, 503, "Ошибка статистики")
			return
		}
		jsonOut(w, 200, data)
		return
	}
	data, e := s.personStats(r.Context(), p.ID)
	if e != nil {
		fail(w, r, 503, "Ошибка квот")
		return
	}
	jsonOut(w, 200, data)
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	_, p, _ := s.getSession(r)
	if p == nil {
		http.Redirect(w, r, "/admin/sessions", 303)
		return
	}
	rows, e := s.query(r.Context(), "SELECT id,name,last_used_at,last_ip_hash,idle_expires_at,absolute_expires_at,revoked FROM device_sessions WHERE person_id=? ORDER BY last_used_at DESC", p.ID)
	if e != nil {
		fail(w, r, 503, "Ошибка устройств")
		return
	}
	s.render(w, r, "devices", map[string]any{"Rows": rows})
}
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	_, p, _ := s.getSession(r)
	if p == nil {
		fail(w, r, 403, "Пользовательская сессия требуется")
		return
	}
	id, e := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if e != nil && r.FormValue("all") != "1" {
		fail(w, r, 400, "Неверное устройство")
		return
	}
	query := "UPDATE device_sessions SET revoked=1 WHERE person_id=?"
	args := []any{p.ID}
	if r.FormValue("all") != "1" {
		query += " AND id=?"
		args = append(args, id)
	}
	if _, e = s.db.ExecContext(r.Context(), query, args...); e != nil {
		fail(w, r, 503, "Ошибка отзыва")
		return
	}
	s.recordAudit(r, "sessions_revoked", "person", fmt.Sprint(p.ID), "Отзыв пользовательских устройств")
	http.Redirect(w, r, "/devices", 303)
}
func (s *Server) dashboard(r *http.Request) (map[string]any, error) {
	free, total, inodes, e := s.sm.GetDiskUsage()
	if e != nil {
		return nil, e
	}
	up, down := s.speedLimit.GetStats()
	data := map[string]any{"Free": free, "Total": total, "Inodes": inodes, "UpSpeed": up, "DownSpeed": down}
	for key, q := range map[string]string{"Used": "SELECT coalesce(sum(size),0) FROM files", "Sessions": "SELECT count(*) FROM device_sessions WHERE revoked=0 AND idle_expires_at>CURRENT_TIMESTAMP AND (absolute_expires_at IS NULL OR absolute_expires_at>CURRENT_TIMESTAMP)", "Uploads": "SELECT count(*) FROM uploads WHERE external_reserved=1 AND status IN ('reserved','uploading')", "Downloads": "SELECT count(*) FROM transfer_pending", "Quarantine": "SELECT count(*) FROM files WHERE status='quarantined'"} {
		var n int64
		if e = s.db.QueryRowContext(r.Context(), q).Scan(&n); e != nil {
			return nil, e
		}
		data[key] = n
	}
	rows, e := s.query(r.Context(), "SELECT time,actor_type,actor_id,event,entity_id,details FROM audit_logs ORDER BY id DESC LIMIT 10")
	data["Rows"] = rows
	return data, e
}
func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	page := strings.TrimPrefix(r.URL.Path, "/admin/")
	data := s.pageData(r)
	var e error
	var rows []map[string]any
	switch page {
	case "dashboard":
		data, e = s.dashboard(r)
	case "people":
		rows, e = s.query(r.Context(), "SELECT * FROM people WHERE id>0 AND is_orphan=0 ORDER BY id")
		if id, _ := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64); id > 0 {
			data["Edit"], e = s.person(r.Context(), id)
		}
	case "invites":
		rows, e = s.query(r.Context(), "SELECT i.id,p.label,i.code_prefix,i.enabled,i.activations_used,i.max_activations,i.expires_at FROM invite_codes i JOIN people p ON p.id=i.person_id ORDER BY i.created_at DESC")
		if e == nil {
			data["People"], e = s.query(r.Context(), "SELECT id,label FROM people WHERE id>0 AND enabled=1 AND is_orphan=0")
		}
	case "sessions":
		rows, e = s.query(r.Context(), "SELECT ds.id,ds.person_id,coalesce(p.label,a.username) AS label,ds.name,ds.last_used_at,ds.last_ip_hash,ds.idle_expires_at,ds.absolute_expires_at,ds.revoked FROM device_sessions ds LEFT JOIN people p ON p.id=ds.person_id LEFT JOIN admin_users a ON a.id=ds.admin_id ORDER BY ds.last_used_at DESC")
	case "files", "quarantine":
		if page == "quarantine" {
			q := r.URL.Query()
			q.Set("status", "quarantined")
			r.URL.RawQuery = q.Encode()
		}
		rows, e = s.fileList(r, true)
	case "uploads":
		rows, e = s.query(r.Context(), "SELECT u.id,p.label,u.original_name,u.declared_size,u.received_bytes,u.reservation_expires_at FROM uploads u JOIN people p ON p.id=u.person_id WHERE u.status IN ('reserved','uploading') ORDER BY u.created_at")
	case "traffic":
		month := r.URL.Query().Get("month")
		if month == "" {
			month = traffic.GetCurrentMonth()
		}
		data["Month"] = month
		months := []string{}
		now := monthStart(time.Now())
		for i := 0; i < 12; i++ {
			months = append(months, now.AddDate(0, -i, 0).Format("2006-01"))
		}
		data["Months"] = months
		rows, e = s.query(r.Context(), `SELECT p.id,p.label,p.monthly_upload_limit_bytes,p.monthly_download_limit_bytes,p.ignore_traffic_quota,coalesce(t.upload_completed_bytes,0) AS up,coalesce(t.upload_aborted_bytes,0) AS up_aborted,coalesce(t.download_completed_bytes,0) AS down,coalesce(t.download_aborted_bytes,0) AS down_aborted FROM people p LEFT JOIN traffic_counters t ON t.person_id=p.id AND t.month=? WHERE p.id>0 AND p.is_orphan=0 ORDER BY p.id`, month)
	case "audit":
		pg, limit := pagination(r)
		q := "SELECT * FROM audit_logs WHERE 1=1"
		args := []any{}
		for _, k := range []string{"event", "actor_type", "actor_id", "entity_type", "entity_id"} {
			if v := r.URL.Query().Get(k); v != "" {
				q += " AND " + k + "=?"
				args = append(args, v)
				data[k] = v
			}
		}
		q += " ORDER BY id DESC LIMIT ? OFFSET ?"
		args = append(args, limit, (pg-1)*limit)
		rows, e = s.query(r.Context(), q, args...)
	case "settings":
		rows, e = s.query(r.Context(), "SELECT key,type,reason,expires_at FROM rate_limit_locks WHERE expires_at>? ORDER BY expires_at", time.Now().UTC())
	}
	if e != nil {
		fail(w, r, 503, "Ошибка загрузки раздела")
		s.logError(e)
		return
	}
	if page != "dashboard" {
		data["Rows"] = rows
	}
	s.render(w, r, "admin_"+page, data)
}
