package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen      string         `yaml:"listen"`
	BaseURL     string         `yaml:"base_url"`
	Paths       PathsConfig    `yaml:"paths"`
	Network     NetworkConfig  `yaml:"network"`
	Secrets     SecretsConfig  `yaml:"secrets"`
	Limits      LimitsConfig   `yaml:"limits"`
	DiskReserve DiskReserve    `yaml:"disk_reserve"`
	SpeedLimits SpeedLimits    `yaml:"speed_limits"`
	ZipLimits   ZipLimits      `yaml:"zip_limits"`
	Sessions    SessionsConfig `yaml:"sessions"`
	Invite      InviteConfig   `yaml:"invite_defaults"`
	Suspicious  []string       `yaml:"suspicious_extensions"`
}

type PathsConfig struct {
	DataDir     string `yaml:"data_dir"`
	TmpDir      string `yaml:"tmp_dir"`
	DBPath      string `yaml:"db_path"`
	BackupDir   string `yaml:"backup_dir"`
	SecurityLog string `yaml:"security_log"`
}

type NetworkConfig struct {
	LocalCIDRs []string `yaml:"local_cidrs"`
	parsedCIDRs []*net.IPNet
}

type SecretsConfig struct {
	SessionSecret string `yaml:"session_secret"`
	IPSalt        string `yaml:"ip_salt"`
}

type LimitsConfig struct {
	DefaultStorageQuotaGB       int64 `yaml:"default_storage_quota_gb"`
	DefaultMonthlyUploadLimitGB int64 `yaml:"default_monthly_upload_limit_gb"`
	DefaultMonthlyDownloadLimitGB int64 `yaml:"default_monthly_download_limit_gb"`
	DefaultMaxFileSizeGB        int64 `yaml:"default_max_file_size_gb"`
	DefaultMaxConcurrentUploads int   `yaml:"default_max_concurrent_uploads"`
	DefaultExpiryDays           int   `yaml:"default_expiry_days"`
}

type DiskReserve struct {
	MinFreeSpaceGB      int64 `yaml:"min_free_space_gb"`
	CriticalFreeSpaceGB int64 `yaml:"critical_free_space_gb"`
	MinFreeInodes       uint64 `yaml:"min_free_inodes"`
}

type SpeedLimits struct {
	ExternalUploadLimitMbps   int `yaml:"external_upload_limit_mbps"`
	ExternalDownloadLimitMbps int `yaml:"external_download_limit_mbps"`
	BurstMB                   int `yaml:"burst_mb"`
}

type ZipLimits struct {
	MaxFiles   int   `yaml:"max_files"`
	MaxTotalGB int64 `yaml:"max_total_gb"`
}

type SessionsConfig struct {
	UserIdleDays     int `yaml:"user_idle_days"`
	UserAbsoluteDays int `yaml:"user_absolute_days"`
	AdminIdleHours   int `yaml:"admin_idle_hours"`
	AdminAbsoluteDays int `yaml:"admin_absolute_days"`
}

type InviteConfig struct {
	ExpiresHours   int `yaml:"expires_hours"`
	MaxActivations int `yaml:"max_activations"`
}

func DefaultConfig() *Config {
	return &Config{
		Listen:  "127.0.0.1:8090",
		BaseURL: "https://files.example.duckdns.org",
		Paths: PathsConfig{
			DataDir:     "/srv/media/fileshare/data",
			TmpDir:      "/srv/media/fileshare/tmp",
			DBPath:      "/srv/media/fileshare/db/lares.db",
			BackupDir:   "/home/fileshare-backup",
			SecurityLog: "/var/log/lares/security.log",
		},
		Network: NetworkConfig{
			LocalCIDRs: []string{"127.0.0.1/32", "::1/128", "192.168.32.0/24"},
		},
		Secrets: SecretsConfig{
			SessionSecret: "",
			IPSalt:        "",
		},
		Limits: LimitsConfig{
			DefaultStorageQuotaGB:       100,
			DefaultMonthlyUploadLimitGB: 200,
			DefaultMonthlyDownloadLimitGB: 300,
			DefaultMaxFileSizeGB:        50,
			DefaultMaxConcurrentUploads: 1,
			DefaultExpiryDays:           14,
		},
		DiskReserve: DiskReserve{
			MinFreeSpaceGB:      40,
			CriticalFreeSpaceGB: 20,
			MinFreeInodes:       100000,
		},
		SpeedLimits: SpeedLimits{
			ExternalUploadLimitMbps:   250,
			ExternalDownloadLimitMbps: 250,
			BurstMB:                   16,
		},
		ZipLimits: ZipLimits{
			MaxFiles:   100,
			MaxTotalGB: 50,
		},
		Sessions: SessionsConfig{
			UserIdleDays:     30,
			UserAbsoluteDays: 90,
			AdminIdleHours:   12,
			AdminAbsoluteDays: 7,
		},
		Invite: InviteConfig{
			ExpiresHours:   24,
			MaxActivations: 1,
		},
		Suspicious: []string{
			"exe", "msi", "msp", "bat", "cmd", "com", "scr", "vbs", "vbe", "js", "jse",
			"ws", "wsf", "wsh", "ps1", "psm1", "sh", "bash", "dll", "ocx", "jar", "apk", "hta", "cpl",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config YAML: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Parse CIDRs
	for _, cidrStr := range cfg.Network.LocalCIDRs {
		_, cidr, err := net.ParseCIDR(cidrStr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidrStr)
			if ip != nil {
				mask := net.CIDRMask(32, 32)
				if ip.To4() == nil {
					mask = net.CIDRMask(128, 128)
				}
				cidr = &net.IPNet{IP: ip, Mask: mask}
			} else {
				return nil, fmt.Errorf("invalid CIDR/IP: %s", cidrStr)
			}
		}
		cfg.Network.parsedCIDRs = append(cfg.Network.parsedCIDRs, cidr)
	}

	// Ensure secrets
	if cfg.Secrets.SessionSecret == "" {
		b := make([]byte, 32)
		rand.Read(b)
		cfg.Secrets.SessionSecret = hex.EncodeToString(b)
	}
	if cfg.Secrets.IPSalt == "" {
		b := make([]byte, 32)
		rand.Read(b)
		cfg.Secrets.IPSalt = hex.EncodeToString(b)
	}

	// Create directories if needed
	dirs := []string{
		cfg.Paths.DataDir,
		cfg.Paths.TmpDir,
		filepath.Dir(cfg.Paths.DBPath),
		cfg.Paths.BackupDir,
		filepath.Dir(cfg.Paths.SecurityLog),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			// Ignore error if permission denied, but return error if not
		}
	}

	return cfg, nil
}

func (c *Config) IsLocalIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range c.Network.parsedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
