package sftpgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"sftpgo-manager/internal/domain"
)

func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	var putPayload map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin-user" || pass != "admin-pass" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-token",
			"expires_at":   "2099-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("/api/v2/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/api/v2/users/testuser", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"username":    "testuser",
				"status":      1,
				"home_dir":    "/data/testuser",
				"permissions": map[string][]string{"/": {"*"}},
				"filesystem":  map[string]any{"provider": 1},
			})
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&putPayload); err != nil {
				t.Fatalf("decode put payload: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		expectedPermissions := map[string]any{"/": []any{"*"}}
		if putPayload != nil && !reflect.DeepEqual(putPayload["permissions"], expectedPermissions) {
			t.Fatalf("permissions in put payload = %#v, want %#v", putPayload["permissions"], expectedPermissions)
		}
		if putPayload != nil && putPayload["home_dir"] != "/data/testuser" {
			t.Fatalf("home_dir in put payload = %#v", putPayload["home_dir"])
		}
	})
	return srv
}

func TestClientLifecycle(t *testing.T) {
	srv := newMockServer(t)
	client := New(srv.URL, "admin-user", "admin-pass")

	if err := client.CreateUser(context.Background(), "testuser", "secret", "/data/testuser", nil, &domain.S3FilesystemConfig{
		Bucket:    "bucket",
		Region:    "us-east-1",
		Endpoint:  "http://minio:9000",
		AccessKey: "access",
		SecretKey: "secret",
		KeyPrefix: "tenant1/",
	}); err != nil {
		t.Fatalf("CreateUser() = %v", err)
	}

	user, err := client.GetUser(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("GetUser() = %v", err)
	}
	if user["username"] != "testuser" {
		t.Fatalf("unexpected user: %+v", user)
	}

	if err := client.UpdateUserPublicKeys(context.Background(), "testuser", []string{"ssh-ed25519 AAAA"}); err != nil {
		t.Fatalf("UpdateUserPublicKeys() = %v", err)
	}
	if err := client.DeleteUser(context.Background(), "testuser"); err != nil {
		t.Fatalf("DeleteUser() = %v", err)
	}
}
