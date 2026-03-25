package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"sftpgo-manager/internal/domain"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	repo, err := New(filepath.Join(t.TempDir(), "repo.db"))
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestAPIKeys(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	hasKeys, err := repo.HasAPIKeys(ctx)
	if err != nil {
		t.Fatalf("HasAPIKeys() = %v", err)
	}
	if hasKeys {
		t.Fatal("expected no API keys")
	}

	key, err := repo.CreateAPIKey(ctx, "bootstrap", "hash123")
	if err != nil {
		t.Fatalf("CreateAPIKey() = %v", err)
	}
	if key.ID == 0 {
		t.Fatal("expected non-zero key ID")
	}

	ok, err := repo.HasAPIKeyHash(ctx, "hash123")
	if err != nil {
		t.Fatalf("HasAPIKeyHash() = %v", err)
	}
	if !ok {
		t.Fatal("expected API key hash to be found")
	}
}

func TestTenantAndRecordLifecycle(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	created, err := repo.CreateTenant(ctx, domain.TenantRecord{
		Tenant: domain.Tenant{
			TenantID:  "tid123",
			Username:  "tenant1",
			PublicKey: "ssh-ed25519 AAAA tenant1@test",
			HomeDir:   "/srv/sftpgo/data/tid123",
		},
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("CreateTenant() = %v", err)
	}

	got, err := repo.GetTenant(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTenant() = %v", err)
	}
	if got.Username != "tenant1" || got.PasswordHash != "hash" {
		t.Fatalf("unexpected tenant: %+v", got)
	}

	tenants, err := repo.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants() = %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("tenant count = %d, want 1", len(tenants))
	}

	if err := repo.UpdateTenantPublicKey(ctx, created.ID, "ssh-ed25519 BBBB tenant1@test"); err != nil {
		t.Fatalf("UpdateTenantPublicKey() = %v", err)
	}

	if err := repo.UpsertRecord(ctx, "tid123", "REC-001", "First", "Desc", "Cat", 10.5); err != nil {
		t.Fatalf("UpsertRecord() = %v", err)
	}
	if err := repo.UpsertRecord(ctx, "tid123", "REC-001", "Updated", "Desc2", "Cat2", 20.5); err != nil {
		t.Fatalf("UpsertRecord(update) = %v", err)
	}

	records, err := repo.ListRecords(ctx, "tid123")
	if err != nil {
		t.Fatalf("ListRecords() = %v", err)
	}
	if len(records) != 1 || records[0].Title != "Updated" {
		t.Fatalf("unexpected records: %+v", records)
	}

	if err := repo.DeleteTenant(ctx, created.ID); err != nil {
		t.Fatalf("DeleteTenant() = %v", err)
	}
	if _, err := repo.GetTenant(ctx, created.ID); err == nil {
		t.Fatal("expected missing tenant after delete")
	}
}
