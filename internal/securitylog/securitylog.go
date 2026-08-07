package securitylog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	mu       sync.Mutex
	filePath string
}

func NewLogger(filePath string) (*Logger, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log dir: %w", err)
	}

	// Ensure file exists with 0600 permissions
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open security log: %w", err)
	}
	_ = f.Chmod(0600)
	_ = f.Close()

	return &Logger{filePath: filePath}, nil
}

func (l *Logger) LogEvent(event string, rawIP string, details string) {
	if l == nil || l.filePath == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s [%s] ip=%s %s\n", timestamp, event, rawIP, details)

	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = f.WriteString(line)
}
