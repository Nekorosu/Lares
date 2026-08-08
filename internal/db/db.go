package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	var sqlSchema string
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='device_sessions'").Scan(&sqlSchema)
	if strings.Contains(sqlSchema, "person_id INTEGER NOT NULL") {
		_, _ = db.Exec("DROP TABLE device_sessions;")
	}

	// Drop legacy tables referencing old 'persons' table or having obsolete column types
	var inviteSql, uploadsSql, filesSql, trafficSql string
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='invite_codes'").Scan(&inviteSql)
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='uploads'").Scan(&uploadsSql)
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='files'").Scan(&filesSql)
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='traffic_counters'").Scan(&trafficSql)

	if strings.Contains(inviteSql, "REFERENCES persons") {
		_, _ = db.Exec("DROP TABLE IF EXISTS invite_codes;")
	}
	if strings.Contains(uploadsSql, "REFERENCES persons") || strings.Contains(uploadsSql, "id INTEGER") || strings.Contains(uploadsSql, "session_id INTEGER NOT NULL") {
		_, _ = db.Exec("DROP TABLE IF EXISTS uploads;")
	}
	if strings.Contains(filesSql, "REFERENCES persons") {
		_, _ = db.Exec("DROP TABLE IF EXISTS files;")
	}
	if strings.Contains(trafficSql, "REFERENCES persons") {
		_, _ = db.Exec("DROP TABLE IF EXISTS traffic_counters;")
	}
	_, _ = db.Exec("DROP TABLE IF EXISTS persons;")

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

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Safe alter table migrations for existing tables
	_, _ = db.Exec("ALTER TABLE device_sessions ADD COLUMN admin_id INTEGER;")
	_, _ = db.Exec("ALTER TABLE device_sessions ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE invite_codes ADD COLUMN code_prefix TEXT NOT NULL DEFAULT '';")
	_, _ = db.Exec("ALTER TABLE files ADD COLUMN uploader_name TEXT NOT NULL DEFAULT 'Пользователь';")
	_, _ = db.Exec("ALTER TABLE traffic_counters ADD COLUMN local_upload_bytes INTEGER NOT NULL DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE traffic_counters ADD COLUMN local_download_bytes INTEGER NOT NULL DEFAULT 0;")
	_, _ = db.Exec(`
		INSERT INTO people (id, label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, allow_user_keep_forever, session_idle_days, session_absolute_days, ignore_traffic_quota, created_at)
		VALUES (0, 'Администратор', 'Системный профиль Администратора', 1, 1099511627776, 1099511627776, 1099511627776, 1099511627776, 100, 1, 365, 365, 1, datetime('now'))
		ON CONFLICT(id) DO NOTHING;
	`)
	_, _ = db.Exec("UPDATE files SET person_id = 0 WHERE uploader_name = 'Администратор' OR uploader_name = 'admin';")

	return nil
}

