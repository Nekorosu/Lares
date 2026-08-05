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
	logPath  string
	file     *os.File
}

func NewLogger(logPath string) (*Logger, error) {
	if logPath == "" {
		logPath = "/var/log/lares/security.log"
	}
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		// fallback to tmp if fail
		logPath = "/tmp/lares_security.log"
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open security log: %w", err)
	}

	return &Logger{
		logPath: logPath,
		file:    f,
	}, nil
}

func (l *Logger) LogEvent(ip string, event string, details string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] [SECURITY] ip=%s event=%s details=\"%s\"\n", timestamp, ip, event, details)
	l.file.WriteString(line)
}

func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
	}
}
