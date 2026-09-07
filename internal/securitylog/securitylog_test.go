package securitylog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionAndPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "security.log")
	l, e := NewLogger(p)
	if e != nil {
		t.Fatal(e)
	}
	old := time.Now().AddDate(0, 0, -8).UTC().Format(time.RFC3339) + " [invite_failed] ip=198.51.100.1 old\n"
	if e = os.WriteFile(p, []byte(old), 0600); e != nil {
		t.Fatal(e)
	}
	l.LogEvent("invite_failed", "198.51.100.2", "failure\nforged")
	if e = l.Rotate(); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "198.51.100.1") || !strings.Contains(string(b), "198.51.100.2") || strings.Count(string(b), "\n") != 1 {
		t.Fatal(string(b))
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}
