package traffic

import (
	"context"
	"database/sql"
	"time"
)

func GetCurrentMonth() string { return time.Now().Local().Format("2006-01") }
func CalculateEffectiveUsed(completed, aborted, limit int64, upload bool) int64 {
	allowance := limit
	if upload {
		allowance = limit / 2
	}
	return completed + max(0, aborted-allowance)
}

// Zero is a zero quota. Only ignore_traffic_quota bypasses external traffic limits.
func CheckGraceRule(used, pending, size, limit int64) bool {
	if used < 0 || pending < 0 || size < 0 || limit < 0 || used > limit || pending > limit-used {
		return false
	}
	return size/2+size%2 <= limit-used-pending
}
func Record(ctx context.Context, tx *sql.Tx, pid int64, month, kind string, n int64) error {
	if n <= 0 {
		return nil
	}
	allowed := map[string]bool{"upload_completed_bytes": true, "upload_aborted_bytes": true, "download_completed_bytes": true, "download_aborted_bytes": true, "local_upload_bytes": true, "local_download_bytes": true}
	if !allowed[kind] {
		panic("invalid counter")
	}
	_, e := tx.ExecContext(ctx, "INSERT INTO traffic_counters(person_id,month,"+kind+",updated_at) VALUES(?,?,?,?) ON CONFLICT(person_id,month) DO UPDATE SET "+kind+"="+kind+"+excluded."+kind+",updated_at=excluded.updated_at", pid, month, n, time.Now().UTC())
	return e
}
func Used(ctx context.Context, tx *sql.Tx, pid int64, limit int64, upload bool) (int64, error) {
	kind := "download"
	if upload {
		kind = "upload"
	}
	var c, a int64
	e := tx.QueryRowContext(ctx, "SELECT "+kind+"_completed_bytes,"+kind+"_aborted_bytes FROM traffic_counters WHERE person_id=? AND month=?", pid, GetCurrentMonth()).Scan(&c, &a)
	if e == sql.ErrNoRows {
		return 0, nil
	}
	return CalculateEffectiveUsed(c, a, limit, upload), e
}
