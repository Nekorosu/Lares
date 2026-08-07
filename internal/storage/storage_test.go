package storage

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.txt", "normal.txt"},
		{"../../../etc/passwd", "passwd"},
		{"C:\\Windows\\System32\\cmd.exe", "cmd.exe"},
		{"..\\..\\secret.pdf", "secret.pdf"},
		{"   .hidden_file  ", "hidden_file"},
	}

	for _, tt := range tests {
		res := SanitizeFilename(tt.input)
		if res != tt.expected {
			t.Errorf("SanitizeFilename(%q) = %q; want %q", tt.input, res, tt.expected)
		}
	}
}
