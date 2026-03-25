package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sftpgo-manager/internal/config"
	"sftpgo-manager/internal/domain"
	"sftpgo-manager/internal/service"
	"sftpgo-manager/internal/sqlite"
)

type fakeSFTPGo struct {
	mu         sync.Mutex
	users      map[string]map[string]any
	failCreate bool
	failDelete bool
	failUpdate bool
}

func newFakeSFTPGo() *fakeSFTPGo {
	return &fakeSFTPGo{users: map[string]map[string]any{}}
}

func (f *fakeSFTPGo) CreateUser(_ context.Context, username, password, homeDir string, publicKeys []string, fs *domain.S3FilesystemConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreate {
		return errors.New("remote create failed")
	}
	user := map[string]any{
		"username": username,
		"status":   float64(1),
		"home_dir": homeDir,
	}
	if len(publicKeys) > 0 {
		user["public_keys"] = append([]string(nil), publicKeys...)
	}
	if fs != nil {
		user["filesystem"] = fs.KeyPrefix
	}
	f.users[username] = user
	return nil
}

func (f *fakeSFTPGo) GetUser(_ context.Context, username string) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.users[username]
	if !ok {
		return nil, errors.New("user not found")
	}
	clone := map[string]any{}
	for k, v := range user {
		clone[k] = v
	}
	return clone, nil
}

func (f *fakeSFTPGo) UpdateUserPublicKeys(_ context.Context, username string, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUpdate {
		return errors.New("remote update failed")
	}
	user, ok := f.users[username]
	if !ok {
		return errors.New("user not found")
	}
	user["public_keys"] = append([]string(nil), keys...)
	return nil
}

func (f *fakeSFTPGo) DeleteUser(_ context.Context, username string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete {
		return errors.New("remote delete failed")
	}
	delete(f.users, username)
	return nil
}

type fakeStore struct {
	files map[string]string
}

func (s *fakeStore) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	body, ok := s.files[bucket+"/"+key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func newTestRouter(t *testing.T, cfg config.Config, sftpgo *fakeSFTPGo, store domain.ObjectStore) (http.Handler, *sqlite.Repository) {
	t.Helper()
	repo, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	bootstrap := service.NewBootstrapService(cfg, repo)
	auth := service.NewAuthService(repo)
	tenants := service.NewTenantService(cfg, repo, sftpgo)
	external := service.NewExternalAuthService(cfg, repo)
	var uploads *service.UploadService
	if store != nil {
		uploads = service.NewUploadService(repo, store, cfg.S3Bucket)
	}
	return New(bootstrap, auth, tenants, external, uploads), repo
}

func bootstrapKey(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"label":"admin"}`))
	req.Header.Set("X-Bootstrap-Token", token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}
	return body.Key
}

func TestBootstrapOnlyOnce(t *testing.T) {
	cfg := config.Config{BootstrapToken: "bootstrap-secret"}
	router, _ := newTestRouter(t, cfg, newFakeSFTPGo(), nil)

	key := bootstrapKey(t, router, cfg.BootstrapToken)
	if len(key) != 64 {
		t.Fatalf("key length = %d, want 64", len(key))
	}

	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"label":"second"}`))
	req.Header.Set("X-Bootstrap-Token", cfg.BootstrapToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTenantLifecycleAndAuthFlow(t *testing.T) {
	cfg := config.Config{
		BootstrapToken: "bootstrap-secret",
		DataDir:        "/srv/sftpgo/data",
		S3Bucket:       "sftpgo",
	}
	sftpgo := newFakeSFTPGo()
	router, _ := newTestRouter(t, cfg, sftpgo, nil)
	apiKey := bootstrapKey(t, router, cfg.BootstrapToken)

	createReq := httptest.NewRequest(http.MethodPost, "/api/tenants", strings.NewReader(`{"username":"tenant1","public_key":"ssh-ed25519 AAAA tenant1@test"}`))
	createReq.Header.Set("Authorization", "Bearer "+apiKey)
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}

	var created domain.CreatedTenant
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Tenant.Username != "tenant1" || created.Password == "" {
		t.Fatalf("unexpected created tenant payload: %+v", created)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/tenants/1", nil)
	getReq.Header.Set("Authorization", "Bearer "+apiKey)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if strings.Contains(getRec.Body.String(), "password") {
		t.Fatalf("tenant payload leaked password: %s", getRec.Body.String())
	}

	authReq := httptest.NewRequest(http.MethodPost, "/api/auth/hook", strings.NewReader(`{"username":"tenant1","password":"`+created.Password+`","protocol":"SSH"}`))
	authRec := httptest.NewRecorder()
	router.ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("auth status = %d body=%s", authRec.Code, authRec.Body.String())
	}

	validateReq := httptest.NewRequest(http.MethodPost, "/api/tenants/1/validate", nil)
	validateReq.Header.Set("Authorization", "Bearer "+apiKey)
	validateRec := httptest.NewRecorder()
	router.ServeHTTP(validateRec, validateReq)
	if validateRec.Code != http.StatusOK {
		t.Fatalf("validate status = %d body=%s", validateRec.Code, validateRec.Body.String())
	}

	keyReq := httptest.NewRequest(http.MethodPut, "/api/tenants/1/keys", strings.NewReader(`{"public_key":"ssh-ed25519 BBBB tenant1@test"}`))
	keyReq.Header.Set("Authorization", "Bearer "+apiKey)
	keyRec := httptest.NewRecorder()
	router.ServeHTTP(keyRec, keyReq)
	if keyRec.Code != http.StatusOK {
		t.Fatalf("rotate key status = %d body=%s", keyRec.Code, keyRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/tenants/1", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+apiKey)
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestCSVUploadIngestion(t *testing.T) {
	cfg := config.Config{
		BootstrapToken: "bootstrap-secret",
		DataDir:        "/srv/sftpgo/data",
		S3Bucket:       "sftpgo",
		S3Endpoint:     "http://minio:9000",
		S3AccessKey:    "minioadmin-dev",
		S3SecretKey:    "minioadmin-dev-secret",
	}
	sftpgo := newFakeSFTPGo()
	store := &fakeStore{
		files: map[string]string{
			"sftpgo/tid-upload/data.csv": "key,title,description,category,value\nREC-001,First,Desc,Cat,10.5\nREC-002,Second,,Other,20.0\n",
		},
	}
	router, repo := newTestRouter(t, cfg, sftpgo, store)

	if _, err := repo.CreateTenant(context.Background(), domain.TenantRecord{
		Tenant: domain.Tenant{
			TenantID: "tid-upload",
			Username: "tenant-upload",
			HomeDir:  "/srv/sftpgo/data/tid-upload",
		},
		PasswordHash: "hashed",
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	payload := `{"action":"upload","username":"tenant-upload","virtual_path":"/data.csv"}`
	req := httptest.NewRequest(http.MethodPost, "/api/events/upload", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload hook status = %d body=%s", rec.Code, rec.Body.String())
	}

	uploadSvc := service.NewUploadService(repo, store, cfg.S3Bucket)
	if err := uploadSvc.ProcessUploadEvent(context.Background(), domain.UploadEvent{
		Action:      "upload",
		Username:    "tenant-upload",
		VirtualPath: "/data.csv",
	}); err != nil {
		t.Fatalf("process upload: %v", err)
	}

	records, err := repo.ListRecords(context.Background(), "tid-upload")
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want 2", len(records))
	}
	if records[0].RecordKey != "REC-001" || records[1].RecordKey != "REC-002" {
		t.Fatalf("unexpected records: %+v", records)
	}
}
