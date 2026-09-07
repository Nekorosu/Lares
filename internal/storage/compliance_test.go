package storage

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestFilenameBoundsAndControls(t *testing.T) {
	for _, s := range []string{"x." + strings.Repeat("e", 400), strings.Repeat("я", 300) + ".txt", "../../\x00a\nb.txt", "\xfffoo"} {
		got := SanitizeFilename(s)
		if len(got) > 255 || !utf8.ValidString(got) || strings.ContainsAny(got, "/\\") {
			t.Fatal(got)
		}
		for _, c := range got {
			if unicode.IsControl(c) {
				t.Fatal("control", got)
			}
		}
	}
}
func TestStrictDiskAndInodeReservation(t *testing.T) {
	for _, tt := range []struct {
		free, inodes, required, pending, min, minInodes int64
		ok                                              bool
	}{{100, 10, 40, 40, 20, 1, true}, {100, 10, 41, 40, 20, 1, false}, {100, 1, 1, 0, 20, 1, false}, {100, 10, math.MaxInt64, 50, 20, 1, false}, {19, 10, 0, 0, 20, 1, false}} {
		e := CheckReserve(tt.free, tt.inodes, tt.required, tt.pending, tt.min, tt.minInodes)
		if (e == nil) != tt.ok {
			t.Fatalf("%+v: %v", tt, e)
		}
	}
}
func TestSymlinkContainmentAndModes(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	s, e := NewStorageManager(data, filepath.Join(root, "tmp"), 0, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	outside := filepath.Join(root, "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(root, filepath.Join(data, "escape"))
	os.Symlink(outside, filepath.Join(data, "link"))
	for _, p := range []string{"../secret", outside, "escape/secret", "link", data + "-other/secret"} {
		if f, e := s.Open(p); e == nil {
			f.Close()
			t.Fatalf("opened %s", p)
		}
	}
	f, part, e := s.PreparePartFile("safe")
	if e != nil {
		t.Fatal(e)
	}
	f.Write([]byte("bytes"))
	info, _ := f.Stat()
	f.Close()
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
	dest, e := s.FinalizeUpload("safe", "finished")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(part); !os.IsNotExist(e) {
		t.Fatal("part remains")
	}
	if e = s.DeleteFile(dest); e != nil {
		t.Fatal(e)
	}
	bytes, _ := os.ReadFile(outside)
	if string(bytes) != "secret" {
		t.Fatal("escaped mutation")
	}
}

func TestDirectoryAncestorSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "linked"))
	if s, e := NewStorageManager(filepath.Join(root, "linked", "data"), filepath.Join(root, "tmp"), 0, 0, 0); e == nil {
		s.Close()
		t.Fatal("followed ancestor symlink")
	}
	if _, e := os.Stat(filepath.Join(outside, "data")); !os.IsNotExist(e) {
		t.Fatal("created data outside")
	}
}
