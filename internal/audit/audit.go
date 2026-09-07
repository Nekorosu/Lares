package audit

import (
	"context"
	"database/sql"
	"errors"
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

func (l *Logger) Log(actorType string, actorID int64, event, entityType, entityID, rawIP, details string) error {
	if l == nil || l.db == nil {
		return errors.New("audit logger is not initialized")
	}

	ipHash := ""
	if rawIP != "" {
		ipHash = auth.HashWithSalt(rawIP, l.ipHashSalt)
	}

	query := `
		INSERT INTO audit_logs (time, actor_type, actor_id, event, entity_type, entity_id, ip_hash, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, e := l.db.ExecContext(ctx, query, time.Now().UTC(), actorType, actorID, event, entityType, entityID, ipHash, details)
	return e
}
