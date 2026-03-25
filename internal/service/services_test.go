package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"sftpgo-manager/internal/config"
	"sftpgo-manager/internal/domain"
)

type fakeKeyRepo struct {
	hasKeys bool
	hashes  map[string]bool
}

func (r *fakeKeyRepo) HasAPIKeys(context.Context) (bool, error) {
	return r.hasKeys, nil
}

func (r *fakeKeyRepo) CreateAPIKey(_ context.Context, label, keyHash string) (*domain.APIKey, error) {
	if r.hashes == nil {
		r.hashes = map[string]bool{}
	}
	r.hasKeys = true
	r.hashes[keyHash] = true
	return &domain.APIKey{ID: 1, Label: label}, nil
}

func (r *fakeKeyRepo) HasAPIKeyHash(_ context.Context, keyHash string) (bool, error) {
	return r.hashes[keyHash], nil
}

type fakeTenantRepo struct {
	nextID  int64
	tenants map[int64]domain.TenantRecord
	records []domain.Record
}

func newFakeTenantRepo() *fakeTenantRepo {
	return &fakeTenantRepo{nextID: 1, tenants: map[int64]domain.TenantRecord{}}
}

func (r *fakeTenantRepo) CreateTenant(_ context.Context, tenant domain.TenantRecord) (*domain.Tenant, error) {
	for _, existing := range r.tenants {
		if existing.Username == tenant.Username {
			return nil, errors.New("duplicate username")
		}
	}
	tenant.ID = r.nextID
	r.nextID++
	r.tenants[tenant.ID] = tenant
	return &tenant.Tenant, nil
}

func (r *fakeTenantRepo) GetTenant(_ context.Context, id int64) (*domain.TenantRecord, error) {
	tenant, ok := r.tenants[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &tenant, nil
}

func (r *fakeTenantRepo) GetTenantByUsername(_ context.Context, username string) (*domain.TenantRecord, error) {
	for _, tenant := range r.tenants {
		if tenant.Username == username {
			copy := tenant
			return &copy, nil
		}
	}
	return nil, errors.New("not found")
}

func (r *fakeTenantRepo) ListTenants(context.Context) ([]domain.Tenant, error) {
	var tenants []domain.Tenant
	for _, tenant := range r.tenants {
		tenants = append(tenants, tenant.Tenant)
	}
	return tenants, nil
}

func (r *fakeTenantRepo) DeleteTenant(_ context.Context, id int64) error {
	if _, ok := r.tenants[id]; !ok {
		return errors.New("not found")
	}
	delete(r.tenants, id)
	return nil
}

func (r *fakeTenantRepo) UpdateTenantPublicKey(_ context.Context, id int64, publicKey string) error {
	tenant, ok := r.tenants[id]
	if !ok {
		return errors.New("not found")
	}
	tenant.PublicKey = publicKey
	r.tenants[id] = tenant
	return nil
}

func (r *fakeTenantRepo) UpsertRecord(_ context.Context, tenantID, recordKey, title, description, category string, value float64) error {
	for i, record := range r.records {
		if record.TenantID == tenantID && record.RecordKey == recordKey {
			r.records[i].Title = title
			r.records[i].Description = description
			r.records[i].Category = category
			r.records[i].Value = value
			return nil
		}
	}
	r.records = append(r.records, domain.Record{
		TenantID:    tenantID,
		RecordKey:   recordKey,
		Title:       title,
		Description: description,
		Category:    category,
		Value:       value,
	})
	return nil
}

func (r *fakeTenantRepo) ListRecords(_ context.Context, tenantID string) ([]domain.Record, error) {
	var out []domain.Record
	for _, record := range r.records {
		if record.TenantID == tenantID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (r *fakeTenantRepo) Close() error { return nil }

type fakeAdmin struct {
	users      map[string]map[string]any
	failCreate bool
	failDelete bool
}

func (a *fakeAdmin) CreateUser(_ context.Context, username, password, homeDir string, publicKeys []string, fs *domain.S3FilesystemConfig) error {
	if a.failCreate {
		return errors.New("create failed")
	}
	if a.users == nil {
		a.users = map[string]map[string]any{}
	}
	a.users[username] = map[string]any{"status": float64(1), "home_dir": homeDir}
	return nil
}

func (a *fakeAdmin) GetUser(_ context.Context, username string) (map[string]any, error) {
	user, ok := a.users[username]
	if !ok {
		return nil, errors.New("not found")
	}
	return user, nil
}

func (a *fakeAdmin) UpdateUserPublicKeys(_ context.Context, username string, keys []string) error {
	if _, ok := a.users[username]; !ok {
		return errors.New("not found")
	}
	a.users[username]["public_keys"] = keys
	return nil
}

func (a *fakeAdmin) DeleteUser(_ context.Context, username string) error {
	if a.failDelete {
		return errors.New("delete failed")
	}
	delete(a.users, username)
	return nil
}

type fakeObjectStore struct {
	body string
}

func (s fakeObjectStore) GetObject(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.body)), nil
}

func TestBootstrapAndAuth(t *testing.T) {
	keys := &fakeKeyRepo{hashes: map[string]bool{}}
	bootstrap := NewBootstrapService(config.Config{BootstrapToken: "token-123"}, keys)

	raw, _, err := bootstrap.BootstrapAPIKey(context.Background(), "bootstrap", "token-123")
	if err != nil {
		t.Fatalf("BootstrapAPIKey() = %v", err)
	}

	auth := NewAuthService(keys)
	if err := auth.ValidateAPIKey(context.Background(), raw); err != nil {
		t.Fatalf("ValidateAPIKey(valid) = %v", err)
	}
	if err := auth.ValidateAPIKey(context.Background(), "bad-key"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ValidateAPIKey(invalid) = %v, want ErrUnauthorized", err)
	}
}

func TestTenantAndUploadServices(t *testing.T) {
	repo := newFakeTenantRepo()
	admin := &fakeAdmin{users: map[string]map[string]any{}}
	cfg := config.Config{
		DataDir:     "/srv/sftpgo/data",
		S3Endpoint:  "http://minio:9000",
		S3Bucket:    "sftpgo",
		S3Region:    "us-east-1",
		S3AccessKey: "minio-user",
		S3SecretKey: "minio-pass",
	}
	tenants := NewTenantService(cfg, repo, admin)

	created, err := tenants.CreateTenant(context.Background(), domain.CreateTenantInput{Username: "tenant1"})
	if err != nil {
		t.Fatalf("CreateTenant() = %v", err)
	}
	if created.Password == "" || created.Tenant.Username != "tenant1" {
		t.Fatalf("unexpected created tenant: %+v", created)
	}

	tenantRecord, err := repo.GetTenant(context.Background(), created.Tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant() = %v", err)
	}
	if tenantRecord.PasswordHash == "" {
		t.Fatal("expected password hash to be stored")
	}
	if bcrypt.CompareHashAndPassword([]byte(tenantRecord.PasswordHash), []byte(created.Password)) != nil {
		t.Fatal("stored password hash does not match generated password")
	}

	if err := tenants.UpdateTenantPublicKey(context.Background(), created.Tenant.ID, "ssh-ed25519 AAAA tenant1@test"); err != nil {
		t.Fatalf("UpdateTenantPublicKey() = %v", err)
	}
	validation, err := tenants.ValidateTenant(context.Background(), created.Tenant.ID)
	if err != nil || !validation.Valid {
		t.Fatalf("ValidateTenant() = %+v, %v", validation, err)
	}

	external := NewExternalAuthService(cfg, repo)
	user, err := external.Authenticate(context.Background(), domain.ExternalAuthRequest{
		Username: "tenant1",
		Password: created.Password,
	})
	if err != nil {
		t.Fatalf("Authenticate() = %v", err)
	}
	if user.Username != "tenant1" || user.Filesystem == nil {
		t.Fatalf("unexpected auth user: %+v", user)
	}

	upload := NewUploadService(repo, fakeObjectStore{
		body: "key,title,description,category,value\nREC-001,First,Desc,Cat,10.5\n",
	}, "sftpgo")
	if err := upload.ProcessUploadEvent(context.Background(), domain.UploadEvent{
		Action:      "upload",
		Username:    "tenant1",
		VirtualPath: "/records.csv",
	}); err != nil {
		t.Fatalf("ProcessUploadEvent() = %v", err)
	}

	records, err := tenants.ListTenantRecords(context.Background(), created.Tenant.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("ListTenantRecords() = %+v, %v", records, err)
	}

	if err := tenants.DeleteTenant(context.Background(), created.Tenant.ID); err != nil {
		t.Fatalf("DeleteTenant() = %v", err)
	}
}
