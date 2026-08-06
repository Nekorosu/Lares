package upload

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"

	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/models"
	"lares/internal/storage"
	"lares/internal/traffic"
)

type Manager struct {
	db          *sql.DB
	cfg         *config.Config
	storage     *storage.Storage
	traffic     *traffic.Tracker
}

func NewManager(db *sql.DB, cfg *config.Config, st *storage.Storage, tr *traffic.Tracker) *Manager {
	return &Manager{
		db:      db,
		cfg:     cfg,
		storage: st,
		traffic: tr,
	}
}

func (m *Manager) CreateReservation(person *models.Person, sessionID int64, filename string, declaredSize int64, contentType string, expiryDays int, clientIPHash string, isLocal bool) (*models.Upload, string, error) {
	if !person.Enabled {
		return nil, "", errors.New("учетная запись отключена")
	}

	// 1. Max file size check
	if declaredSize > person.MaxFileSizeBytes {
		return nil, "", fmt.Errorf("размер файла (%d MB) превышает максимальный допустимый (%d MB)",
			declaredSize/(1024*1024), person.MaxFileSizeBytes/(1024*1024))
	}

	// 2. Max concurrent uploads check
	var activeUploadsCount int
	err := m.db.QueryRow(`
		SELECT COUNT(*) FROM uploads
		WHERE person_id = ? AND status IN ('reserved', 'uploading') AND reservation_expires_at > ?
	`, person.ID, time.Now()).Scan(&activeUploadsCount)
	if err != nil {
		return nil, "", err
	}
	if activeUploadsCount >= person.MaxConcurrentUploads {
		return nil, "", fmt.Errorf("превышено количество одновременных загрузок (макс: %d)", person.MaxConcurrentUploads)
	}

	// 3. Disk space reserve check
	if err := m.storage.VerifyDiskCanAcceptUpload(declaredSize); err != nil {
		return nil, "", err
	}

	// 4. Strict storage quota check (Ready Files + Active Reserved Uploads + DeclaredSize <= Quota)
	tx, err := m.db.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	var usedFilesSize int64
	tx.QueryRow("SELECT COALESCE(SUM(size), 0) FROM files WHERE person_id = ?", person.ID).Scan(&usedFilesSize)

	var reservedUploadsSize int64
	tx.QueryRow(`
		SELECT COALESCE(SUM(declared_size), 0) FROM uploads
		WHERE person_id = ? AND status IN ('reserved', 'uploading') AND reservation_expires_at > ?
	`, person.ID, time.Now()).Scan(&reservedUploadsSize)

	if usedFilesSize+reservedUploadsSize+declaredSize > person.StorageQuotaBytes {
		return nil, "", fmt.Errorf("превышена квота хранилища (%d GB из %d GB)",
			(usedFilesSize+reservedUploadsSize+declaredSize)/(1024*1024*1024), person.StorageQuotaBytes/(1024*1024*1024))
	}

	// 5. Monthly upload traffic grace check
	if err := m.traffic.CheckUploadTrafficGrace(person, reservedUploadsSize, declaredSize, isLocal); err != nil {
		return nil, "", err
	}

	// 6. Dynamic TTL calculation
	// Estimated speed: 2 MB/s (2,097,152 bytes/s)
	estimatedSeconds := float64(declaredSize) / (2 * 1024 * 1024)
	ttlSeconds := int64(estimatedSeconds * 2.0)
	if ttlSeconds < 3600 {
		ttlSeconds = 3600 // min 1 hour
	}
	if ttlSeconds > 72*3600 {
		ttlSeconds = 72 * 3600 // max 72 hours
	}
	reservationExpiresAt := time.Now().Add(time.Duration(ttlSeconds) * time.Second)

	// Generate Upload ID and Upload Secret
	bID := make([]byte, 16)
	rand.Read(bID)
	uploadID := hex.EncodeToString(bID)

	uploadSecret, secretHash := auth.GenerateRandomToken()
	cleanName := m.storage.SanitizeFilename(filename)

	upload := &models.Upload{
		ID:                   uploadID,
		PersonID:             person.ID,
		SessionID:            sessionID,
		UploadSecretHash:     secretHash,
		OriginalName:         cleanName,
		DeclaredSize:         declaredSize,
		ReceivedBytes:        0,
		Status:               models.UploadStatusReserved,
		ExpiryDays:           expiryDays,
		ReservationExpiresAt: reservationExpiresAt,
		CreatedAt:            time.Now(),
		ClientIPHash:         clientIPHash,
	}

	_, err = tx.Exec(`
		INSERT INTO uploads (id, person_id, session_id, upload_secret_hash, original_name, declared_size, received_bytes, status, expiry_days, reservation_expires_at, created_at, client_ip_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, upload.ID, upload.PersonID, upload.SessionID, upload.UploadSecretHash, upload.OriginalName, upload.DeclaredSize, upload.ReceivedBytes, upload.Status, upload.ExpiryDays, upload.ReservationExpiresAt, upload.CreatedAt, upload.ClientIPHash)

	if err != nil {
		return nil, "", err
	}

	if err := tx.Commit(); err != nil {
		return nil, "", err
	}

	return upload, uploadSecret, nil
}

func (m *Manager) GetUpload(id string) (*models.Upload, error) {
	row := m.db.QueryRow(`
		SELECT id, person_id, session_id, upload_secret_hash, original_name, declared_size, received_bytes, status, expiry_days, reservation_expires_at, created_at, completed_at, client_ip_hash
		FROM uploads WHERE id = ?
	`, id)

	var u models.Upload
	err := row.Scan(&u.ID, &u.PersonID, &u.SessionID, &u.UploadSecretHash, &u.OriginalName, &u.DeclaredSize, &u.ReceivedBytes, &u.Status, &u.ExpiryDays, &u.ReservationExpiresAt, &u.CreatedAt, &u.CompletedAt, &u.ClientIPHash)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (m *Manager) AppendChunk(uploadID string, secret string, offset int64, reader io.Reader) (int64, error) {
	upload, err := m.GetUpload(uploadID)
	if err != nil {
		return 0, fmt.Errorf("загрузка не найдена: %w", err)
	}

	if auth.HashString(secret) != upload.UploadSecretHash {
		return 0, errors.New("неверный секретный ключ загрузки")
	}

	if upload.Status != models.UploadStatusReserved && upload.Status != models.UploadStatusUploading {
		return 0, fmt.Errorf("загрузка в статусе %s не может принимать данные", upload.Status)
	}

	if time.Now().After(upload.ReservationExpiresAt) {
		m.db.Exec("UPDATE uploads SET status = ? WHERE id = ?", models.UploadStatusExpired, uploadID)
		return 0, errors.New("срок резервирования загрузки истек")
	}

	partialPath := m.storage.GetPartialPath(uploadID)
	f, err := os.OpenFile(partialPath, os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return 0, fmt.Errorf("не удалось открыть файл .part: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err == nil {
		if stat.Size() != offset {
			return 0, fmt.Errorf("несоответствие смещения (offset): сервер имеет %d, получено %d", stat.Size(), offset)
		}
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}

	written, err := io.Copy(f, reader)
	if err != nil {
		return written, err
	}

	newTotal := offset + written
	if newTotal > upload.DeclaredSize {
		// Size mismatch: actual size > declared size
		m.CancelUpload(uploadID, secret, "превышен заявленный размер файла")
		return written, errors.New("размер переданных данных превышает заявленный")
	}

	// Extend TTL by 1 hour minimum
	newTTL := time.Now().Add(1 * time.Hour)
	if newTTL.Before(upload.ReservationExpiresAt) {
		newTTL = upload.ReservationExpiresAt
	}

	m.db.Exec(`
		UPDATE uploads
		SET received_bytes = ?, status = ?, reservation_expires_at = ?
		WHERE id = ?
	`, newTotal, models.UploadStatusUploading, newTTL, uploadID)

	return written, nil
}

func (m *Manager) FinalizeUpload(uploadID string, secret string, customSuspicious []string) (*models.FileRecord, error) {
	upload, err := m.GetUpload(uploadID)
	if err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: не удалось найти загрузку ID=%s: %v", uploadID, err)
		return nil, err
	}

	if auth.HashString(secret) != upload.UploadSecretHash {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: неверный секретный ключ для загрузки ID=%s", uploadID)
		return nil, errors.New("неверный секретный ключ загрузки")
	}

	// Verify person_id exists and is valid
	var personExists bool
	err = m.db.QueryRow("SELECT EXISTS(SELECT 1 FROM people WHERE id = ?)", upload.PersonID).Scan(&personExists)
	if err != nil || !personExists {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: пользователь person_id=%d не существует для загрузки ID=%s", upload.PersonID, uploadID)
		return nil, fmt.Errorf("пользователь (person_id=%d) не найден", upload.PersonID)
	}

	partialPath := m.storage.GetPartialPath(uploadID)
	info, err := os.Stat(partialPath)
	if err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: частичный файл не найден %s: %v", partialPath, err)
		return nil, fmt.Errorf("частичный файл не найден: %w", err)
	}

	actualSize := info.Size()
	if actualSize == 0 {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: попытка завершить пустую загрузку ID=%s", uploadID)
		return nil, errors.New("нельзя завершить пустую загрузку")
	}

	// Generate random file ID and stored path
	bID := make([]byte, 16)
	rand.Read(bID)
	fileID := hex.EncodeToString(bID)

	relStoredPath, err := m.storage.GenerateStoredPath(fileID)
	if err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: ошибка генерации пути хранения: %v", err)
		return nil, err
	}
	finalFullPath := m.storage.GetFullPath(relStoredPath)

	// Check suspicious / quarantine
	isSuspicious, reason := m.storage.IsSuspicious(upload.OriginalName, customSuspicious)
	fileStatus := models.FileStatusReady
	if isSuspicious {
		fileStatus = models.FileStatusQuarantined
	}

	var expiresAt *time.Time
	if upload.ExpiryDays > 0 {
		t := time.Now().AddDate(0, 0, upload.ExpiryDays)
		expiresAt = &t
	}

	fileRecord := &models.FileRecord{
		ID:           fileID,
		PersonID:     upload.PersonID,
		OriginalName: upload.OriginalName,
		StoredPath:   relStoredPath,
		Size:         actualSize,
		ContentType:  detectContentType(upload.OriginalName),
		Status:       fileStatus,
		Flagged:      isSuspicious,
		FlagReason:   reason,
		Protected:    false,
		KeepForever:  false,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
		ClientIPHash: upload.ClientIPHash,
	}

	// Create DB record BEFORE atomic rename
	tx, err := m.db.Begin()
	if err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: ошибка открытия транзакции БД: %v", err)
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO files (id, person_id, original_name, stored_path, size, content_type, status, flagged, flag_reason, protected, keep_forever, expires_at, created_at, client_ip_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, fileRecord.ID, fileRecord.PersonID, fileRecord.OriginalName, fileRecord.StoredPath, fileRecord.Size, fileRecord.ContentType, fileRecord.Status, fileRecord.Flagged, fileRecord.FlagReason, fileRecord.Protected, fileRecord.KeepForever, fileRecord.ExpiresAt, fileRecord.CreatedAt, fileRecord.ClientIPHash)

	if err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: ошибка создания записи в таблице files (person_id=%d): %v", fileRecord.PersonID, err)
		return nil, fmt.Errorf("ошибка сохранения файла в БД: %w", err)
	}

	now := time.Now()
	_, err = tx.Exec("UPDATE uploads SET status = ?, completed_at = ?, received_bytes = ? WHERE id = ?",
		models.UploadStatusCompleted, now, actualSize, uploadID)
	if err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: ошибка обновления статуса uploads: %v", err)
		return nil, fmt.Errorf("ошибка обновления статуса загрузки: %w", err)
	}

	// Atomic rename from .part to final storage path
	if err := os.Rename(partialPath, finalFullPath); err != nil {
		log.Printf("[UPLOAD ERROR] FinalizeUpload: ошибка атомарного переименования (%s -> %s): %v", partialPath, finalFullPath, err)
		return nil, fmt.Errorf("ошибка атомарного переименования файла: %w", err)
	}

	if err := tx.Commit(); err != nil {
		// If DB commit fails, try to rollback physical file rename if possible
		os.Rename(finalFullPath, partialPath)
		log.Printf("[UPLOAD ERROR] FinalizeUpload: ошибка фиксации (commit) транзакции: %v", err)
		return nil, fmt.Errorf("ошибка фиксации транзакции в БД: %w", err)
	}

	// Track completed traffic
	m.traffic.AddUploadCompleted(upload.PersonID, actualSize)
	log.Printf("[UPLOAD SUCCESS] Файл '%s' (ID=%s, Size=%d bytes, PersonID=%d) успешно загружен и зарегистрирован в БД", fileRecord.OriginalName, fileRecord.ID, fileRecord.Size, fileRecord.PersonID)

	return fileRecord, nil
}

func (m *Manager) CancelUpload(uploadID string, secret string, reason string) error {
	upload, err := m.GetUpload(uploadID)
	if err != nil {
		return err
	}

	if secret != "" && auth.HashString(secret) != upload.UploadSecretHash {
		return errors.New("неверный секретный ключ загрузки")
	}

	// Remove partial file if exists
	partialPath := m.storage.GetPartialPath(uploadID)
	os.Remove(partialPath)

	m.db.Exec("UPDATE uploads SET status = ? WHERE id = ?", models.UploadStatusCanceled, uploadID)

	// Record aborted traffic
	if upload.ReceivedBytes > 0 {
		m.traffic.AddUploadAborted(upload.PersonID, upload.ReceivedBytes)
	}
	return nil
}

func detectContentType(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

func Round(val float64) int64 {
	return int64(math.Round(val))
}
