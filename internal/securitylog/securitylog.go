package securitylog

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	mu       sync.Mutex
	filePath string
}

func NewLogger(p string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0750); err != nil {
		return nil, err
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	e = f.Chmod(0600)
	f.Close()
	return &Logger{filePath: p}, e
}
func (l *Logger) LogEvent(event, ip, details string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, e := os.OpenFile(l.filePath, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		log.Printf("security journal: %v", e)
		return
	}
	defer f.Close()
	if _, e = fmt.Fprintf(f, "%s [%s] ip=%s %q\n", time.Now().UTC().Format(time.RFC3339), event, ip, details); e != nil {
		log.Printf("security journal: %v", e)
	}
}
func (l *Logger) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	in, e := os.Open(l.filePath)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.CreateTemp(filepath.Dir(l.filePath), ".security-*")
	if e != nil {
		return e
	}
	defer os.Remove(out.Name())
	defer out.Close()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 4096), 1024*1024)
	cut := time.Now().AddDate(0, 0, -7)
	for sc.Scan() {
		line := sc.Text()
		stamp, _, _ := strings.Cut(line, " ")
		t, err := time.Parse(time.RFC3339, stamp)
		if err == nil && !t.Before(cut) {
			if _, e = fmt.Fprintln(out, line); e != nil {
				return e
			}
		}
	}
	if e = sc.Err(); e != nil {
		return e
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	return os.Rename(out.Name(), l.filePath)
}
