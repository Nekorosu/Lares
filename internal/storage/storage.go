package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"homeshare/internal/config"
)

type Storage struct {
	cfg *config.Config
}

func NewStorage(cfg *config.Config) *Storage {
	return &Storage{cfg: cfg}
}

func (s *Storage) SanitizeFilename(filename string) string {
	// Remove directory elements
	clean := filepath.Base(filename)
	clean = strings.ReplaceAll(clean, "..", "")
	clean = strings.ReplaceAll(clean, "/", "_")
	clean = strings.ReplaceAll(clean, "\\", "_")
	clean = strings.TrimSpace(clean)

	if len(clean) == 0 {
		clean = "file"
	}
	if len(clean) > 255 {
		ext := filepath.Ext(clean)
		base := clean[:255-len(ext)]
		clean = base + ext
	}
	return clean
}

func (s *Storage) IsSuspicious(filename string, customSuspicious []string) (bool, string) {
	lower := strings.ToLower(filename)
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")

	// Check suspicious list
	suspiciousList := s.cfg.Suspicious
	if len(customSuspicious) > 0 {
		suspiciousList = customSuspicious
	}

	for _, sExt := range suspiciousList {
		if strings.EqualFold(ext, strings.TrimPrefix(sExt, ".")) {
			return true, fmt.Sprintf("подозрительное расширение .%s", ext)
		}
	}

	// Double extension check (e.g. file.pdf.exe)
	parts := strings.Split(lower, ".")
	if len(parts) > 2 {
		secondToLast := parts[len(parts)-2]
		last := parts[len(parts)-1]
		commonDocs := map[string]bool{"pdf": true, "doc": true, "docx": true, "jpg": true, "png": true, "txt": true}
		commonExec := map[string]bool{"exe": true, "bat": true, "scr": true, "cmd": true, "vbs": true, "ps1": true}

		if commonDocs[secondToLast] && commonExec[last] {
			return true, fmt.Sprintf("двойное расширение .%s.%s", secondToLast, last)
		}
	}

	return false, ""
}

func (s *Storage) GenerateStoredPath(fileID string) (string, error) {
	if len(fileID) < 4 {
		b := make([]byte, 16)
		rand.Read(b)
		fileID = hex.EncodeToString(b)
	}
	dir1 := fileID[0:2]
	dir2 := fileID[2:4]
	relPath := filepath.Join(dir1, dir2, fileID)
	fullDir := filepath.Join(s.cfg.Paths.DataDir, dir1, dir2)

	if err := os.MkdirAll(fullDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create storage dir: %w", err)
	}

	return relPath, nil
}

func (s *Storage) GetFullPath(storedPath string) string {
	return filepath.Join(s.cfg.Paths.DataDir, storedPath)
}

func (s *Storage) GetPartialPath(uploadID string) string {
	return filepath.Join(s.cfg.Paths.TmpDir, fmt.Sprintf("%s.part", uploadID))
}

type DiskStats struct {
	FreeBytes      uint64
	TotalBytes     uint64
	FreeInodes     uint64
	IsMinSpace     bool
	IsCriticalSpace bool
	IsMinInodes    bool
}

func (s *Storage) CheckDiskSpace() (DiskStats, error) {
	var stat syscall.Statfs_t
	path := s.cfg.Paths.DataDir

	if err := syscall.Statfs(path, &stat); err != nil {
		// Fallback to parent if data dir stat fails
		path = filepath.Dir(path)
		if err2 := syscall.Statfs(path, &stat); err2 != nil {
			return DiskStats{}, err
		}
	}

	freeBytes := stat.Bavail * uint64(stat.Bsize)
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeInodes := stat.Ffree

	minSpaceBytes := uint64(s.cfg.DiskReserve.MinFreeSpaceGB) * 1024 * 1024 * 1024
	criticalSpaceBytes := uint64(s.cfg.DiskReserve.CriticalFreeSpaceGB) * 1024 * 1024 * 1024

	return DiskStats{
		FreeBytes:       freeBytes,
		TotalBytes:      totalBytes,
		FreeInodes:      freeInodes,
		IsMinSpace:      freeBytes < minSpaceBytes,
		IsCriticalSpace: freeBytes < criticalSpaceBytes,
		IsMinInodes:     freeInodes < s.cfg.DiskReserve.MinFreeInodes,
	}, nil
}

func (s *Storage) VerifyDiskCanAcceptUpload(declaredSize int64) error {
	stats, err := s.CheckDiskSpace()
	if err != nil {
		return nil // Non-blocking if OS stat unavailable
	}
	if stats.IsCriticalSpace {
		return errors.New("критический остаток дискового пространства (<20GB), загрузка заблокирована")
	}
	if stats.IsMinSpace {
		return errors.New("недостаточно дискового пространства (<40GB), новые загрузки запрещены")
	}
	if stats.IsMinInodes {
		return errors.New("недостаточно свободных inodes на сервере")
	}
	if stats.FreeBytes < uint64(declaredSize) {
		return errors.New("недостаточно свободного места на диске для заявляемого файла")
	}
	return nil
}
