package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func InitDB(dbPath string) (*sql.DB, error) {
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dbDir, err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	if err := os.Chmod(dbPath, 0640); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateSchema(db *sql.DB) error {
	// Unknown legacy layouts must never be destroyed during startup.
	var legacy int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='persons'").Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 {
		return fmt.Errorf("обнаружена старая таблица persons: требуется отдельный перенос на копии БД; исходные данные не изменены")
	}
	var uploadSchema string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='uploads'").Scan(&uploadSchema); err != nil && err != sql.ErrNoRows {
		return err
	}
	if regexp.MustCompile(`(?i)(?:\(|,)\s*id\s+INTEGER\b`).MatchString(uploadSchema) {
		return fmt.Errorf("старая схема uploads: автоматический запуск остановлен без удаления данных")
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("база создана более новой версией Lares")
	}
	if version == 0 && uploadSchema != "" {
		var path string
		var seq int
		var name string
		if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
			return err
		}
		backup := path + ".pre-v1.db"
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if _, err = db.Exec("VACUUM INTO ?", backup); err != nil {
				return fmt.Errorf("backup перед миграцией: %w", err)
			}
			if err = os.Chmod(backup, 0640); err != nil {
				return err
			}
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	schema := `

	CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		totp_secret TEXT NOT NULL,
		totp_enabled BOOLEAN NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL,
		last_login_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS people (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL,
		notes TEXT NOT NULL DEFAULT '',
		enabled BOOLEAN NOT NULL DEFAULT 1,
		storage_quota_bytes INTEGER NOT NULL,
		monthly_upload_limit_bytes INTEGER NOT NULL,
		monthly_download_limit_bytes INTEGER NOT NULL,
		max_file_size_bytes INTEGER NOT NULL,
		max_concurrent_uploads INTEGER NOT NULL DEFAULT 1,
		allow_user_keep_forever BOOLEAN NOT NULL DEFAULT 0,
		session_idle_days INTEGER NOT NULL DEFAULT 30,
		session_absolute_days INTEGER NOT NULL DEFAULT 90,
		ignore_traffic_quota BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		last_activity_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS invite_codes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		person_id INTEGER NOT NULL,
		code_hash TEXT UNIQUE NOT NULL,
		code_prefix TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		max_activations INTEGER NOT NULL DEFAULT 1,
		activations_used INTEGER NOT NULL DEFAULT 0,
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		created_by_admin_id INTEGER NOT NULL,
		FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS device_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		person_id INTEGER,
		admin_id INTEGER,

		is_admin BOOLEAN NOT NULL DEFAULT 0,
		name TEXT NOT NULL,
		session_token_hash TEXT UNIQUE NOT NULL,
		created_at DATETIME NOT NULL,
		last_used_at DATETIME NOT NULL,
		last_ip_hash TEXT NOT NULL,
		last_user_agent_hash TEXT NOT NULL,
		idle_expires_at DATETIME NOT NULL,
		absolute_expires_at DATETIME,
		revoked BOOLEAN NOT NULL DEFAULT 0,
		FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS uploads (
		id TEXT PRIMARY KEY,
		person_id INTEGER NOT NULL,
		session_id INTEGER,
		upload_secret_hash TEXT NOT NULL,
		original_name TEXT NOT NULL,
		declared_size INTEGER NOT NULL,
		received_bytes INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		expiry_days INTEGER NOT NULL DEFAULT 14,
		reservation_expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		completed_at DATETIME,
		client_ip_hash TEXT NOT NULL,
		FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE,
		FOREIGN KEY (session_id) REFERENCES device_sessions(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		person_id INTEGER NOT NULL,
		uploader_name TEXT NOT NULL,
		original_name TEXT NOT NULL,
		stored_path TEXT NOT NULL,
		size INTEGER NOT NULL,
		content_type TEXT NOT NULL,
		status TEXT NOT NULL,
		flagged BOOLEAN NOT NULL DEFAULT 0,
		flag_reason TEXT NOT NULL DEFAULT '',
		protected BOOLEAN NOT NULL DEFAULT 0,
		keep_forever BOOLEAN NOT NULL DEFAULT 0,
		expires_at DATETIME,
		created_at DATETIME NOT NULL,
		client_ip_hash TEXT NOT NULL,
		FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS traffic_counters (
		person_id INTEGER NOT NULL,
		month TEXT NOT NULL,
		upload_completed_bytes INTEGER NOT NULL DEFAULT 0,
		upload_aborted_bytes INTEGER NOT NULL DEFAULT 0,
		download_completed_bytes INTEGER NOT NULL DEFAULT 0,
		download_aborted_bytes INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME NOT NULL,
		PRIMARY KEY (person_id, month),
		FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		time DATETIME NOT NULL,
		actor_type TEXT NOT NULL,
		actor_id INTEGER NOT NULL,
		event TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		ip_hash TEXT NOT NULL,
		details TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rate_limit_locks (
		key TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		reason TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_device_sessions_token ON device_sessions(session_token_hash);
	CREATE INDEX IF NOT EXISTS idx_invite_codes_hash ON invite_codes(code_hash);
	CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);
	CREATE INDEX IF NOT EXISTS idx_files_expires ON files(expires_at);
	CREATE INDEX IF NOT EXISTS idx_uploads_status ON uploads(status);
	CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_logs(time);
	CREATE INDEX IF NOT EXISTS idx_rate_limit_expires ON rate_limit_locks(expires_at);
	`

	if _, err = tx.Exec(schema); err != nil {
		return err
	}
	additions := map[string][]string{
		"device_sessions":  {"admin_id INTEGER", "is_admin BOOLEAN NOT NULL DEFAULT 0"},
		"admin_users":      {"last_totp_step INTEGER NOT NULL DEFAULT 0"},
		"invite_codes":     {"code_prefix TEXT NOT NULL DEFAULT ''"},
		"people":           {"is_orphan BOOLEAN NOT NULL DEFAULT 0", "upload_speed REAL NOT NULL DEFAULT 2097152"},
		"files":            {"uploader_name TEXT NOT NULL DEFAULT 'Пользователь'"},
		"traffic_counters": {"local_upload_bytes INTEGER NOT NULL DEFAULT 0", "local_download_bytes INTEGER NOT NULL DEFAULT 0"},
		"uploads":          {"external_reserved BOOLEAN NOT NULL DEFAULT 1", "external_received_bytes INTEGER NOT NULL DEFAULT 0", "local_received_bytes INTEGER NOT NULL DEFAULT 0", "io_seconds REAL NOT NULL DEFAULT 0", "final_file_id TEXT NOT NULL DEFAULT ''", "file_record TEXT NOT NULL DEFAULT ''"},
	}
	for table, defs := range additions {
		cols := map[string]bool{}
		rows, e := tx.Query("PRAGMA table_info(" + table + ")")
		if e != nil {
			return e
		}
		for rows.Next() {
			var id, notnull, pk int
			var name, typ string
			var def interface{}
			if e = rows.Scan(&id, &name, &typ, &notnull, &def, &pk); e != nil {
				rows.Close()
				return e
			}
			cols[name] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, def := range defs {
			if !cols[strings.Fields(def)[0]] {
				if _, e = tx.Exec("ALTER TABLE " + table + " ADD COLUMN " + def); e != nil {
					return e
				}
			}
		}
	}
	if _, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS request_events(key TEXT NOT NULL,time DATETIME NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_request_events ON request_events(key,time);
 CREATE TABLE IF NOT EXISTS transfer_pending(id TEXT PRIMARY KEY,person_id INTEGER NOT NULL,ip_hash TEXT NOT NULL,size INTEGER NOT NULL,bytes INTEGER NOT NULL DEFAULT 0,month TEXT NOT NULL,created_at DATETIME NOT NULL);
 CREATE TABLE IF NOT EXISTS upload_traffic(upload_id TEXT NOT NULL,month TEXT NOT NULL,external_bytes INTEGER NOT NULL DEFAULT 0,local_bytes INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(upload_id,month));
 CREATE TABLE IF NOT EXISTS file_deletions(id TEXT PRIMARY KEY,stored_path TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS revoke_deleted_admin BEFORE DELETE ON admin_users BEGIN DELETE FROM device_sessions WHERE admin_id=OLD.id; END;
 INSERT INTO people(id,label,enabled,storage_quota_bytes,monthly_upload_limit_bytes,monthly_download_limit_bytes,max_file_size_bytes,max_concurrent_uploads,created_at,is_orphan)
 VALUES(0,'Удалённый пользователь',0,0,0,0,0,1,datetime('now'),1) ON CONFLICT(id) DO NOTHING;
 UPDATE people SET enabled=0,is_orphan=1 WHERE id=0;
 UPDATE device_sessions SET revoked=1 WHERE person_id=0;
 PRAGMA user_version=1;
 `); err != nil {
		return err
	}
	if version == 0 {
		// Old active uploads have no reliable network ledger. Preserve bytes conservatively as external.
		if _, err = tx.Exec(`UPDATE uploads SET external_received_bytes=received_bytes WHERE status IN ('reserved','uploading') AND received_bytes>0;
 UPDATE uploads SET status='canceled' WHERE status='cancelled';
 UPDATE device_sessions SET revoked=1 WHERE is_admin=1;
 UPDATE files SET uploader_name=coalesce((SELECT label FROM people WHERE people.id=files.person_id),'Пользователь') WHERE uploader_name='Пользователь';`); err != nil {
			return err
		}

		rows, e := tx.Query("SELECT id,created_at,received_bytes FROM uploads WHERE status IN ('reserved','uploading') AND received_bytes>0")
		if e != nil {
			return e
		}
		type progress struct {
			id      string
			created time.Time
			bytes   int64
		}
		var old []progress
		for rows.Next() {
			var p progress
			if e = rows.Scan(&p.id, &p.created, &p.bytes); e != nil {
				rows.Close()
				return e
			}
			old = append(old, p)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, p := range old {
			if _, e = tx.Exec("INSERT INTO upload_traffic(upload_id,month,external_bytes,local_bytes) VALUES(?,?,?,0) ON CONFLICT(upload_id,month) DO NOTHING", p.id, p.created.Local().Format("2006-01"), p.bytes); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
