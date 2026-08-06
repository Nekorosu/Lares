package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func InitDB(dbPath string) (*sql.DB, error) {
	// Enable WAL mode, busy_timeout, foreign keys
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite write safety

	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		totp_secret TEXT NOT NULL,
		totp_enabled INTEGER NOT NULL DEFAULT 1,
		session_token_hash TEXT,
		created_at DATETIME NOT NULL,
		last_login_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS people (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL,
		notes TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		storage_quota_bytes INTEGER NOT NULL,
		monthly_upload_limit_bytes INTEGER NOT NULL,
		monthly_download_limit_bytes INTEGER NOT NULL,
		max_file_size_bytes INTEGER NOT NULL,
		max_concurrent_uploads INTEGER NOT NULL DEFAULT 1,
		allow_user_keep_forever INTEGER NOT NULL DEFAULT 0,
		session_idle_days INTEGER NOT NULL DEFAULT 30,
		session_absolute_days INTEGER NOT NULL DEFAULT 90,
		ignore_traffic_quota INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		last_activity_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS invite_codes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		code_hash TEXT NOT NULL UNIQUE,
		code_prefix TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		max_activations INTEGER NOT NULL DEFAULT 1,
		activations_used INTEGER NOT NULL DEFAULT 0,
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		created_by_admin_id INTEGER NOT NULL REFERENCES admin_users(id)
	);

	CREATE TABLE IF NOT EXISTS device_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		session_token_hash TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL,
		last_used_at DATETIME NOT NULL,
		last_ip_hash TEXT NOT NULL,
		last_user_agent_hash TEXT NOT NULL,
		idle_expires_at DATETIME NOT NULL,
		absolute_expires_at DATETIME,
		revoked INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS uploads (
		id TEXT PRIMARY KEY,
		person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		session_id INTEGER NOT NULL REFERENCES device_sessions(id) ON DELETE CASCADE,
		upload_secret_hash TEXT NOT NULL,
		original_name TEXT NOT NULL,
		declared_size INTEGER NOT NULL,
		received_bytes INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		expiry_days INTEGER NOT NULL DEFAULT 14,
		reservation_expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		completed_at DATETIME,
		client_ip_hash TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		original_name TEXT NOT NULL,
		stored_path TEXT NOT NULL,
		size INTEGER NOT NULL,
		content_type TEXT NOT NULL,
		status TEXT NOT NULL,
		flagged INTEGER NOT NULL DEFAULT 0,
		flag_reason TEXT NOT NULL DEFAULT '',
		protected INTEGER NOT NULL DEFAULT 0,
		keep_forever INTEGER NOT NULL DEFAULT 0,
		expires_at DATETIME,
		created_at DATETIME NOT NULL,
		client_ip_hash TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS traffic_counters (
		person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		month TEXT NOT NULL,
		upload_completed_bytes INTEGER NOT NULL DEFAULT 0,
		upload_aborted_bytes INTEGER NOT NULL DEFAULT 0,
		download_completed_bytes INTEGER NOT NULL DEFAULT 0,
		download_aborted_bytes INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME NOT NULL,
		PRIMARY KEY (person_id, month)
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		time DATETIME NOT NULL,
		actor_type TEXT NOT NULL,
		actor_id INTEGER NOT NULL DEFAULT 0,
		event TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		ip_hash TEXT NOT NULL,
		details TEXT NOT NULL DEFAULT ''
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
		value TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_device_sessions_person ON device_sessions(person_id);
	CREATE INDEX IF NOT EXISTS idx_uploads_person ON uploads(person_id);
	CREATE INDEX IF NOT EXISTS idx_files_person ON files(person_id);
	CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);
	CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_logs(time);
	`

	_, err := db.Exec(schema)
	return err
}
