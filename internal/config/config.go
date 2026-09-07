package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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

	ExpiryOptions        []int      `yaml:"expiry_options"`
	RateLimits           RateLimits `yaml:"rate_limits"`
	SuspiciousExtensions []string   `yaml:"suspicious_extensions"`
}

type RateLimits struct {
	EnforceLocal         bool `yaml:"enforce_local"`
	UploadPersonHour     int  `yaml:"upload_person_hour"`
	UploadIPHour         int  `yaml:"upload_ip_hour"`
	ChunkMinute          int  `yaml:"chunk_minute"`
	DownloadPersonMinute int  `yaml:"download_person_minute"`
	DownloadIPMinute     int  `yaml:"download_ip_minute"`
	ZipHour              int  `yaml:"zip_hour"`
	ZipDay               int  `yaml:"zip_day"`
	ListMinute           int  `yaml:"list_minute"`
	AdminMinute          int  `yaml:"admin_minute"`
}

type Secrets struct {
	SessionSecret string `yaml:"session_secret"`
	IPHashSalt    string `yaml:"ip_hash_salt"`
}

type StorageDefaults struct {
	QuotaBytes           int64 `yaml:"quota_bytes"`
	MonthlyUploadLimit   int64 `yaml:"monthly_upload_limit"`
	MonthlyDownloadLimit int64 `yaml:"monthly_download_limit"`
	MaxFileSize          int64 `yaml:"max_file_size"`
	MaxConcurrentUploads int   `yaml:"max_concurrent_uploads"`
	DefaultExpiryDays    int   `yaml:"default_expiry_days"`
	AllowUserKeepForever bool  `yaml:"allow_user_keep_forever"`
	QuarantineSuspicious bool  `yaml:"quarantine_suspicious"`
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
	UserIdleDays      int `yaml:"user_idle_days"`
	UserAbsoluteDays  int `yaml:"user_absolute_days"`
	AdminIdleHours    int `yaml:"admin_idle_hours"`
	AdminAbsoluteDays int `yaml:"admin_absolute_days"`
}

type DiskReserve struct {
	MinFreeSpaceGB      int64 `yaml:"min_free_space_gb"`
	CriticalFreeSpaceGB int64 `yaml:"critical_free_space_gb"`
	MinFreeInodes       int64 `yaml:"min_free_inodes"`
}

func DefaultConfig() *Config {
	return &Config{
		Listen:        "127.0.0.1:8090",
		BaseURL:       "http://127.0.0.1:8090",
		DataDir:       "/srv/media/fileshare/data",
		TmpDir:        "/srv/media/fileshare/tmp",
		DBPath:        "/srv/media/fileshare/db/lares.db",
		BackupDir:     "/home/fileshare-backup",
		LogDir:        "/var/log/homeshare",
		SecurityLog:   "/var/log/homeshare/security.log",
		LocalCIDR:     "192.168.32.0/24",
		Secrets:       Secrets{},
		ExpiryOptions: []int{1, 7, 14, 30},
		RateLimits:    RateLimits{UploadPersonHour: 10, UploadIPHour: 20, ChunkMinute: 120, DownloadPersonMinute: 120, DownloadIPMinute: 300, ZipHour: 5, ZipDay: 20, ListMinute: 120, AdminMinute: 120},
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

const DefaultPath = "/etc/homeshare/config.yaml"

func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv("LARES_CONFIG")
		if path == "" {
			path = DefaultPath
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("конфигурация %s: %w", path, err)
	}
	cfg := DefaultConfig()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err = dec.Decode(cfg); err != nil {
		return nil, err
	}
	if v := os.Getenv("LARES_SESSION_SECRET"); v != "" {
		cfg.Secrets.SessionSecret = v
	}
	if v := os.Getenv("LARES_IP_SALT"); v != "" {
		cfg.Secrets.IPHashSalt = v
	}
	if err = cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
func (c *Config) ValidateRuntime() error {
	d := c.StorageDefaults
	const maxBytes = int64(1) << 60
	for _, n := range []int64{d.QuotaBytes, d.MonthlyUploadLimit, d.MonthlyDownloadLimit, d.MaxFileSize} {
		if n < 0 || n > maxBytes {
			return fmt.Errorf("квоты должны быть от 0 до 2^60 байт")
		}
	}
	if d.MaxConcurrentUploads < 1 || d.MaxConcurrentUploads > 100 {
		return fmt.Errorf("число загрузок: 1..100")
	}
	if c.SpeedLimits.ExternalUploadMbps < 10 || c.SpeedLimits.ExternalUploadMbps > 1000 || c.SpeedLimits.ExternalDownloadMbps < 10 || c.SpeedLimits.ExternalDownloadMbps > 1000 || c.SpeedLimits.BurstMB < 1 || c.SpeedLimits.BurstMB > 128 {
		return fmt.Errorf("скорость: 10..1000 Мбит/с; burst: 1..128 МиБ")
	}
	if c.ZipLimits.MaxFiles < 1 || c.ZipLimits.MaxFiles > 10000 || c.ZipLimits.MaxTotalGB < 1 || c.ZipLimits.MaxTotalGB > 1048576 {
		return fmt.Errorf("недопустимые пределы ZIP")
	}
	found := false
	seen := map[int]bool{}
	for _, d := range c.ExpiryOptions {
		if d < 1 || d > 3650 || seen[d] {
			return fmt.Errorf("сроки хранения: уникальные 1..3650 дней")
		}
		seen[d] = true
		if d == c.StorageDefaults.DefaultExpiryDays {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("срок по умолчанию должен входить в варианты хранения")
	}
	for _, ext := range c.SuspiciousExtensions {
		if ext == "" || strings.ContainsAny(ext, ". /\\\r\n\t") {
			return fmt.Errorf("недопустимое расширение")
		}
	}
	return nil
}
func (c *Config) Validate() error {
	if err := c.ValidateRuntime(); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || host != "127.0.0.1" || port != "8090" {
		return fmt.Errorf("listen должен быть 127.0.0.1:8090")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("недопустимый base_url")
	}
	if _, _, err = net.ParseCIDR(c.LocalCIDR); err != nil {
		return err
	}
	for _, p := range []string{c.DataDir, c.TmpDir, c.DBPath} {
		r, e := filepath.Rel("/srv/media/fileshare", p)
		if e != nil || r == ".." || strings.HasPrefix(r, "../") {
			return fmt.Errorf("путь должен находиться внутри /srv/media/fileshare")
		}
	}
	for _, p := range []string{c.TmpDir, c.DBPath, c.BackupDir, c.SecurityLog} {
		rel, e := filepath.Rel(c.DataDir, p)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return fmt.Errorf("data_dir не должен содержать tmp, БД, backup или журналы")
		}
	}
	for _, p := range []string{c.DataDir, c.TmpDir, c.DBPath, c.BackupDir, c.SecurityLog} {
		for current := filepath.Clean(p); current != "/" && current != "."; current = filepath.Dir(current) {
			info, e := os.Lstat(current)
			if e == nil && info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink в служебном пути запрещён")
			}
			if e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	}
	for _, p := range []string{c.BackupDir, c.SecurityLog} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("требуются абсолютные пути")
		}
	}
	if len(c.Secrets.SessionSecret) < 32 || len(c.Secrets.IPHashSalt) < 32 {
		return fmt.Errorf("задайте session_secret и ip_hash_salt не короче 32 символов; homeshare config init")
	}
	d := c.SessionDefaults
	if d.UserIdleDays < 1 || d.UserIdleDays > 3650 || d.UserAbsoluteDays < 1 || d.UserAbsoluteDays > 3650 || d.AdminIdleHours < 1 || d.AdminIdleHours > 12 || d.AdminAbsoluteDays < 1 || d.AdminAbsoluteDays > 7 {
		return fmt.Errorf("недопустимые сроки сессий; админ не более 12ч/7д")
	}
	if c.DiskReserve.CriticalFreeSpaceGB < 0 || c.DiskReserve.MinFreeSpaceGB < c.DiskReserve.CriticalFreeSpaceGB || c.DiskReserve.MinFreeInodes < 0 || c.DiskReserve.MinFreeSpaceGB > 1048576 {
		return fmt.Errorf("недопустимые резервы диска")
	}
	r := c.RateLimits
	for _, n := range []int{r.UploadPersonHour, r.UploadIPHour, r.ChunkMinute, r.DownloadPersonMinute, r.DownloadIPMinute, r.ZipHour, r.ZipDay, r.ListMinute, r.AdminMinute} {
		if n < 1 || n > 1000000 {
			return fmt.Errorf("недопустимый rate limit")
		}
	}
	return nil
}
func SaveConfig(c *Config, path string) error {
	if path == "" {
		path = DefaultPath
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lares-config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, path)
}
func Initialize(path string) error {
	c := DefaultConfig()
	c.Secrets.SessionSecret = generateRandomSecret(32)
	c.Secrets.IPHashSalt = generateRandomSecret(32)
	data, e := yaml.Marshal(c)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("создание конфигурации (существующий файл не перезаписывается): %w", e)
	}
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(path)
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func generateRandomSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
