package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"sftpgo-manager/internal/domain"
)

// Repository implements persistence on SQLite.
type Repository struct {
	conn *sql.DB
}

// New opens and migrates the SQLite database.
func New(path string) (*Repository, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1)
	repo := &Repository{conn: conn}
	if err := repo.migrate(context.Background()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return repo, nil
}

// Close closes the underlying DB.
func (r *Repository) Close() error {
	return r.conn.Close()
}

// HasAPIKeys reports whether any API key exists.
func (r *Repository) HasAPIKeys(ctx context.Context) (bool, error) {
	var count int
	if err := r.conn.QueryRowContext(ctx, "SELECT COUNT(1) FROM api_keys").Scan(&count); err != nil {
		return false, fmt.Errorf("count api keys: %w", err)
	}
	return count > 0, nil
}

// CreateAPIKey persists a new API key hash.
func (r *Repository) CreateAPIKey(ctx context.Context, label, keyHash string) (*domain.APIKey, error) {
	res, err := r.conn.ExecContext(ctx, "INSERT INTO api_keys (label, key_hash) VALUES (?, ?)", label, keyHash)
	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return &domain.APIKey{ID: id, Label: label, CreatedAt: time.Now()}, nil
}

// HasAPIKeyHash validates an API key hash.
func (r *Repository) HasAPIKeyHash(ctx context.Context, keyHash string) (bool, error) {
	var count int
	err := r.conn.QueryRowContext(ctx, "SELECT COUNT(1) FROM api_keys WHERE key_hash = ? OR key = ?", keyHash, keyHash).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("lookup api key: %w", err)
	}
	return count > 0, nil
}

// CreateTenant creates a new tenant record.
func (r *Repository) CreateTenant(ctx context.Context, tenant domain.TenantRecord) (*domain.Tenant, error) {
	res, err := r.conn.ExecContext(ctx,
		"INSERT INTO tenants (tenant_id, username, password_hash, public_key, home_dir) VALUES (?, ?, ?, ?, ?)",
		tenant.TenantID, tenant.Username, tenant.PasswordHash, tenant.PublicKey, tenant.HomeDir,
	)
	if err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	created := tenant.Tenant
	created.ID = id
	created.CreatedAt = time.Now()
	return &created, nil
}

// GetTenant retrieves a tenant by ID.
func (r *Repository) GetTenant(ctx context.Context, id int64) (*domain.TenantRecord, error) {
	return r.scanTenant(ctx,
		"SELECT id, tenant_id, username, password_hash, public_key, home_dir, created_at FROM tenants WHERE id = ?",
		id,
	)
}

// GetTenantByUsername retrieves a tenant by username.
func (r *Repository) GetTenantByUsername(ctx context.Context, username string) (*domain.TenantRecord, error) {
	return r.scanTenant(ctx,
		"SELECT id, tenant_id, username, password_hash, public_key, home_dir, created_at FROM tenants WHERE username = ?",
		username,
	)
}

// ListTenants returns all tenants.
func (r *Repository) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := r.conn.QueryContext(ctx,
		"SELECT id, tenant_id, username, public_key, home_dir, created_at FROM tenants ORDER BY id",
	)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tenants []domain.Tenant
	for rows.Next() {
		var tenant domain.Tenant
		if err := rows.Scan(&tenant.ID, &tenant.TenantID, &tenant.Username, &tenant.PublicKey, &tenant.HomeDir, &tenant.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, tenant)
	}
	return tenants, rows.Err()
}

// DeleteTenant deletes a tenant.
func (r *Repository) DeleteTenant(ctx context.Context, id int64) error {
	res, err := r.conn.ExecContext(ctx, "DELETE FROM tenants WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateTenantPublicKey updates the stored public key.
func (r *Repository) UpdateTenantPublicKey(ctx context.Context, id int64, publicKey string) error {
	res, err := r.conn.ExecContext(ctx, "UPDATE tenants SET public_key = ? WHERE id = ?", publicKey, id)
	if err != nil {
		return fmt.Errorf("update tenant public key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpsertRecord inserts or updates a CSV-derived record.
func (r *Repository) UpsertRecord(ctx context.Context, tenantID, recordKey, title, description, category string, value float64) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO records (tenant_id, record_key, title, description, category, value, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(tenant_id, record_key) DO UPDATE SET
			title=excluded.title,
			description=excluded.description,
			category=excluded.category,
			value=excluded.value,
			updated_at=CURRENT_TIMESTAMP`,
		tenantID, recordKey, title, description, category, value,
	)
	if err != nil {
		return fmt.Errorf("upsert record: %w", err)
	}
	return nil
}

// ListRecords lists records for a tenant.
func (r *Repository) ListRecords(ctx context.Context, tenantID string) ([]domain.Record, error) {
	rows, err := r.conn.QueryContext(ctx,
		"SELECT id, tenant_id, record_key, title, description, category, value, updated_at FROM records WHERE tenant_id = ? ORDER BY id",
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []domain.Record
	for rows.Next() {
		var record domain.Record
		if err := rows.Scan(&record.ID, &record.TenantID, &record.RecordKey, &record.Title, &record.Description, &record.Category, &record.Value, &record.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan record: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *Repository) migrate(ctx context.Context) error {
	if _, err := r.conn.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY,
			key TEXT UNIQUE,
			key_hash TEXT UNIQUE,
			label TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS tenants (
			id INTEGER PRIMARY KEY,
			tenant_id TEXT UNIQUE NOT NULL,
			username TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT '',
			public_key TEXT NOT NULL DEFAULT '',
			home_dir TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS records (
			id INTEGER PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			record_key TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			value REAL NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tenant_id, record_key)
		);`,
	}
	for _, stmt := range statements {
		if _, err := r.conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}
	if err := ensureColumn(ctx, r.conn, "api_keys", "key_hash", "ALTER TABLE api_keys ADD COLUMN key_hash TEXT UNIQUE"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, r.conn, "tenants", "password_hash", "ALTER TABLE tenants ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, alter string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid        int
			name       string
			typ        string
			notNull    int
			defaultVal any
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan table info: %w", err)
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if rows.Err() != nil {
		return rows.Err()
	}
	if _, err := db.ExecContext(ctx, alter); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("alter table %s add %s: %w", table, column, err)
	}
	return nil
}

func (r *Repository) scanTenant(ctx context.Context, query string, arg any) (*domain.TenantRecord, error) {
	var tenant domain.TenantRecord
	err := r.conn.QueryRowContext(ctx, query, arg).Scan(
		&tenant.ID,
		&tenant.TenantID,
		&tenant.Username,
		&tenant.PasswordHash,
		&tenant.PublicKey,
		&tenant.HomeDir,
		&tenant.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("query tenant: %w", err)
	}
	return &tenant, nil
}
