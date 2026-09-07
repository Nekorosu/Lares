package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentSecretsAndStrictConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if e := Initialize(p); e != nil {
		t.Fatal(e)
	}
	a, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	b, e := LoadConfig(p)
	if e != nil {
		t.Fatal(e)
	}
	if a.Secrets != b.Secrets || len(a.Secrets.SessionSecret) < 32 {
		t.Fatal("unstable secrets")
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if Initialize(p) == nil {
		t.Fatal("overwrote existing secrets")
	}
	if _, e := LoadConfig(p + "missing"); e == nil {
		t.Fatal("missing config accepted")
	}
	t.Setenv("LARES_CONFIG", p)
	t.Setenv("LARES_IP_SALT", strings.Repeat("z", 32))
	c, e := LoadConfig("")
	if e != nil || c.Secrets.IPHashSalt != strings.Repeat("z", 32) {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("unknown_key: true\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadConfig(p); e == nil {
		t.Fatal("unknown config accepted")
	}
}
func TestConfigGuards(t *testing.T) {
	for _, mutate := range []func(*Config){func(c *Config) { c.Listen = "0.0.0.0:8090" }, func(c *Config) { c.DataDir = "/srv/media/fileshare-other" }, func(c *Config) { c.DataDir = "../data" }, func(c *Config) { c.SpeedLimits.ExternalUploadMbps = 9 }, func(c *Config) { c.StorageDefaults.QuotaBytes = -1 }, func(c *Config) { c.ZipLimits.MaxFiles = 0 }, func(c *Config) { c.SessionDefaults.AdminAbsoluteDays = 0 }, func(c *Config) { c.ExpiryOptions = []int{1, 7} }, func(c *Config) { c.DiskReserve.MinFreeSpaceGB = 1 << 40 }} {
		c := DefaultConfig()
		c.Secrets = Secrets{strings.Repeat("a", 32), strings.Repeat("b", 32)}
		mutate(c)
		if c.Validate() == nil {
			t.Fatalf("invalid config accepted: %+v", c)
		}
	}
}
