package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/lares/config.yaml"
const gib = int64(1024 * 1024 * 1024)

type Config struct {
	Listen               string         `yaml:"listen"`
	BaseURL              string         `yaml:"base_url"`
	Paths                Paths          `yaml:"paths"`
	Network              Network        `yaml:"network"`
	Limits               Limits         `yaml:"limits"`
	SpeedLimits          SpeedLimits    `yaml:"speed_limits"`
	ZipLimits            ZipLimits      `yaml:"zip_limits"`
	Sessions             Sessions       `yaml:"sessions"`
	InviteDefaults       InviteDefaults `yaml:"invite_defaults"`
	DiskReserve          DiskReserve    `yaml:"disk_reserve"`
	Secrets              Secrets        `yaml:"secrets"`
	SuspiciousExtensions []string       `yaml:"suspicious_extensions"`
	loadedFrom           string         `yaml:"-"`
}

type Paths struct {
	DataDir     string `yaml:"data_dir"`
	TmpDir      string `yaml:"tmp_dir"`
	DBPath      string `yaml:"db_path"`
	BackupDir   string `yaml:"backup_dir"`
	SecurityLog string `yaml:"security_log"`
}

type Network struct {
	LocalCIDRs []string `yaml:"local_cidrs"`
}

type Limits struct {
	DefaultStorageQuotaGB         int64 `yaml:"default_storage_quota_gb"`
	DefaultMonthlyUploadLimitGB   int64 `yaml:"default_monthly_upload_limit_gb"`
	DefaultMonthlyDownloadLimitGB int64 `yaml:"default_monthly_download_limit_gb"`
	DefaultMaxFileSizeGB          int64 `yaml:"default_max_file_size_gb"`
	MaxConcurrentUploads          int   `yaml:"max_concurrent_uploads"`
	DefaultExpiryDays             int   `yaml:"default_expiry_days"`
	AllowUserKeepForever          bool  `yaml:"allow_user_keep_forever"`
	QuarantineSuspicious          bool  `yaml:"quarantine_suspicious"`
}

func (l Limits) StorageQuotaBytes() int64         { return l.DefaultStorageQuotaGB * gib }
func (l Limits) MonthlyUploadLimitBytes() int64   { return l.DefaultMonthlyUploadLimitGB * gib }
func (l Limits) MonthlyDownloadLimitBytes() int64 { return l.DefaultMonthlyDownloadLimitGB * gib }
func (l Limits) MaxFileSizeBytes() int64          { return l.DefaultMaxFileSizeGB * gib }

type SpeedLimits struct {
	ExternalUploadMbps   int `yaml:"external_upload_limit_mbps"`
	ExternalDownloadMbps int `yaml:"external_download_limit_mbps"`
	BurstMB              int `yaml:"burst_mb"`
}

type ZipLimits struct {
	MaxFiles   int   `yaml:"max_files"`
	MaxTotalGB int64 `yaml:"max_total_gb"`
}

func (l ZipLimits) MaxTotalBytes() int64 { return l.MaxTotalGB * gib }

type Sessions struct {
	UserIdleDays      int `yaml:"user_idle_days"`
	UserAbsoluteDays  int `yaml:"user_absolute_days"`
	AdminIdleHours    int `yaml:"admin_idle_hours"`
	AdminAbsoluteDays int `yaml:"admin_absolute_days"`
}

type InviteDefaults struct {
	ExpiryDays     int `yaml:"expiry_days"`
	MaxActivations int `yaml:"max_activations"`
}

type DiskReserve struct {
	MinFreeSpaceGB      int64 `yaml:"min_free_space_gb"`
	CriticalFreeSpaceGB int64 `yaml:"critical_free_space_gb"`
	MinFreeInodes       int64 `yaml:"min_free_inodes"`
}

type Secrets struct {
	SessionSecret string `yaml:"session_secret"`
	IPSalt        string `yaml:"ip_salt"`
}

func DefaultConfig() *Config {
	return &Config{
		Listen:  "127.0.0.1:8090",
		BaseURL: "http://127.0.0.1:8090",
		Paths: Paths{
			DataDir:     "/srv/media/fileshare/data",
			TmpDir:      "/srv/media/fileshare/tmp",
			DBPath:      "/srv/media/fileshare/db/lares.db",
			BackupDir:   "/home/fileshare-backup",
			SecurityLog: "/var/log/lares/security.log",
		},
		Network: Network{LocalCIDRs: []string{"127.0.0.1/32", "::1/128", "192.168.32.0/24"}},
		Limits: Limits{
			DefaultStorageQuotaGB:         100,
			DefaultMonthlyUploadLimitGB:   200,
			DefaultMonthlyDownloadLimitGB: 300,
			DefaultMaxFileSizeGB:          50,
			MaxConcurrentUploads:          1,
			DefaultExpiryDays:             14,
			AllowUserKeepForever:          false,
			QuarantineSuspicious:          true,
		},
		SpeedLimits:    SpeedLimits{ExternalUploadMbps: 250, ExternalDownloadMbps: 250, BurstMB: 16},
		ZipLimits:      ZipLimits{MaxFiles: 100, MaxTotalGB: 50},
		Sessions:       Sessions{UserIdleDays: 30, UserAbsoluteDays: 90, AdminIdleHours: 12, AdminAbsoluteDays: 7},
		InviteDefaults: InviteDefaults{ExpiryDays: 30, MaxActivations: 1},
		DiskReserve:    DiskReserve{MinFreeSpaceGB: 40, CriticalFreeSpaceGB: 20, MinFreeInodes: 100000},
		SuspiciousExtensions: []string{
			"exe", "msi", "msp", "bat", "cmd", "com", "scr", "vbs", "vbe",
			"js", "jse", "ws", "wsf", "wsh", "ps1", "psm1", "sh", "bash",
			"dll", "ocx", "jar", "apk", "hta", "cpl",
		},
	}
}

// legacyConfig describes the pre-Lares flat schema. It is accepted only when
// the caller explicitly loads that file; LoadConfig never searches legacy paths.
type legacyConfig struct {
	DataDir     *string `yaml:"data_dir"`
	TmpDir      *string `yaml:"tmp_dir"`
	DBPath      *string `yaml:"db_path"`
	BackupDir   *string `yaml:"backup_dir"`
	SecurityLog *string `yaml:"security_log"`
	LocalCIDR   *string `yaml:"local_cidr"`
	Secrets     struct {
		IPHashSalt *string `yaml:"ip_hash_salt"`
	} `yaml:"secrets"`
	StorageDefaults *struct {
		QuotaBytes           *int64 `yaml:"quota_bytes"`
		MonthlyUploadLimit   *int64 `yaml:"monthly_upload_limit"`
		MonthlyDownloadLimit *int64 `yaml:"monthly_download_limit"`
		MaxFileSize          *int64 `yaml:"max_file_size"`
		MaxConcurrentUploads *int   `yaml:"max_concurrent_uploads"`
		DefaultExpiryDays    *int   `yaml:"default_expiry_days"`
		AllowUserKeepForever *bool  `yaml:"allow_user_keep_forever"`
		QuarantineSuspicious *bool  `yaml:"quarantine_suspicious"`
	} `yaml:"storage_defaults"`
	SessionDefaults *Sessions `yaml:"session_defaults"`
	SpeedLimits     *struct {
		ExternalUploadMbps   *int `yaml:"external_upload_mbps"`
		ExternalDownloadMbps *int `yaml:"external_download_mbps"`
	} `yaml:"speed_limits"`
}

func LoadConfig(path string) (*Config, error) {
	path = resolveConfigPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := applyLegacyConfig(data, cfg); err != nil {
		return nil, fmt.Errorf("parse legacy config %s: %w", path, err)
	}
	cfg.loadedFrom = path

	needSave := false
	if cfg.Secrets.SessionSecret == "" || cfg.Secrets.SessionSecret == "AUTO_GENERATED_SECRET_KEY_CHANGE_IN_PRODUCTION" {
		cfg.Secrets.SessionSecret, err = generateRandomSecret(32)
		needSave = true
	}
	if err == nil && (cfg.Secrets.IPSalt == "" || cfg.Secrets.IPSalt == "AUTO_GENERATED_IP_SALT_CHANGE_IN_PRODUCTION") {
		cfg.Secrets.IPSalt, err = generateRandomSecret(32)
		needSave = true
	}
	if err != nil {
		return nil, fmt.Errorf("generate config secrets: %w", err)
	}
	if needSave {
		if err := SaveConfig(cfg, path); err != nil {
			return nil, fmt.Errorf("persist generated config secrets: %w", err)
		}
	}
	return cfg, nil
}

func resolveConfigPath(path string) string {
	if path == "" {
		return DefaultConfigPath
	}
	return path
}

func (cfg *Config) LoadedFrom() string {
	if cfg.loadedFrom != "" {
		return cfg.loadedFrom
	}
	return DefaultConfigPath
}

func SaveConfig(cfg *Config, path string) error {
	if path == "" {
		path = DefaultConfigPath
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	mode := os.FileMode(0640)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.yaml-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func applyLegacyConfig(data []byte, cfg *Config) error {
	var sections map[string]yaml.Node
	if err := yaml.Unmarshal(data, &sections); err != nil {
		return err
	}
	var old legacyConfig
	if err := yaml.Unmarshal(data, &old); err != nil {
		return err
	}
	_, hasPaths := sections["paths"]
	_, hasNetwork := sections["network"]
	_, hasLimits := sections["limits"]
	_, hasSessions := sections["sessions"]
	if old.DataDir != nil && !hasPaths {
		cfg.Paths.DataDir = *old.DataDir
	}
	if old.TmpDir != nil && !hasPaths {
		cfg.Paths.TmpDir = *old.TmpDir
	}
	if old.DBPath != nil && !hasPaths {
		cfg.Paths.DBPath = *old.DBPath
	}
	if old.BackupDir != nil && !hasPaths {
		cfg.Paths.BackupDir = *old.BackupDir
	}
	if old.SecurityLog != nil && !hasPaths {
		cfg.Paths.SecurityLog = *old.SecurityLog
	}
	if old.LocalCIDR != nil && !hasNetwork {
		cfg.Network.LocalCIDRs = []string{*old.LocalCIDR}
	}
	if old.Secrets.IPHashSalt != nil && cfg.Secrets.IPSalt == "" {
		cfg.Secrets.IPSalt = *old.Secrets.IPHashSalt
	}
	if old.StorageDefaults != nil && !hasLimits {
		o := old.StorageDefaults
		if o.QuotaBytes != nil {
			cfg.Limits.DefaultStorageQuotaGB = *o.QuotaBytes / gib
		}
		if o.MonthlyUploadLimit != nil {
			cfg.Limits.DefaultMonthlyUploadLimitGB = *o.MonthlyUploadLimit / gib
		}
		if o.MonthlyDownloadLimit != nil {
			cfg.Limits.DefaultMonthlyDownloadLimitGB = *o.MonthlyDownloadLimit / gib
		}
		if o.MaxFileSize != nil {
			cfg.Limits.DefaultMaxFileSizeGB = *o.MaxFileSize / gib
		}
		if o.MaxConcurrentUploads != nil {
			cfg.Limits.MaxConcurrentUploads = *o.MaxConcurrentUploads
		}
		if o.DefaultExpiryDays != nil {
			cfg.Limits.DefaultExpiryDays = *o.DefaultExpiryDays
		}
		if o.AllowUserKeepForever != nil {
			cfg.Limits.AllowUserKeepForever = *o.AllowUserKeepForever
		}
		if o.QuarantineSuspicious != nil {
			cfg.Limits.QuarantineSuspicious = *o.QuarantineSuspicious
		}
	}
	if old.SessionDefaults != nil && !hasSessions {
		cfg.Sessions = *old.SessionDefaults
	}
	if old.SpeedLimits != nil {
		if old.SpeedLimits.ExternalUploadMbps != nil {
			cfg.SpeedLimits.ExternalUploadMbps = *old.SpeedLimits.ExternalUploadMbps
		}
		if old.SpeedLimits.ExternalDownloadMbps != nil {
			cfg.SpeedLimits.ExternalDownloadMbps = *old.SpeedLimits.ExternalDownloadMbps
		}
	}
	return nil
}

func generateRandomSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
