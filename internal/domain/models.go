package domain

import "time"

// APIKey represents a management API key.
type APIKey struct {
	ID        int64     `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// Tenant is the externally visible tenant model.
type Tenant struct {
	ID        int64     `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Username  string    `json:"username"`
	PublicKey string    `json:"public_key,omitempty"`
	HomeDir   string    `json:"home_dir"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantRecord stores the internal tenant representation.
type TenantRecord struct {
	Tenant
	PasswordHash string
}

// Record represents a CSV-derived record.
type Record struct {
	ID          int64     `json:"id"`
	TenantID    string    `json:"tenant_id"`
	RecordKey   string    `json:"record_key"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Value       float64   `json:"value"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateTenantInput describes the tenant creation request.
type CreateTenantInput struct {
	Username  string
	Password  string
	PublicKey string
}

// CreatedTenant is returned once after tenant creation.
type CreatedTenant struct {
	Tenant   Tenant `json:"tenant"`
	Password string `json:"password"`
	TenantID string `json:"tenant_id"`
}

// ValidateTenantResult reports whether the remote tenant is active.
type ValidateTenantResult struct {
	Valid    bool   `json:"valid"`
	Username string `json:"username"`
	Reason   string `json:"reason,omitempty"`
}

// UploadEvent is the normalized upload-hook payload.
type UploadEvent struct {
	Action      string `json:"action"`
	Username    string `json:"username"`
	VirtualPath string `json:"virtual_path"`
}

// ExternalAuthRequest is the normalized auth-hook payload.
type ExternalAuthRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	PublicKey string `json:"public_key"`
	Protocol  string `json:"protocol"`
	IP        string `json:"ip"`
}

// S3FilesystemConfig describes a tenant's S3-backed filesystem.
type S3FilesystemConfig struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	KeyPrefix string
}

// SFTPGoUser is returned to SFTPGo after successful external auth.
type SFTPGoUser struct {
	Status      int                 `json:"status"`
	Username    string              `json:"username"`
	HomeDir     string              `json:"home_dir"`
	Permissions map[string][]string `json:"permissions"`
	Filesystem  any                 `json:"filesystem,omitempty"`
}
