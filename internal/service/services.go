package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"sftpgo-manager/internal/config"
	"sftpgo-manager/internal/domain"
)

var (
	// ErrBootstrapDisabled means bootstrap is not configured.
	ErrBootstrapDisabled = errors.New("bootstrap endpoint is disabled")
	// ErrBootstrapForbidden means the provided bootstrap token is invalid.
	ErrBootstrapForbidden = errors.New("invalid bootstrap token")
	// ErrBootstrapAlreadyCompleted means an API key already exists.
	ErrBootstrapAlreadyCompleted = errors.New("bootstrap already completed")
	// ErrUnauthorized means bearer auth failed.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrTenantNotFound means the tenant does not exist.
	ErrTenantNotFound = errors.New("tenant not found")
)

// BootstrapService manages one-time bootstrap.
type BootstrapService struct {
	cfg  config.Config
	keys domain.APIKeyRepository
}

// NewBootstrapService constructs a BootstrapService.
func NewBootstrapService(cfg config.Config, keys domain.APIKeyRepository) *BootstrapService {
	return &BootstrapService{cfg: cfg, keys: keys}
}

// BootstrapAPIKey mints the initial management API key once.
func (s *BootstrapService) BootstrapAPIKey(ctx context.Context, label, token string) (string, *domain.APIKey, error) {
	if s.cfg.BootstrapToken == "" {
		return "", nil, ErrBootstrapDisabled
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.BootstrapToken)) != 1 {
		return "", nil, ErrBootstrapForbidden
	}
	hasKeys, err := s.keys.HasAPIKeys(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("check bootstrap state: %w", err)
	}
	if hasKeys {
		return "", nil, ErrBootstrapAlreadyCompleted
	}

	plain, hash, err := newAPIKey()
	if err != nil {
		return "", nil, err
	}
	key, err := s.keys.CreateAPIKey(ctx, label, hash)
	if err != nil {
		return "", nil, fmt.Errorf("create api key: %w", err)
	}
	return plain, key, nil
}

// AuthService validates API keys.
type AuthService struct {
	keys domain.APIKeyRepository
}

// NewAuthService constructs an AuthService.
func NewAuthService(keys domain.APIKeyRepository) *AuthService {
	return &AuthService{keys: keys}
}

// ValidateAPIKey reports whether the presented API key is valid.
func (s *AuthService) ValidateAPIKey(ctx context.Context, key string) error {
	if key == "" {
		return ErrUnauthorized
	}
	ok, err := s.keys.HasAPIKeyHash(ctx, hashAPIKey(key))
	if err != nil {
		return fmt.Errorf("validate api key: %w", err)
	}
	if !ok {
		return ErrUnauthorized
	}
	return nil
}

// TenantService manages tenant lifecycle and validation.
type TenantService struct {
	cfg    config.Config
	repo   domain.TenantRepository
	sftpgo domain.SFTPGoAdmin
}

// NewTenantService constructs a TenantService.
func NewTenantService(cfg config.Config, repo domain.TenantRepository, sftpgo domain.SFTPGoAdmin) *TenantService {
	return &TenantService{cfg: cfg, repo: repo, sftpgo: sftpgo}
}

// CreateTenant provisions a tenant across the DB and SFTPGo.
func (s *TenantService) CreateTenant(ctx context.Context, input domain.CreateTenantInput) (*domain.CreatedTenant, error) {
	if strings.TrimSpace(input.Username) == "" {
		return nil, fmt.Errorf("username is required")
	}
	if _, err := s.repo.GetTenantByUsername(ctx, input.Username); err == nil {
		return nil, fmt.Errorf("tenant username already exists")
	}

	password := input.Password
	if password == "" {
		var err error
		password, err = newPassword()
		if err != nil {
			return nil, err
		}
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	tenantID, err := newTenantID()
	if err != nil {
		return nil, err
	}
	homeDir := filepath.Join(s.cfg.DataDir, tenantID)

	var publicKeys []string
	if input.PublicKey != "" {
		publicKeys = []string{input.PublicKey}
	}

	if err := s.sftpgo.CreateUser(ctx, input.Username, password, homeDir, publicKeys, s.s3Filesystem(tenantID)); err != nil {
		return nil, fmt.Errorf("create tenant in sftpgo: %w", err)
	}

	tenant, err := s.repo.CreateTenant(ctx, domain.TenantRecord{
		Tenant: domain.Tenant{
			TenantID:  tenantID,
			Username:  input.Username,
			PublicKey: input.PublicKey,
			HomeDir:   homeDir,
		},
		PasswordHash: string(passwordHash),
	})
	if err != nil {
		rollbackErr := s.sftpgo.DeleteUser(ctx, input.Username)
		if rollbackErr != nil {
			return nil, fmt.Errorf("create tenant record: %w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, fmt.Errorf("create tenant record: %w", err)
	}

	return &domain.CreatedTenant{
		Tenant:   *tenant,
		Password: password,
		TenantID: tenantID,
	}, nil
}

// ListTenants returns all tenants.
func (s *TenantService) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	return s.repo.ListTenants(ctx)
}

// GetTenant returns one tenant.
func (s *TenantService) GetTenant(ctx context.Context, id int64) (*domain.Tenant, error) {
	tenant, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return nil, ErrTenantNotFound
	}
	return &tenant.Tenant, nil
}

// DeleteTenant removes a tenant from SFTPGo first, then locally.
func (s *TenantService) DeleteTenant(ctx context.Context, id int64) error {
	tenant, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return ErrTenantNotFound
	}
	if err := s.sftpgo.DeleteUser(ctx, tenant.Username); err != nil {
		return fmt.Errorf("delete tenant from sftpgo: %w", err)
	}
	if err := s.repo.DeleteTenant(ctx, id); err != nil {
		return fmt.Errorf("delete tenant locally: %w", err)
	}
	return nil
}

// ValidateTenant checks whether the SFTPGo account exists and is active.
func (s *TenantService) ValidateTenant(ctx context.Context, id int64) (*domain.ValidateTenantResult, error) {
	tenant, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return nil, ErrTenantNotFound
	}
	user, err := s.sftpgo.GetUser(ctx, tenant.Username)
	if err != nil {
		return &domain.ValidateTenantResult{
			Valid:    false,
			Username: tenant.Username,
			Reason:   err.Error(),
		}, nil
	}
	status, _ := user["status"].(float64)
	return &domain.ValidateTenantResult{
		Valid:    status == 1,
		Username: tenant.Username,
	}, nil
}

// UpdateTenantPublicKey rotates the public key in SFTPGo before persisting it.
func (s *TenantService) UpdateTenantPublicKey(ctx context.Context, id int64, publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return fmt.Errorf("public_key is required")
	}
	tenant, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return ErrTenantNotFound
	}
	if err := s.sftpgo.UpdateUserPublicKeys(ctx, tenant.Username, []string{publicKey}); err != nil {
		return fmt.Errorf("update key in sftpgo: %w", err)
	}
	if err := s.repo.UpdateTenantPublicKey(ctx, id, publicKey); err != nil {
		return fmt.Errorf("update key locally: %w", err)
	}
	return nil
}

// ListTenantRecords returns ingested records for a tenant.
func (s *TenantService) ListTenantRecords(ctx context.Context, id int64) ([]domain.Record, error) {
	tenant, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return nil, ErrTenantNotFound
	}
	return s.repo.ListRecords(ctx, tenant.TenantID)
}

// ExternalAuthService authenticates users for SFTPGo's external auth hook.
type ExternalAuthService struct {
	cfg  config.Config
	repo domain.TenantRepository
}

// NewExternalAuthService constructs an ExternalAuthService.
func NewExternalAuthService(cfg config.Config, repo domain.TenantRepository) *ExternalAuthService {
	return &ExternalAuthService{cfg: cfg, repo: repo}
}

// Authenticate validates a tenant password or public key.
func (s *ExternalAuthService) Authenticate(ctx context.Context, req domain.ExternalAuthRequest) (*domain.SFTPGoUser, error) {
	tenant, err := s.repo.GetTenantByUsername(ctx, req.Username)
	if err != nil {
		return nil, ErrUnauthorized
	}

	authenticated := false
	if req.PublicKey != "" && tenant.PublicKey != "" {
		reqParts := strings.Fields(strings.TrimSpace(req.PublicKey))
		storedParts := strings.Fields(strings.TrimSpace(tenant.PublicKey))
		if len(reqParts) >= 2 && len(storedParts) >= 2 && subtle.ConstantTimeCompare([]byte(reqParts[1]), []byte(storedParts[1])) == 1 {
			authenticated = true
		}
	}
	if !authenticated && req.Password != "" && tenant.PasswordHash != "" {
		authenticated = bcrypt.CompareHashAndPassword([]byte(tenant.PasswordHash), []byte(req.Password)) == nil
	}
	if !authenticated {
		return nil, ErrUnauthorized
	}

	user := &domain.SFTPGoUser{
		Status:      1,
		Username:    tenant.Username,
		HomeDir:     tenant.HomeDir,
		Permissions: map[string][]string{"/": {"*"}},
	}
	if fs := s.s3Filesystem(tenant.TenantID); fs != nil {
		user.Filesystem = map[string]any{
			"provider": 1,
			"s3config": map[string]any{
				"bucket":           fs.Bucket,
				"region":           fs.Region,
				"endpoint":         fs.Endpoint,
				"access_key":       fs.AccessKey,
				"access_secret":    map[string]string{"status": "Plain", "payload": fs.SecretKey},
				"key_prefix":       fs.KeyPrefix,
				"force_path_style": true,
				"skip_tls_verify":  true,
			},
		}
	}
	return user, nil
}

// UploadService parses upload events and ingests CSV records.
type UploadService struct {
	repo   domain.TenantRepository
	store  domain.ObjectStore
	bucket string
}

// NewUploadService constructs an UploadService.
func NewUploadService(repo domain.TenantRepository, store domain.ObjectStore, bucket string) *UploadService {
	return &UploadService{repo: repo, store: store, bucket: bucket}
}

// ProcessUploadEvent downloads the uploaded CSV and upserts its rows.
func (s *UploadService) ProcessUploadEvent(ctx context.Context, event domain.UploadEvent) error {
	if event.Action != "upload" {
		return nil
	}
	if event.Username == "" || event.VirtualPath == "" {
		return fmt.Errorf("missing username or virtual_path in event")
	}
	if !strings.HasSuffix(strings.ToLower(event.VirtualPath), ".csv") {
		return nil
	}
	tenant, err := s.repo.GetTenantByUsername(ctx, event.Username)
	if err != nil {
		return fmt.Errorf("resolve tenant: %w", err)
	}

	key := tenant.TenantID + "/" + strings.TrimPrefix(event.VirtualPath, "/")
	body, err := s.store.GetObject(ctx, s.bucket, key)
	if err != nil {
		return fmt.Errorf("fetch csv object: %w", err)
	}
	defer func() { _ = body.Close() }()

	reader := csv.NewReader(body)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read csv header: %w", err)
	}
	index := mapColumns(header)
	for _, required := range []string{"key", "title", "value"} {
		if _, ok := index[required]; !ok {
			return fmt.Errorf("csv missing required column %q", required)
		}
	}

	for {
		row, err := reader.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read csv row: %w", err)
		}
		if len(row) < len(header) {
			return fmt.Errorf("csv row has fewer columns than header")
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(row[index["value"]]), 64)
		if err != nil {
			return fmt.Errorf("invalid csv value %q: %w", row[index["value"]], err)
		}
		if err := s.repo.UpsertRecord(
			ctx,
			tenant.TenantID,
			strings.TrimSpace(row[index["key"]]),
			strings.TrimSpace(row[index["title"]]),
			readOptional(row, index, "description"),
			readOptional(row, index, "category"),
			value,
		); err != nil {
			return fmt.Errorf("upsert record: %w", err)
		}
	}
}

func (s *TenantService) s3Filesystem(tenantID string) *domain.S3FilesystemConfig {
	if s.cfg.S3Endpoint == "" {
		return nil
	}
	return &domain.S3FilesystemConfig{
		Bucket:    s.cfg.S3Bucket,
		Region:    s.cfg.S3Region,
		Endpoint:  s.cfg.S3Endpoint,
		AccessKey: s.cfg.S3AccessKey,
		SecretKey: s.cfg.S3SecretKey,
		KeyPrefix: tenantID + "/",
	}
}

func (s *ExternalAuthService) s3Filesystem(tenantID string) *domain.S3FilesystemConfig {
	if s.cfg.S3Endpoint == "" {
		return nil
	}
	return &domain.S3FilesystemConfig{
		Bucket:    s.cfg.S3Bucket,
		Region:    s.cfg.S3Region,
		Endpoint:  s.cfg.S3Endpoint,
		AccessKey: s.cfg.S3AccessKey,
		SecretKey: s.cfg.S3SecretKey,
		KeyPrefix: tenantID + "/",
	}
}

func newPassword() (string, error) {
	secret, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return secret, nil
}

func newTenantID() (string, error) {
	id, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("generate tenant_id: %w", err)
	}
	return id, nil
}

func newAPIKey() (string, string, error) {
	key, err := randomHex(32)
	if err != nil {
		return "", "", fmt.Errorf("generate api key: %w", err)
	}
	return key, hashAPIKey(key), nil
}

func randomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func mapColumns(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for i, col := range header {
		index[strings.TrimSpace(strings.ToLower(col))] = i
	}
	return index
}

func readOptional(row []string, index map[string]int, column string) string {
	i, ok := index[column]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}
