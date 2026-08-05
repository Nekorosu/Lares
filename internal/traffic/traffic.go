package traffic

import (
	"database/sql"
	"fmt"
	"time"

	"homeshare/internal/models"
)

type Tracker struct {
	db *sql.DB
}

func NewTracker(db *sql.DB) *Tracker {
	return &Tracker{db: db}
}

func CurrentMonthKey() string {
	return time.Now().Format("2006-01")
}

func CalculateEffectiveUsed(completedBytes, abortedBytes int64, allowanceFactor float64, limitBytes int64) int64 {
	allowance := int64(float64(limitBytes) * allowanceFactor)
	wasted := abortedBytes - allowance
	if wasted < 0 {
		wasted = 0
	}
	return completedBytes + wasted
}

func (t *Tracker) GetCounter(personID int64, month string) (*models.TrafficCounter, error) {
	if month == "" {
		month = CurrentMonthKey()
	}
	row := t.db.QueryRow(`
		SELECT person_id, month, upload_completed_bytes, upload_aborted_bytes,
		       download_completed_bytes, download_aborted_bytes, updated_at
		FROM traffic_counters
		WHERE person_id = ? AND month = ?
	`, personID, month)

	var tc models.TrafficCounter
	err := row.Scan(&tc.PersonID, &tc.Month, &tc.UploadCompletedBytes, &tc.UploadAbortedBytes,
		&tc.DownloadCompletedBytes, &tc.DownloadAbortedBytes, &tc.UpdatedAt)

	if err == sql.ErrNoRows {
		return &models.TrafficCounter{
			PersonID: personID,
			Month:    month,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &tc, nil
}

func (t *Tracker) AddUploadCompleted(personID int64, bytes int64) error {
	month := CurrentMonthKey()
	_, err := t.db.Exec(`
		INSERT INTO traffic_counters (person_id, month, upload_completed_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			upload_completed_bytes = upload_completed_bytes + excluded.upload_completed_bytes,
			updated_at = excluded.updated_at
	`, personID, month, bytes, time.Now())
	return err
}

func (t *Tracker) AddUploadAborted(personID int64, bytes int64) error {
	month := CurrentMonthKey()
	_, err := t.db.Exec(`
		INSERT INTO traffic_counters (person_id, month, upload_aborted_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			upload_aborted_bytes = upload_aborted_bytes + excluded.upload_aborted_bytes,
			updated_at = excluded.updated_at
	`, personID, month, bytes, time.Now())
	return err
}

func (t *Tracker) AddDownloadCompleted(personID int64, bytes int64) error {
	month := CurrentMonthKey()
	_, err := t.db.Exec(`
		INSERT INTO traffic_counters (person_id, month, download_completed_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			download_completed_bytes = download_completed_bytes + excluded.download_completed_bytes,
			updated_at = excluded.updated_at
	`, personID, month, bytes, time.Now())
	return err
}

func (t *Tracker) AddDownloadAborted(personID int64, bytes int64) error {
	month := CurrentMonthKey()
	_, err := t.db.Exec(`
		INSERT INTO traffic_counters (person_id, month, download_aborted_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(person_id, month) DO UPDATE SET
			download_aborted_bytes = download_aborted_bytes + excluded.download_aborted_bytes,
			updated_at = excluded.updated_at
	`, personID, month, bytes, time.Now())
	return err
}

// CheckTrafficGrace enforces: current_effective_used + pending_reserved + file_size/2 <= limit
func (t *Tracker) CheckUploadTrafficGrace(person *models.Person, pendingReserved int64, fileSize int64, isLocal bool) error {
	if isLocal || person.IgnoreTrafficQuota {
		return nil
	}
	tc, err := t.GetCounter(person.ID, CurrentMonthKey())
	if err != nil {
		return err
	}

	effectiveUsed := CalculateEffectiveUsed(tc.UploadCompletedBytes, tc.UploadAbortedBytes, 0.5, person.MonthlyUploadLimitBytes)
	required := effectiveUsed + pendingReserved + (fileSize / 2)

	if required > person.MonthlyUploadLimitBytes {
		return fmt.Errorf("превышен лимит исходящего трафика за месяц (исп: %d MB, лимит: %d MB)",
			effectiveUsed/(1024*1024), person.MonthlyUploadLimitBytes/(1024*1024))
	}
	return nil
}

func (t *Tracker) CheckDownloadTrafficGrace(person *models.Person, pendingReserved int64, fileSize int64, isLocal bool) error {
	if isLocal || person.IgnoreTrafficQuota {
		return nil
	}
	tc, err := t.GetCounter(person.ID, CurrentMonthKey())
	if err != nil {
		return err
	}

	effectiveUsed := CalculateEffectiveUsed(tc.DownloadCompletedBytes, tc.DownloadAbortedBytes, 1.0, person.MonthlyDownloadLimitBytes)
	required := effectiveUsed + pendingReserved + (fileSize / 2)

	if required > person.MonthlyDownloadLimitBytes {
		return fmt.Errorf("превышен лимит входящего трафика за месяц (исп: %d MB, лимит: %d MB)",
			effectiveUsed/(1024*1024), person.MonthlyDownloadLimitBytes/(1024*1024))
	}
	return nil
}

func (t *Tracker) ResetMonthTraffic(personID int64, month string) error {
	if month == "" {
		month = CurrentMonthKey()
	}
	_, err := t.db.Exec("DELETE FROM traffic_counters WHERE person_id = ? AND month = ?", personID, month)
	return err
}

func (t *Tracker) PurgeOldHistory(keepMonths int) (int64, error) {
	cutoffMonth := time.Now().AddDate(0, -keepMonths, 0).Format("2006-01")
	res, err := t.db.Exec("DELETE FROM traffic_counters WHERE month < ?", cutoffMonth)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
