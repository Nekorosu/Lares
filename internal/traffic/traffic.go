package traffic

import (
	"database/sql"
	"fmt"
	"time"
)

type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

func GetCurrentMonth() string {
	return time.Now().Local().Format("2006-01")
}

func CalculateEffectiveUsed(completedBytes, abortedBytes, limitBytes int64, isUpload bool) int64 {
	var allowanceRatio float64 = 1.0
	if isUpload {
		allowanceRatio = 0.5
	}
	allowance := int64(float64(limitBytes) * allowanceRatio)

	excessAborted := abortedBytes - allowance
	if excessAborted < 0 {
		excessAborted = 0
	}

	return completedBytes + excessAborted
}

func CheckGraceRule(effectiveUsed, pendingReserved, newFileSize, limitBytes int64) bool {
	if limitBytes <= 0 {
		return true // Unlimited
	}
	projected := effectiveUsed + pendingReserved + (newFileSize / 2)
	return projected <= limitBytes
}

func (m *Manager) CanUpload(personID int64, monthlyLimit, declaredSize int64, ignoreQuota bool, isLocal bool) (bool, error) {
	if isLocal || ignoreQuota || monthlyLimit <= 0 {
		return true, nil
	}

	month := GetCurrentMonth()

	var completed, aborted int64
	err := m.db.QueryRow(`
		SELECT upload_completed_bytes, upload_aborted_bytes
		FROM traffic_counters
		WHERE person_id = ? AND month = ?
	`, personID, month).Scan(&completed, &aborted)

	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to query upload traffic counter: %w", err)
	}

	// Pending reserved uploads for this person
	var pendingReserved int64
	_ = m.db.QueryRow(`
		SELECT COALESCE(SUM(declared_size - received_bytes), 0)
		FROM uploads
		WHERE person_id = ? AND status IN ('reserved', 'uploading')
	`, personID).Scan(&pendingReserved)

	effectiveUsed := CalculateEffectiveUsed(completed, aborted, monthlyLimit, true)
	return CheckGraceRule(effectiveUsed, pendingReserved, declaredSize, monthlyLimit), nil
}

func (m *Manager) CanDownload(personID int64, monthlyLimit, fileSize int64, ignoreQuota bool, isLocal bool) (bool, error) {
	if isLocal || ignoreQuota || monthlyLimit <= 0 {
		return true, nil
	}

	month := GetCurrentMonth()

	var completed, aborted int64
	err := m.db.QueryRow(`
		SELECT download_completed_bytes, download_aborted_bytes
		FROM traffic_counters
		WHERE person_id = ? AND month = ?
	`, personID, month).Scan(&completed, &aborted)

	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to query download traffic counter: %w", err)
	}

	effectiveUsed := CalculateEffectiveUsed(completed, aborted, monthlyLimit, false)
	return CheckGraceRule(effectiveUsed, 0, fileSize, monthlyLimit), nil
}

func (m *Manager) RecordUploadCompleted(personID int64, bytes int64, isLocal bool) error {
	if isLocal || bytes <= 0 {
		return nil
	}
	month := GetCurrentMonth()
	query := `
		INSERT INTO traffic_counters (person_id, month, upload_completed_bytes, upload_aborted_bytes, download_completed_bytes, download_aborted_bytes, updated_at)
		VALUES (?, ?, ?, 0, 0, 0, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			upload_completed_bytes = upload_completed_bytes + excluded.upload_completed_bytes,
			updated_at = excluded.updated_at
	`
	_, err := m.db.Exec(query, personID, month, bytes, time.Now().UTC())
	return err
}

func (m *Manager) RecordUploadAborted(personID int64, bytes int64, isLocal bool) error {
	if isLocal || bytes <= 0 {
		return nil
	}
	month := GetCurrentMonth()
	query := `
		INSERT INTO traffic_counters (person_id, month, upload_completed_bytes, upload_aborted_bytes, download_completed_bytes, download_aborted_bytes, updated_at)
		VALUES (?, ?, 0, ?, 0, 0, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			upload_aborted_bytes = upload_aborted_bytes + excluded.upload_aborted_bytes,
			updated_at = excluded.updated_at
	`
	_, err := m.db.Exec(query, personID, month, bytes, time.Now().UTC())
	return err
}

func (m *Manager) RecordDownloadCompleted(personID int64, bytes int64, isLocal bool) error {
	if isLocal || bytes <= 0 {
		return nil
	}
	month := GetCurrentMonth()
	query := `
		INSERT INTO traffic_counters (person_id, month, upload_completed_bytes, upload_aborted_bytes, download_completed_bytes, download_aborted_bytes, updated_at)
		VALUES (?, ?, 0, 0, ?, 0, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			download_completed_bytes = download_completed_bytes + excluded.download_completed_bytes,
			updated_at = excluded.updated_at
	`
	_, err := m.db.Exec(query, personID, month, bytes, time.Now().UTC())
	return err
}

func (m *Manager) RecordDownloadAborted(personID int64, bytes int64, isLocal bool) error {
	if isLocal || bytes <= 0 {
		return nil
	}
	month := GetCurrentMonth()
	query := `
		INSERT INTO traffic_counters (person_id, month, upload_completed_bytes, upload_aborted_bytes, download_completed_bytes, download_aborted_bytes, updated_at)
		VALUES (?, ?, 0, 0, 0, ?, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			download_aborted_bytes = download_aborted_bytes + excluded.download_aborted_bytes,
			updated_at = excluded.updated_at
	`
	_, err := m.db.Exec(query, personID, month, bytes, time.Now().UTC())
	return err
}

func (m *Manager) ResetCurrentMonth(personID int64) error {
	month := GetCurrentMonth()
	_, err := m.db.Exec("DELETE FROM traffic_counters WHERE person_id = ? AND month = ?", personID, month)
	return err
}
