package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCanonicalLaresConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
listen: "0.0.0.0:9123"
base_url: "https://files.example.test"
paths:
  data_dir: "/srv/test/data"
  tmp_dir: "/srv/test/tmp"
  db_path: "/srv/test/db/lares.db"
  backup_dir: "/home/test-backup"
  security_log: "/var/log/lares/security-test.log"
network:
  local_cidrs: ["127.0.0.1/32", "10.0.0.0/8"]
limits:
  default_storage_quota_gb: 11
  default_monthly_upload_limit_gb: 22
  default_monthly_download_limit_gb: 33
  default_max_file_size_gb: 4
  max_concurrent_uploads: 3
  default_expiry_days: 9
  allow_user_keep_forever: true
  quarantine_suspicious: false
speed_limits:
  external_upload_limit_mbps: 101
  external_download_limit_mbps: 202
  burst_mb: 7
zip_limits:
  max_files: 12
  max_total_gb: 13
sessions:
  user_idle_days: 5
  user_absolute_days: 6
  admin_idle_hours: 7
  admin_absolute_days: 8
invite_defaults:
  expiry_days: 19
  max_activations: 2
disk_reserve:
  min_free_space_gb: 14
  critical_free_space_gb: 10
  min_free_inodes: 1234
secrets:
  session_secret: "session-test-secret"
  ip_salt: "ip-test-salt"
security_log: "/must/not/override/nested/path"
`
	if err := os.WriteFile(path, []byte(contents), 0640); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Listen != "0.0.0.0:9123" || cfg.BaseURL != "https://files.example.test" {
		t.Fatalf("top-level values not loaded: %#v", cfg)
	}
	if cfg.Paths.DataDir != "/srv/test/data" || cfg.Paths.TmpDir != "/srv/test/tmp" || cfg.Paths.DBPath != "/srv/test/db/lares.db" {
		t.Fatalf("paths not loaded: %#v", cfg.Paths)
	}
	if cfg.Paths.SecurityLog != "/var/log/lares/security-test.log" {
		t.Fatalf("nested security_log lost precedence: %q", cfg.Paths.SecurityLog)
	}
	if len(cfg.Network.LocalCIDRs) != 2 || cfg.Network.LocalCIDRs[1] != "10.0.0.0/8" {
		t.Fatalf("network.local_cidrs not loaded: %#v", cfg.Network.LocalCIDRs)
	}
	if cfg.Limits.DefaultStorageQuotaGB != 11 || cfg.Limits.MaxConcurrentUploads != 3 || cfg.Limits.QuarantineSuspicious {
		t.Fatalf("limits not loaded: %#v", cfg.Limits)
	}
	if cfg.Sessions.AdminIdleHours != 7 || cfg.Sessions.UserAbsoluteDays != 6 {
		t.Fatalf("sessions not loaded: %#v", cfg.Sessions)
	}
	if cfg.InviteDefaults.ExpiryDays != 19 || cfg.InviteDefaults.MaxActivations != 2 {
		t.Fatalf("invite_defaults not loaded: %#v", cfg.InviteDefaults)
	}
	if cfg.Secrets.SessionSecret != "session-test-secret" || cfg.Secrets.IPSalt != "ip-test-salt" {
		t.Fatalf("secrets not loaded: %#v", cfg.Secrets)
	}
}

func TestGeneratedSecretsArePersistedInCanonicalSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:8090\nsecrets: {}\n"), 0640); err != nil {
		t.Fatal(err)
	}

	first, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("first LoadConfig() error = %v", err)
	}
	if len(first.Secrets.SessionSecret) != 64 || len(first.Secrets.IPSalt) != 64 {
		t.Fatalf("generated secrets have unexpected lengths: %#v", first.Secrets)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "ip_salt:") || strings.Contains(text, "ip_hash_salt:") {
		t.Fatalf("secrets were not saved using canonical keys:\n%s", text)
	}

	second, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("second LoadConfig() error = %v", err)
	}
	if first.Secrets != second.Secrets {
		t.Fatalf("secrets changed after reload: first=%#v second=%#v", first.Secrets, second.Secrets)
	}
}

func TestLegacySchemaCompatibilityDoesNotChangeDefaultPath(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "homeshare.yaml")
	legacy := `
data_dir: /legacy/data
security_log: /legacy/security.log
local_cidr: 10.2.0.0/16
secrets:
  session_secret: old-session-secret
  ip_hash_salt: old-ip-salt
storage_defaults:
  quota_bytes: 10737418240
session_defaults:
  user_idle_days: 4
  user_absolute_days: 40
  admin_idle_hours: 5
  admin_absolute_days: 6
`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0640); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(legacyPath)
	if err != nil {
		t.Fatalf("explicit legacy LoadConfig() error = %v", err)
	}
	if cfg.Paths.DataDir != "/legacy/data" || cfg.Limits.DefaultStorageQuotaGB != 10 || cfg.Secrets.IPSalt != "old-ip-salt" {
		t.Fatalf("legacy migration failed: %#v", cfg)
	}
	if got := resolveConfigPath(""); got != DefaultConfigPath {
		t.Fatalf("empty path resolved to %q, want %q", got, DefaultConfigPath)
	}
	if strings.Contains(resolveConfigPath(""), "homeshare") {
		t.Fatalf("default resolution unexpectedly uses legacy path: %q", resolveConfigPath(""))
	}
}
