package audit

import (
	"database/sql"
	"time"

	"lares/internal/auth"
)

type Logger struct {
	db         *sql.DB
	ipHashSalt string
}

func NewLogger(db *sql.DB, ipHashSalt string) *Logger {
	return &Logger{
		db:         db,
		ipHashSalt: ipHashSalt,
	}
}

func (l *Logger) Log(actorType string, actorID int64, event, entityType, entityID, rawIP, details string) {
	if l == nil || l.db == nil {
		return
	}

	ipHash := ""
	if rawIP != "" {
		ipHash = auth.HashWithSalt(rawIP, l.ipHashSalt)
	}

	query := `
		INSERT INTO audit_logs (time, actor_type, actor_id, event, entity_type, entity_id, ip_hash, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, _ = l.db.Exec(query, time.Now().UTC(), actorType, actorID, event, entityType, entityID, ipHash, details)
}
