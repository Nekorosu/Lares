package audit

import (
	"database/sql"
	"time"

	"lares/internal/models"
)

type Logger struct {
	db *sql.DB
}

func NewLogger(db *sql.DB) *Logger {
	return &Logger{db: db}
}

func (l *Logger) Log(actorType string, actorID int64, event, entityType, entityID, ipHash, details string) error {
	_, err := l.db.Exec(`
		INSERT INTO audit_logs (time, actor_type, actor_id, event, entity_type, entity_id, ip_hash, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, time.Now(), actorType, actorID, event, entityType, entityID, ipHash, details)
	return err
}

func (l *Logger) GetLogs(limit int, offset int, actorType string) ([]models.AuditLog, error) {
	query := "SELECT id, time, actor_type, actor_id, event, entity_type, entity_id, ip_hash, details FROM audit_logs"
	args := []interface{}{}
	if actorType != "" {
		query += " WHERE actor_type = ?"
		args = append(args, actorType)
	}
	query += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.AuditLog
	for rows.Next() {
		var item models.AuditLog
		if err := rows.Scan(&item.ID, &item.Time, &item.ActorType, &item.ActorID, &item.Event, &item.EntityType, &item.EntityID, &item.IPHash, &item.Details); err != nil {
			return nil, err
		}
		logs = append(logs, item)
	}
	return logs, nil
}

func (l *Logger) PurgeOldLogs(retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	res, err := l.db.Exec("DELETE FROM audit_logs WHERE time < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
