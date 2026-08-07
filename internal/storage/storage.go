package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"lares/internal/auth"
)

var (
	ErrInsufficientDiskSpace = errors.New("недостаточно свободного места на сервере")
	ErrCriticalDiskSpace     = errors.New("критический дефицит места на диске")
	ErrInsufficientInodes    = errors.New("недостаточно свободных inodes на сервере")
)

type StorageManager struct {
	dataDir             string
	tmpDir              string
	minFreeSpaceGB      int64
	criticalFreeSpaceGB int64
	minFreeInodes       int64
}

func NewStorageManager(dataDir, tmpDir string, minFreeGB, criticalFreeGB, minInodes int64) (*StorageManager, error) {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create tmp directory: %w", err)
	}

	return &StorageManager{
		dataDir:             dataDir,
		tmpDir:              tmpDir,
		minFreeSpaceGB:      minFreeGB,
		criticalFreeSpaceGB: criticalFreeGB,
		minFreeInodes:       minInodes,
	}, nil
}

func (sm *StorageManager) UpdateReserves(minFreeGB, criticalFreeGB, minInodes int64) {
	sm.minFreeSpaceGB = minFreeGB
	sm.criticalFreeSpaceGB = criticalFreeGB
	sm.minFreeInodes = minInodes
}

func (sm *StorageManager) GetDiskUsage() (freeBytes int64, totalBytes int64, freeInodes int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(sm.dataDir, &stat); err != nil {
		return 0, 0, 0, err
	}

	freeBytes = int64(stat.Bavail) * int64(stat.Bsize)
	totalBytes = int64(stat.Blocks) * int64(stat.Bsize)
	freeInodes = int64(stat.Ffree)
	return freeBytes, totalBytes, freeInodes, nil
}

func (sm *StorageManager) CheckDiskSpaceForNewUpload(requiredBytes int64) error {
	freeBytes, _, freeInodes, err := sm.GetDiskUsage()
	if err != nil {
		return fmt.Errorf("failed to check disk space: %w", err)
	}

	minBytes := sm.minFreeSpaceGB * 1024 * 1024 * 1024
	if freeBytes < minBytes || (freeBytes-requiredBytes) < minBytes {
		return ErrInsufficientDiskSpace
	}

	if freeInodes < sm.minFreeInodes {
		return ErrInsufficientInodes
	}

	return nil
}

func (sm *StorageManager) CheckDiskSpaceCritical() error {
	freeBytes, _, _, err := sm.GetDiskUsage()
	if err != nil {
		return err
	}

	criticalBytes := sm.criticalFreeSpaceGB * 1024 * 1024 * 1024
	if freeBytes < criticalBytes {
		return ErrCriticalDiskSpace
	}

	return nil
}

func (sm *StorageManager) GetShardedPath(fileOrUploadID string) string {
	idHash := auth.HashString(fileOrUploadID)
	if len(idHash) < 4 {
		return filepath.Join(sm.dataDir, fileOrUploadID)
	}
	shard1 := idHash[:2]
	shard2 := idHash[2:4]
	return filepath.Join(sm.dataDir, shard1, shard2, fileOrUploadID)
}

func (sm *StorageManager) GetPartPath(uploadID string) string {
	sharded := sm.GetShardedPath(uploadID)
	return sharded + ".part"
}

func (sm *StorageManager) PreparePartFile(uploadID string) (*os.File, string, error) {
	partPath := sm.GetPartPath(uploadID)
	dir := filepath.Dir(partPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, "", fmt.Errorf("failed to create directory for upload: %w", err)
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open part file: %w", err)
	}

	return f, partPath, nil
}

func (sm *StorageManager) FinalizeUpload(uploadID, fileID string) (string, error) {
	partPath := sm.GetPartPath(uploadID)
	finalPath := sm.GetShardedPath(fileID)

	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create directory for final file: %w", err)
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		return "", fmt.Errorf("failed to rename part file to final path: %w", err)
	}

	_ = os.Chmod(finalPath, 0640)
	return finalPath, nil
}

func (sm *StorageManager) DeletePartFile(uploadID string) {
	partPath := sm.GetPartPath(uploadID)
	_ = os.Remove(partPath)
}

func (sm *StorageManager) DeleteFile(storedPath string) error {
	if !strings.HasPrefix(filepath.Clean(storedPath), filepath.Clean(sm.dataDir)) {
		return errors.New("invalid stored path out of data dir boundary")
	}
	return os.Remove(storedPath)
}

var invalidFilenameChars = regexp.MustCompile(`[^\w\.\-\s\(\)\[\]]`)

func SanitizeFilename(originalName string) string {
	// Replace backslashes with forward slashes for cross-platform safety
	normalized := strings.ReplaceAll(originalName, "\\", "/")
	filename := filepath.Base(normalized)
	filename = strings.TrimSpace(filename)


	// Remove leading dots or slashes
	filename = strings.TrimLeft(filename, ".\\/")

	if filename == "" {
		filename = "unnamed_file"
	}

	// Truncate to 255 chars
	if len(filename) > 255 {
		ext := filepath.Ext(filename)
		base := filename[:255-len(ext)]
		filename = base + ext
	}

	return filename
}
