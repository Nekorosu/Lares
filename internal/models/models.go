package models

import (
	"time"
)

type AdminUser struct {
	ID           int64     `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	TOTPSecret   string    `json:"-" db:"totp_secret"`
	TOTPEnabled  bool      `json:"totp_enabled" db:"totp_enabled"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at" db:"last_login_at"`
}

type Person struct {
	ID                      int64      `json:"id" db:"id"`
	Label                   string     `json:"label" db:"label"`
	Notes                   string     `json:"notes" db:"notes"`
	Enabled                 bool       `json:"enabled" db:"enabled"`
	StorageQuotaBytes       int64      `json:"storage_quota_bytes" db:"storage_quota_bytes"`
	MonthlyUploadLimitBytes int64      `json:"monthly_upload_limit_bytes" db:"monthly_upload_limit_bytes"`
	MonthlyDownloadLimit    int64      `json:"monthly_download_limit_bytes" db:"monthly_download_limit_bytes"`
	MaxFileSizeBytes        int64      `json:"max_file_size_bytes" db:"max_file_size_bytes"`
	MaxConcurrentUploads    int        `json:"max_concurrent_uploads" db:"max_concurrent_uploads"`
	AllowUserKeepForever    bool       `json:"allow_user_keep_forever" db:"allow_user_keep_forever"`
	SessionIdleDays         int        `json:"session_idle_days" db:"session_idle_days"`
	SessionAbsoluteDays     int        `json:"session_absolute_days" db:"session_absolute_days"`
	IgnoreTrafficQuota      bool       `json:"ignore_traffic_quota" db:"ignore_traffic_quota"`
	CreatedAt               time.Time  `json:"created_at" db:"created_at"`
	LastActivityAt          *time.Time `json:"last_activity_at" db:"last_activity_at"`
}

type InviteCode struct {
	ID               int64      `json:"id" db:"id"`
	PersonID         int64      `json:"person_id" db:"person_id"`
	CodeHash         string     `json:"-" db:"code_hash"`
	CodePrefix       string     `json:"code_prefix" db:"code_prefix"`
	Enabled          bool       `json:"enabled" db:"enabled"`
	MaxActivations   int        `json:"max_activations" db:"max_activations"`
	ActivationsUsed  int        `json:"activations_used" db:"activations_used"`
	ExpiresAt        time.Time  `json:"expires_at" db:"expires_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	CreatedByAdminID int64      `json:"created_by_admin_id" db:"created_by_admin_id"`
}

type DeviceSession struct {
	ID                int64      `json:"id" db:"id"`
	PersonID          *int64     `json:"person_id,omitempty" db:"person_id"`
	AdminID           *int64     `json:"admin_id,omitempty" db:"admin_id"`
	IsAdmin           bool       `json:"is_admin" db:"is_admin"`

	Name              string     `json:"name" db:"name"`
	SessionTokenHash  string     `json:"-" db:"session_token_hash"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	LastUsedAt        time.Time  `json:"last_used_at" db:"last_used_at"`
	LastIPHash        string     `json:"last_ip_hash" db:"last_ip_hash"`
	LastUserAgentHash string     `json:"last_user_agent_hash" db:"last_user_agent_hash"`
	IdleExpiresAt     time.Time  `json:"idle_expires_at" db:"idle_expires_at"`
	AbsoluteExpiresAt *time.Time `json:"absolute_expires_at,omitempty" db:"absolute_expires_at"`
	Revoked           bool       `json:"revoked" db:"revoked"`
}

type UploadStatus string

const (
	UploadStatusReserved  UploadStatus = "reserved"
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusCanceled  UploadStatus = "canceled"
	UploadStatusExpired   UploadStatus = "expired"
	UploadStatusFailed    UploadStatus = "failed"
)

type Upload struct {
	ID                   string       `json:"id" db:"id"`
	PersonID             int64        `json:"person_id" db:"person_id"`
	SessionID            int64        `json:"session_id" db:"session_id"`
	UploadSecretHash     string       `json:"-" db:"upload_secret_hash"`
	OriginalName         string       `json:"original_name" db:"original_name"`
	DeclaredSize         int64        `json:"declared_size" db:"declared_size"`
	ReceivedBytes        int64        `json:"received_bytes" db:"received_bytes"`
	Status               UploadStatus `json:"status" db:"status"`
	ExpiryDays           int          `json:"expiry_days" db:"expiry_days"`
	ReservationExpiresAt time.Time    `json:"reservation_expires_at" db:"reservation_expires_at"`
	CreatedAt            time.Time    `json:"created_at" db:"created_at"`
	CompletedAt          *time.Time   `json:"completed_at,omitempty" db:"completed_at"`
	ClientIPHash         string       `json:"client_ip_hash" db:"client_ip_hash"`
}

type FileStatus string

const (
	FileStatusReady       FileStatus = "ready"
	FileStatusQuarantined FileStatus = "quarantined"
)

type FileRecord struct {
	ID            string     `json:"id" db:"id"`
	PersonID      int64      `json:"person_id" db:"person_id"`
	UploaderName  string     `json:"uploader_name" db:"uploader_name"`
	OriginalName  string     `json:"original_name" db:"original_name"`
	StoredPath    string     `json:"stored_path" db:"stored_path"`
	Size          int64      `json:"size" db:"size"`
	ContentType   string     `json:"content_type" db:"content_type"`
	Status        FileStatus `json:"status" db:"status"`
	Flagged       bool       `json:"flagged" db:"flagged"`
	FlagReason    string     `json:"flag_reason" db:"flag_reason"`
	Protected     bool       `json:"protected" db:"protected"`
	KeepForever   bool       `json:"keep_forever" db:"keep_forever"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	ClientIPHash  string     `json:"client_ip_hash" db:"client_ip_hash"`
}

type TrafficCounter struct {
	PersonID              int64     `json:"person_id" db:"person_id"`
	Month                 string    `json:"month" db:"month"` // YYYY-MM
	UploadCompletedBytes   int64     `json:"upload_completed_bytes" db:"upload_completed_bytes"`
	UploadAbortedBytes     int64     `json:"upload_aborted_bytes" db:"upload_aborted_bytes"`
	DownloadCompletedBytes int64     `json:"download_completed_bytes" db:"download_completed_bytes"`
	DownloadAbortedBytes   int64     `json:"download_aborted_bytes" db:"download_aborted_bytes"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
}

type AuditLog struct {
	ID         int64     `json:"id" db:"id"`
	Time       time.Time `json:"time" db:"time"`
	ActorType  string    `json:"actor_type" db:"actor_type"` // admin | person | system
	ActorID    int64     `json:"actor_id" db:"actor_id"`
	Event      string    `json:"event" db:"event"`
	EntityType string    `json:"entity_type" db:"entity_type"`
	EntityID   string    `json:"entity_id" db:"entity_id"`
	IPHash     string    `json:"ip_hash" db:"ip_hash"`
	Details    string    `json:"details" db:"details"`
}

type RateLimitLock struct {
	Key       string    `json:"key" db:"key"`
	Type      string    `json:"type" db:"type"`
	Reason    string    `json:"reason" db:"reason"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Setting struct {
	Key       string    `json:"key" db:"key"`
	Value     string    `json:"value" db:"value"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
