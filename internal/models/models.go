package models

import (
	"time"
)

type AdminUser struct {
	ID           int64      `db:"id" json:"id"`
	Username     string     `db:"username" json:"username"`
	PasswordHash string     `db:"password_hash" json:"-"`
	TOTPSecret   string     `db:"totp_secret" json:"-"`
	TOTPEnabled  bool       `db:"totp_enabled" json:"totp_enabled"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	LastLoginAt  *time.Time `db:"last_login_at" json:"last_login_at"`
}

type Person struct {
	ID                       int64      `db:"id" json:"id"`
	Label                    string     `db:"label" json:"label"`
	Notes                    string     `db:"notes" json:"notes"`
	Enabled                  bool       `db:"enabled" json:"enabled"`
	StorageQuotaBytes        int64      `db:"storage_quota_bytes" json:"storage_quota_bytes"`
	MonthlyUploadLimitBytes  int64      `db:"monthly_upload_limit_bytes" json:"monthly_upload_limit_bytes"`
	MonthlyDownloadLimitBytes int64     `db:"monthly_download_limit_bytes" json:"monthly_download_limit_bytes"`
	MaxFileSizeBytes         int64      `db:"max_file_size_bytes" json:"max_file_size_bytes"`
	MaxConcurrentUploads     int        `db:"max_concurrent_uploads" json:"max_concurrent_uploads"`
	AllowUserKeepForever     bool       `db:"allow_user_keep_forever" json:"allow_user_keep_forever"`
	SessionIdleDays          int        `db:"session_idle_days" json:"session_idle_days"`
	SessionAbsoluteDays      int        `db:"session_absolute_days" json:"session_absolute_days"`
	IgnoreTrafficQuota       bool       `db:"ignore_traffic_quota" json:"ignore_traffic_quota"`
	CreatedAt                time.Time  `db:"created_at" json:"created_at"`
	LastActivityAt           *time.Time `db:"last_activity_at" json:"last_activity_at"`
}

type InviteCode struct {
	ID               int64      `db:"id" json:"id"`
	PersonID         int64      `db:"person_id" json:"person_id"`
	CodeHash         string     `db:"code_hash" json:"-"`
	CodePrefix       string     `db:"code_prefix" json:"code_prefix"`
	Enabled          bool       `db:"enabled" json:"enabled"`
	MaxActivations   int        `db:"max_activations" json:"max_activations"`
	ActivationsUsed  int        `db:"activations_used" json:"activations_used"`
	ExpiresAt        time.Time  `db:"expires_at" json:"expires_at"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	CreatedByAdminID int64      `db:"created_by_admin_id" json:"created_by_admin_id"`
}

type DeviceSession struct {
	ID                int64      `db:"id" json:"id"`
	PersonID          int64      `db:"person_id" json:"person_id"`
	Name              string     `db:"name" json:"name"`
	SessionTokenHash  string     `db:"session_token_hash" json:"-"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
	LastUsedAt        time.Time  `db:"last_used_at" json:"last_used_at"`
	LastIPHash        string     `db:"last_ip_hash" json:"last_ip_hash"`
	LastUserAgentHash string     `db:"last_user_agent_hash" json:"last_user_agent_hash"`
	IdleExpiresAt     time.Time  `db:"idle_expires_at" json:"idle_expires_at"`
	AbsoluteExpiresAt *time.Time `db:"absolute_expires_at" json:"absolute_expires_at"` // NULL if unlimited
	Revoked           bool       `db:"revoked" json:"revoked"`
}

type UploadStatus string

const (
	UploadStatusReserved  UploadStatus = "reserved"
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusCanceled UploadStatus = "canceled"
	UploadStatusExpired   UploadStatus = "expired"
	UploadStatusFailed    UploadStatus = "failed"
)

type Upload struct {
	ID                   string       `db:"id" json:"id"`
	PersonID             int64        `db:"person_id" json:"person_id"`
	SessionID            int64        `db:"session_id" json:"session_id"`
	UploadSecretHash     string       `db:"upload_secret_hash" json:"-"`
	OriginalName         string       `db:"original_name" json:"original_name"`
	DeclaredSize         int64        `db:"declared_size" json:"declared_size"`
	ReceivedBytes        int64        `db:"received_bytes" json:"received_bytes"`
	Status               UploadStatus `db:"status" json:"status"`
	ExpiryDays           int          `db:"expiry_days" json:"expiry_days"`
	ReservationExpiresAt time.Time    `db:"reservation_expires_at" json:"reservation_expires_at"`
	CreatedAt            time.Time    `db:"created_at" json:"created_at"`
	CompletedAt          *time.Time   `db:"completed_at" json:"completed_at"`
	ClientIPHash         string       `db:"client_ip_hash" json:"client_ip_hash"`
}

type FileStatus string

const (
	FileStatusReady       FileStatus = "ready"
	FileStatusQuarantined FileStatus = "quarantined"
)

type FileRecord struct {
	ID           string     `db:"id" json:"id"`
	PersonID     int64      `db:"person_id" json:"person_id"`
	OriginalName string     `db:"original_name" json:"original_name"`
	StoredPath   string     `db:"stored_path" json:"stored_path"`
	Size         int64      `db:"size" json:"size"`
	ContentType  string     `db:"content_type" json:"content_type"`
	Status       FileStatus `db:"status" json:"status"`
	Flagged      bool       `db:"flagged" json:"flagged"`
	FlagReason   string     `db:"flag_reason" json:"flag_reason"`
	Protected    bool       `db:"protected" json:"protected"`
	KeepForever  bool       `db:"keep_forever" json:"keep_forever"`
	ExpiresAt    *time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	ClientIPHash string     `db:"client_ip_hash" json:"client_ip_hash"`
	// Joined fields for UI
	UploaderLabel string `db:"-" json:"uploader_label,omitempty"`
}

type TrafficCounter struct {
	PersonID              int64     `db:"person_id" json:"person_id"`
	Month                 string    `db:"month" json:"month"` // e.g., "2026-08"
	UploadCompletedBytes  int64     `db:"upload_completed_bytes" json:"upload_completed_bytes"`
	UploadAbortedBytes    int64     `db:"upload_aborted_bytes" json:"upload_aborted_bytes"`
	DownloadCompletedBytes int64    `db:"download_completed_bytes" json:"download_completed_bytes"`
	DownloadAbortedBytes  int64     `db:"download_aborted_bytes" json:"download_aborted_bytes"`
	UpdatedAt             time.Time `db:"updated_at" json:"updated_at"`
}

type AuditLog struct {
	ID         int64     `db:"id" json:"id"`
	Time       time.Time `db:"time" json:"time"`
	ActorType  string    `db:"actor_type" json:"actor_type"` // admin | person | system
	ActorID    int64     `db:"actor_id" json:"actor_id"`
	ActorLabel string    `db:"-" json:"actor_label,omitempty"`
	Event      string    `db:"event" json:"event"`
	EntityType string    `db:"entity_type" json:"entity_type"`
	EntityID   string    `db:"entity_id" json:"entity_id"`
	IPHash     string    `db:"ip_hash" json:"ip_hash"`
	Details    string    `db:"details" json:"details"`
}

type RateLimitLock struct {
	Key       string    `db:"key" json:"key"`
	Type      string    `db:"type" json:"type"`
	Reason    string    `db:"reason" json:"reason"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type SettingRecord struct {
	Key   string `db:"key" json:"key"`
	Value string `db:"value" json:"value"`
}
