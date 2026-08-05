package zip

import (
	"archive/zip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"lares/internal/models"
	"lares/internal/speedlimit"
	"lares/internal/storage"
	"lares/internal/traffic"
)

type ZipService struct {
	db           *sql.DB
	storage      *storage.Storage
	traffic      *traffic.Tracker
	speedManager *speedlimit.SpeedManager
	speedTracker *speedlimit.SpeedTracker
}

func NewZipService(db *sql.DB, st *storage.Storage, tr *traffic.Tracker, sm *speedlimit.SpeedManager, stTr *speedlimit.SpeedTracker) *ZipService {
	return &ZipService{
		db:           db,
		storage:      st,
		traffic:      tr,
		speedManager: sm,
		speedTracker: stTr,
	}
}

func (zs *ZipService) StreamZip(w http.ResponseWriter, r *http.Request, fileIDs []string, person *models.Person, isAdmin bool, isLocal bool, maxFiles int, maxTotalGB int64) error {
	if len(fileIDs) == 0 {
		return errors.New("не выбрано ни одного файла")
	}
	if len(fileIDs) > maxFiles {
		return fmt.Errorf("превышено максимальное количество файлов в архиве (макс: %d)", maxFiles)
	}

	// Fetch files
	query := fmt.Sprintf("SELECT id, person_id, original_name, stored_path, size, status FROM files WHERE id IN ('%s')",
		strings.Join(fileIDs, "','"))

	rows, err := zs.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var files []models.FileRecord
	var totalSize int64

	for rows.Next() {
		var f models.FileRecord
		if err := rows.Scan(&f.ID, &f.PersonID, &f.OriginalName, &f.StoredPath, &f.Size, &f.Status); err != nil {
			return err
		}
		if f.Status == models.FileStatusQuarantined && !isAdmin && f.PersonID != person.ID {
			continue // exclude quarantined files
		}
		files = append(files, f)
		totalSize += f.Size
	}

	maxTotalBytes := maxTotalGB * 1024 * 1024 * 1024
	if totalSize > maxTotalBytes {
		return fmt.Errorf("общий размер файлов в архиве (%d GB) превышает лимит (%d GB)",
			totalSize/(1024*1024*1024), maxTotalGB)
	}

	// Check download traffic grace
	if !isLocal && person != nil && !person.IgnoreTrafficQuota {
		if err := zs.traffic.CheckDownloadTrafficGrace(person, 0, totalSize, isLocal); err != nil {
			return err
		}
	}

	zipName := fmt.Sprintf("lares_archive_%s.zip", time.Now().Format("20060102_150405"))

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipName))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var outWriter io.Writer = w
	if !isLocal && zs.speedManager != nil {
		limiter := zs.speedManager.GetDownloadLimiter()
		outWriter = speedlimit.NewThrottledWriter(r.Context(), w, limiter, isLocal)
	}

	cw := &zipCountingWriter{Writer: outWriter, speedTracker: zs.speedTracker}
	zipWriter := zip.NewWriter(cw)

	for _, f := range files {
		fullPath := zs.storage.GetFullPath(f.StoredPath)
		fileOnDisk, err := os.Open(fullPath)
		if err != nil {
			continue
		}

		header := &zip.FileHeader{
			Name:   f.OriginalName,
			Method: zip.Store, // Store mode (no compression)
		}
		header.SetMode(0644)

		fw, err := zipWriter.CreateHeader(header)
		if err != nil {
			fileOnDisk.Close()
			continue
		}

		io.Copy(fw, fileOnDisk)
		fileOnDisk.Close()
	}

	if err := zipWriter.Close(); err != nil {
		return err
	}

	if person != nil {
		if cw.bytesWritten >= totalSize {
			zs.traffic.AddDownloadCompleted(person.ID, cw.bytesWritten)
		} else if cw.bytesWritten > 0 {
			zs.traffic.AddDownloadAborted(person.ID, cw.bytesWritten)
		}
	}

	return nil
}

type zipCountingWriter struct {
	io.Writer
	bytesWritten int64
	speedTracker *speedlimit.SpeedTracker
}

func (zcw *zipCountingWriter) Write(p []byte) (int, error) {
	n, err := zcw.Writer.Write(p)
	zcw.bytesWritten += int64(n)
	if zcw.speedTracker != nil {
		zcw.speedTracker.RecordDownload(int64(n))
	}
	return n, err
}
