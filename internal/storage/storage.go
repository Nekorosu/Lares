package storage

import (
	"errors"
	"io"
	"lares/internal/auth"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

var ErrInsufficientDiskSpace = errors.New("недостаточно свободного места")
var ErrInsufficientInodes = errors.New("недостаточно свободных inode")
var ErrCriticalDiskSpace = errors.New("критическая нехватка места")

type StorageManager struct {
	dataDir, tmpDir                                    string
	minFreeSpaceGB, criticalFreeSpaceGB, minFreeInodes int64
	root                                               *os.File
}

// Resolve each directory component from / using dirfds; no ancestor symlink is followed.
func openDirectory(path string) (int, error) {
	if !filepath.IsAbs(path) {
		return -1, errors.New("требуется абсолютный каталог")
	}
	fd, e := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return -1, e
	}
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if component == "" {
			continue
		}
		if e = syscall.Mkdirat(fd, component, 0750); e != nil && e != syscall.EEXIST {
			syscall.Close(fd)
			return -1, e
		}
		next, err := syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if err != nil {
			return -1, err
		}
		fd = next
	}
	if e = syscall.Fchmod(fd, 0750); e != nil {
		syscall.Close(fd)
		return -1, e
	}
	return fd, nil
}
func NewStorageManager(data, tmp string, min, critical, inodes int64) (*StorageManager, error) {
	fd, e := openDirectory(data)
	if e != nil {
		return nil, e
	}
	t, e := openDirectory(tmp)
	if e != nil {
		syscall.Close(fd)
		return nil, e
	}
	syscall.Close(t)
	return &StorageManager{data, tmp, min, critical, inodes, os.NewFile(uintptr(fd), data)}, nil
}
func (s *StorageManager) Close() error { return s.root.Close() }
func (s *StorageManager) GetDiskUsage() (int64, int64, int64, error) {
	var st syscall.Statfs_t
	err := syscall.Fstatfs(int(s.root.Fd()), &st)
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), int64(st.Ffree), err
}
func CheckReserve(free, inodes, required, pending, min, minInodes int64) error {
	if required < 0 || pending < 0 || free < min || pending > free-min || required > free-min-pending {
		return ErrInsufficientDiskSpace
	}
	if inodes <= minInodes {
		return ErrInsufficientInodes
	}
	return nil
}
func (s *StorageManager) CheckDiskSpaceForNewUpload(n int64) error { return s.CheckReserved(n, 0) }
func (s *StorageManager) CheckReserved(n, pending int64) error {
	free, _, inodes, err := s.GetDiskUsage()
	if err != nil {
		return err
	}
	return CheckReserve(free, inodes, n, pending, s.minFreeSpaceGB<<30, s.minFreeInodes)
}
func (s *StorageManager) CheckDiskSpaceCritical() error {
	free, _, _, err := s.GetDiskUsage()
	if err != nil {
		return err
	}
	if free < s.criticalFreeSpaceGB<<30 {
		return ErrCriticalDiskSpace
	}
	return nil
}
func (s *StorageManager) relative(p string) (string, error) {
	if filepath.IsAbs(p) {
		var err error
		p, err = filepath.Rel(s.dataDir, p)
		if err != nil {
			return "", err
		}
	}
	if p == "" || p == "." || strings.Contains(p, "\\") {
		return "", errors.New("небезопасный путь")
	}
	for _, v := range strings.Split(p, "/") {
		if v == "" || v == "." || v == ".." {
			return "", errors.New("небезопасный путь")
		}
	}
	return p, nil
}

// Walk parent directories through dirfds, never following symlinks, including races.
func (s *StorageManager) parent(p string, create bool) (int, string, error) {
	rel, err := s.relative(p)
	if err != nil {
		return -1, "", err
	}
	parts := strings.Split(rel, "/")
	fd, err := syscall.Dup(int(s.root.Fd()))
	if err != nil {
		return -1, "", err
	}
	for _, v := range parts[:len(parts)-1] {
		if create {
			e := syscall.Mkdirat(fd, v, 0750)
			if e != nil && e != syscall.EEXIST {
				syscall.Close(fd)
				return -1, "", e
			}
		}
		next, e := syscall.Openat(fd, v, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if e != nil {
			return -1, "", e
		}
		fd = next
	}
	return fd, parts[len(parts)-1], nil
}
func (s *StorageManager) Open(p string) (*os.File, error) { return s.open(p, syscall.O_RDONLY, false) }
func (s *StorageManager) open(p string, flags int, create bool) (*os.File, error) {
	fd, name, err := s.parent(p, create)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	f, err := syscall.Openat(fd, name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0640)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(f), p)
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("ожидался обычный файл")
	}
	if err = file.Chmod(0640); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func (s *StorageManager) GetShardedPath(id string) string {
	if strings.ContainsAny(id, "/\\.") || id == "" {
		return ""
	}
	h := auth.HashString(id)
	return filepath.Join(s.dataDir, h[:2], h[2:4], id)
}
func (s *StorageManager) GetPartPath(id string) string {
	p := s.GetShardedPath(id)
	if p == "" {
		return ""
	}
	return p + ".part"
}
func (s *StorageManager) PreparePartFile(id string) (*os.File, string, error) {
	p := s.GetPartPath(id)
	f, err := s.open(p, syscall.O_CREAT|syscall.O_RDWR, true)
	return f, p, err
}
func (s *StorageManager) FinalizeUpload(id, fileID string) (string, error) {
	old := s.GetPartPath(id)
	dest := s.GetShardedPath(fileID)
	a, an, err := s.parent(old, false)
	if err != nil {
		return "", err
	}
	defer syscall.Close(a)
	b, bn, err := s.parent(dest, true)
	if err != nil {
		return "", err
	}
	defer syscall.Close(b)
	f, err := s.Open(old)
	if err != nil {
		return "", err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return "", err
	}
	if err = syscall.Renameat(a, an, b, bn); err != nil {
		return "", err
	}
	if err = syscall.Fsync(a); err != nil {
		return "", err
	}
	if err = syscall.Fsync(b); err != nil {
		return "", err
	}
	return dest, nil
}
func (s *StorageManager) DeleteFile(p string) error {
	fd, name, err := s.parent(p, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	err = syscall.Unlinkat(fd, name)
	if err == syscall.ENOENT {
		return nil
	}
	if err != nil {
		return err
	}
	return syscall.Fsync(fd)
}
func (s *StorageManager) DeletePartFile(id string) error { return s.DeleteFile(s.GetPartPath(id)) }
func (s *StorageManager) ResolvePath(p string) string {
	rel, err := s.relative(p)
	if err != nil {
		return ""
	}
	return filepath.Join(s.dataDir, rel)
}
func (s *StorageManager) Relative(p string) (string, error) { return s.relative(p) }
func (s *StorageManager) Walk(fn func(string, os.FileInfo) error) error {
	return filepath.Walk(s.dataDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink в хранилище")
		}
		if info.IsDir() {
			return nil
		}
		return fn(p, info)
	})
}
func SanitizeFilename(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	s = strings.TrimLeft(strings.TrimSpace(s), ". ")
	if s == "" {
		s = "без-имени"
	}
	ext := filepath.Ext(s)
	if len(ext) > 32 {
		ext = ""
	}
	if len(s) > 255 {
		n := 255 - len(ext)
		prefix := s[:n]
		for !utf8.ValidString(prefix) {
			prefix = prefix[:len(prefix)-1]
		}
		s = prefix + ext
	}
	return s
}
func Sniff(f *os.File) (string, error) {
	b := make([]byte, 512)
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return "", err
	}
	n, err := f.Read(b)
	if err != nil && err != io.EOF {
		return "", err
	}
	return string(b[:n]), nil
}
