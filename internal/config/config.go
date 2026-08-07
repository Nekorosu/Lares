package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"

	"gopkg.in/yaml.v3"
)


type Config struct {
	Listen      string `yaml:"listen"`
	BaseURL     string `yaml:"base_url"`
	DataDir     string `yaml:"data_dir"`
	TmpDir      string `yaml:"tmp_dir"`
	DBPath      string `yaml:"db_path"`
	BackupDir   string `yaml:"backup_dir"`
	LogDir      string `yaml:"log_dir"`
	SecurityLog string `yaml:"security_log"`
	LocalCIDR   string `yaml:"local_cidr"`

	Secrets Secrets `yaml:"secrets"`

	StorageDefaults StorageDefaults `yaml:"storage_defaults"`
	SpeedLimits     SpeedLimits     `yaml:"speed_limits"`
	ZipLimits       ZipLimits       `yaml:"zip_limits"`
	SessionDefaults SessionDefaults `yaml:"session_defaults"`
	DiskReserve     DiskReserve     `yaml:"disk_reserve"`

	SuspiciousExtensions []string `yaml:"suspicious_extensions"`
}

type Secrets struct {
	SessionSecret string `yaml:"session_secret"`
	IPHashSalt    string `yaml:"ip_hash_salt"`
}

type StorageDefaults struct {
	QuotaBytes            int64 `yaml:"quota_bytes"`
	MonthlyUploadLimit    int64 `yaml:"monthly_upload_limit"`
	MonthlyDownloadLimit  int64 `yaml:"monthly_download_limit"`
	MaxFileSize           int64 `yaml:"max_file_size"`
	MaxConcurrentUploads  int   `yaml:"max_concurrent_uploads"`
	DefaultExpiryDays     int   `yaml:"default_expiry_days"`
	AllowUserKeepForever  bool  `yaml:"allow_user_keep_forever"`
	QuarantineSuspicious  bool  `yaml:"quarantine_suspicious"`
}

type SpeedLimits struct {
	ExternalUploadMbps   int `yaml:"external_upload_mbps"`
	ExternalDownloadMbps int `yaml:"external_download_mbps"`
	BurstMB              int `yaml:"burst_mb"`
}

type ZipLimits struct {
	MaxFiles   int   `yaml:"max_files"`
	MaxTotalGB int64 `yaml:"max_total_gb"`
}

type SessionDefaults struct {
	UserIdleDays     int `yaml:"user_idle_days"`
	UserAbsoluteDays int `yaml:"user_absolute_days"`
	AdminIdleHours   int `yaml:"admin_idle_hours"`
	AdminAbsoluteDays int `yaml:"admin_absolute_days"`
}

type DiskReserve struct {
	MinFreeSpaceGB      int64 `yaml:"min_free_space_gb"`
	CriticalFreeSpaceGB int64 `yaml:"critical_free_space_gb"`
	MinFreeInodes       int64 `yaml:"min_free_inodes"`
}

func DefaultConfig() *Config {
	return &Config{
		Listen:      "127.0.0.1:8090",
		BaseURL:     "http://127.0.0.1:8090",
		DataDir:     "/srv/media/fileshare/data",
		TmpDir:      "/srv/media/fileshare/tmp",
		DBPath:      "/srv/media/fileshare/db/homeshare.db",
		BackupDir:   "/home/fileshare-backup",
		LogDir:      "/var/log/homeshare",
		SecurityLog: "/var/log/homeshare/security.log",
		LocalCIDR:   "192.168.32.0/24",
		Secrets: Secrets{
			SessionSecret: generateRandomSecret(32),
			IPHashSalt:    generateRandomSecret(32),
		},
		StorageDefaults: StorageDefaults{
			QuotaBytes:           int64(100) * 1024 * 1024 * 1024, // 100 GB
			MonthlyUploadLimit:   int64(200) * 1024 * 1024 * 1024, // 200 GB
			MonthlyDownloadLimit: int64(300) * 1024 * 1024 * 1024, // 300 GB
			MaxFileSize:          int64(50) * 1024 * 1024 * 1024,  // 50 GB
			MaxConcurrentUploads: 1,
			DefaultExpiryDays:    14,
			AllowUserKeepForever: false,
			QuarantineSuspicious: true,
		},
		SpeedLimits: SpeedLimits{
			ExternalUploadMbps:   250,
			ExternalDownloadMbps: 250,
			BurstMB:              16,
		},
		ZipLimits: ZipLimits{
			MaxFiles:   100,
			MaxTotalGB: 50,
		},
		SessionDefaults: SessionDefaults{
			UserIdleDays:      30,
			UserAbsoluteDays:  90,
			AdminIdleHours:    12,
			AdminAbsoluteDays: 7,
		},
		DiskReserve: DiskReserve{
			MinFreeSpaceGB:      40,
			CriticalFreeSpaceGB: 20,
			MinFreeInodes:       100000,
		},
		SuspiciousExtensions: []string{
			"exe", "msi", "msp", "bat", "cmd", "com", "scr", "vbs", "vbe",
			"js", "jse", "ws", "wsf", "wsh", "ps1", "psm1", "sh", "bash",
			"dll", "ocx", "jar", "apk", "hta", "cpl",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		// try standard locations
		locations := []string{"/etc/homeshare/config.yaml", "config.yaml"}
		for _, loc := range locations {
			if _, err := os.Stat(loc); err == nil {
				path = loc
				break
			}
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	needSave := false
	if cfg.Secrets.SessionSecret == "" || cfg.Secrets.SessionSecret == "AUTO_GENERATED_SECRET_KEY_CHANGE_IN_PRODUCTION" {
		cfg.Secrets.SessionSecret = generateRandomSecret(32)
		needSave = true
	}
	if cfg.Secrets.IPHashSalt == "" || cfg.Secrets.IPHashSalt == "AUTO_GENERATED_IP_SALT_CHANGE_IN_PRODUCTION" {
		cfg.Secrets.IPHashSalt = generateRandomSecret(32)
		needSave = true
	}

	if needSave && path != "" {
		_ = SaveConfig(cfg, path)
	}

	return cfg, nil
}

func SaveConfig(cfg *Config, path string) error {
	if path == "" {
		path = "config.yaml"
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func generateRandomSecret(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
