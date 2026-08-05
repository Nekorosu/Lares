package settings

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"lares/internal/config"
)

type Manager struct {
	db  *sql.DB
	cfg *config.Config
}

func NewManager(db *sql.DB, cfg *config.Config) *Manager {
	return &Manager{db: db, cfg: cfg}
}

func (m *Manager) Get(key string, fallback string) string {
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil || val == "" {
		return fallback
	}
	return val
}

func (m *Manager) GetInt64(key string, fallback int64) int64 {
	valStr := m.Get(key, "")
	if valStr == "" {
		return fallback
	}
	v, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func (m *Manager) GetInt(key string, fallback int) int {
	return int(m.GetInt64(key, int64(fallback)))
}

func (m *Manager) GetBool(key string, fallback bool) bool {
	valStr := m.Get(key, "")
	if valStr == "" {
		return fallback
	}
	return valStr == "true" || valStr == "1"
}

func (m *Manager) Set(key string, value string) error {
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func (m *Manager) GetSuspiciousList() []string {
	str := m.Get("suspicious_extensions", "")
	if str == "" {
		return m.cfg.Suspicious
	}
	parts := strings.Split(str, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.ToLower(strings.TrimSpace(p))
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (m *Manager) SetSuspiciousList(exts []string) error {
	return m.Set("suspicious_extensions", strings.Join(exts, ","))
}

func (m *Manager) ValidateAndSave(keyValues map[string]string) error {
	for k, v := range keyValues {
		v = strings.TrimSpace(v)
		switch k {
		case "external_upload_limit_mbps", "external_download_limit_mbps":
			val, err := strconv.Atoi(v)
			if err != nil || val < 10 || val > 1000 {
				return fmt.Errorf("%s должно быть от 10 до 1000 Mbps", k)
			}
		case "burst_mb":
			val, err := strconv.Atoi(v)
			if err != nil || val < 1 || val > 128 {
				return fmt.Errorf("burst_mb должно быть от 1 до 128 MB")
			}
		case "zip_max_files":
			val, err := strconv.Atoi(v)
			if err != nil || val <= 0 {
				return fmt.Errorf("zip_max_files должно быть больше 0")
			}
		case "zip_max_total_gb":
			val, err := strconv.ParseInt(v, 10, 64)
			if err != nil || val <= 0 {
				return fmt.Errorf("zip_max_total_gb должно быть больше 0")
			}
		}
	}

	for k, v := range keyValues {
		if err := m.Set(k, strings.TrimSpace(v)); err != nil {
			return err
		}
	}
	return nil
}
