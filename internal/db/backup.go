package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Backup copies an existing SQLite database without running any schema migration.
func Backup(source, destination string) error {
	if _, e := os.Stat(source); e != nil {
		return e
	}
	if _, e := os.Lstat(destination); e == nil {
		return fmt.Errorf("резервная копия уже существует")
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(destination), 0750); e != nil {
		return e
	}
	d, e := sql.Open("sqlite", source+"?_pragma=busy_timeout(5000)")
	if e != nil {
		return e
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, e = d.ExecContext(ctx, "VACUUM INTO ?", destination); e != nil {
		return e
	}
	return os.Chmod(destination, 0640)
}
