package domain

import (
	"context"
	"io"
)

// APIKeyRepository manages API key persistence.
type APIKeyRepository interface {
	HasAPIKeys(ctx context.Context) (bool, error)
	CreateAPIKey(ctx context.Context, label, keyHash string) (*APIKey, error)
	HasAPIKeyHash(ctx context.Context, keyHash string) (bool, error)
}

// TenantRepository manages tenant persistence.
type TenantRepository interface {
	CreateTenant(ctx context.Context, tenant TenantRecord) (*Tenant, error)
	GetTenant(ctx context.Context, id int64) (*TenantRecord, error)
	GetTenantByUsername(ctx context.Context, username string) (*TenantRecord, error)
	ListTenants(ctx context.Context) ([]Tenant, error)
	DeleteTenant(ctx context.Context, id int64) error
	UpdateTenantPublicKey(ctx context.Context, id int64, publicKey string) error
	UpsertRecord(ctx context.Context, tenantID, recordKey, title, description, category string, value float64) error
	ListRecords(ctx context.Context, tenantID string) ([]Record, error)
	Close() error
}

// SFTPGoAdmin provisions tenant accounts in SFTPGo.
type SFTPGoAdmin interface {
	CreateUser(ctx context.Context, username, password, homeDir string, publicKeys []string, fs *S3FilesystemConfig) error
	GetUser(ctx context.Context, username string) (map[string]any, error)
	UpdateUserPublicKeys(ctx context.Context, username string, keys []string) error
	DeleteUser(ctx context.Context, username string) error
}

// ObjectStore retrieves uploaded objects for processing.
type ObjectStore interface {
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}
