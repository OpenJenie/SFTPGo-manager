# SFTPGo Manager

Go backend for multi-tenant SFTP user management. Each tenant gets an isolated S3 prefix, and uploaded CSV files are automatically parsed into a records table.

Built on top of [SFTPGo](https://github.com/drakkan/sftpgo) for SFTP and [MinIO](https://min.io/) for S3-compatible object storage.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, workflow, and quality expectations.

## Architecture

<a href="diagrams/exported/architecture-light.svg">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="diagrams/exported/architecture-dark.svg">
    <img alt="Architecture diagram" src="diagrams/exported/architecture-light.svg">
  </picture>
</a>

## Sequence Diagrams

### Tenant Creation

<a href="diagrams/exported/sequence-tenant-creation-light.svg">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="diagrams/exported/sequence-tenant-creation-dark.svg">
    <img alt="Tenant creation sequence diagram" src="diagrams/exported/sequence-tenant-creation-light.svg">
  </picture>
</a>

### SFTP Authentication (External Auth Hook)

<a href="diagrams/exported/sequence-sftp-auth-light.svg">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="diagrams/exported/sequence-sftp-auth-dark.svg">
    <img alt="SFTP authentication sequence diagram" src="diagrams/exported/sequence-sftp-auth-light.svg">
  </picture>
</a>

### CSV Upload and Processing

<a href="diagrams/exported/sequence-csv-processing-light.svg">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="diagrams/exported/sequence-csv-processing-dark.svg">
    <img alt="CSV upload and processing sequence diagram" src="diagrams/exported/sequence-csv-processing-light.svg">
  </picture>
</a>

## Quick Start

```bash
docker compose up -d --build
```

Wait for all services to become healthy, then follow the usage examples below.

## Services

| Service     | Port              | Description                   |
|-------------|-------------------|-------------------------------|
| **backend** | 9090              | Management API + Swagger UI   |
| **sftpgo**  | 8080 (HTTP), 2022 (SFTP) | SFTPGo server         |
| **minio**   | 9000 (S3), 9001 (Console) | S3-compatible storage |

## API Endpoints

| Method | Path                       | Auth     | Description                      |
|--------|----------------------------|----------|----------------------------------|
| POST   | `/api/keys`                | bootstrap token | Create the initial API key once |
| POST   | `/api/tenants`               | API key  | Create a new tenant              |
| GET    | `/api/tenants`               | API key  | List all tenants                 |
| GET    | `/api/tenants/{id}`          | API key  | Get tenant details               |
| DELETE | `/api/tenants/{id}`          | API key  | Remove tenant                    |
| POST   | `/api/tenants/{id}/validate` | API key  | Check tenant is active in SFTPGo |
| PUT    | `/api/tenants/{id}/keys`     | API key  | Update SSH public key            |
| GET    | `/api/tenants/{id}/records`  | API key  | List ingested records            |
| POST   | `/api/auth/hook`           | internal | SFTPGo external auth hook        |
| POST   | `/api/events/upload`       | internal | SFTPGo upload event hook         |

All endpoints except `/swagger/*` and internal hooks require bootstrap or API-key auth.

## Swagger UI

Open http://localhost:9090/swagger/index.html after starting the stack.

## Usage

### 1. Bootstrap the initial API key

```bash
curl -s -X POST localhost:9090/api/keys \
  -H "X-Bootstrap-Token: local-bootstrap-token" | jq .
```

This endpoint is one-time only. After the first API key exists, later bootstrap attempts return `409 Conflict`.

### 2. Create a tenant

```bash
curl -s -H "Authorization: Bearer <KEY>" \
     -X POST localhost:9090/api/tenants \
     -d '{"username":"tenant1"}' | jq .
```

The generated password is returned only in the create response. `GET /api/tenants` and `GET /api/tenants/{id}` do not expose password material.

### 3. Upload a CSV via SFTP

```bash
sshpass -p '<PASSWORD>' sftp -P 2022 -o StrictHostKeyChecking=no tenant1@localhost <<< "put /tmp/data.csv"
```

### 4. Query records

```bash
curl -s -H "Authorization: Bearer <KEY>" localhost:9090/api/tenants/1/records | jq .
```

## CSV Format

The CSV must have a header row. Required columns: `key`, `title`, `value`. Optional columns: `description`, `category`.

```csv
key,title,description,category,value
REC-001,First Record,A description,category-a,10.5
REC-002,Second Record,Another description,category-b,20.0
```

Column order does not matter. Non-CSV files are silently ignored.

## Configuration

All configuration is via environment variables:

| Variable           | Default                    | Description              |
|--------------------|----------------------------|--------------------------|
| `SFTPGO_URL`       | `http://localhost:8080`    | SFTPGo API URL           |
| `SFTPGO_ADMIN_USER`| _(required in production)_ | SFTPGo admin username    |
| `SFTPGO_ADMIN_PASS`| _(required in production)_ | SFTPGo admin password    |
| `BOOTSTRAP_TOKEN`  | _(disabled if empty)_      | One-time bootstrap token |
| `LISTEN_ADDR`      | `:9090`                    | Backend listen address   |
| `DB_PATH`          | `sftpgo.db`                | SQLite database path     |
| `DATA_DIR`         | `/srv/sftpgo/data`         | Base data directory      |
| `S3_BUCKET`        | `sftpgo`                   | S3 bucket name           |
| `S3_REGION`        | `us-east-1`                | S3 region                |
| `S3_ENDPOINT`      | _(empty = no S3)_          | S3/MinIO endpoint        |
| `S3_ACCESS_KEY`    | _(empty)_                  | S3 access key            |
| `S3_SECRET_KEY`    | _(empty)_                  | S3 secret key            |

## Project Structure

```
.
├── main.go              # Entrypoint and dependency wiring
├── internal/
│   ├── config/          # Environment-based configuration
│   ├── domain/          # Core models and interfaces
│   ├── httpapi/         # HTTP transport and integration tests
│   ├── service/         # Tenant/bootstrap/auth/upload use cases
│   ├── sftpgo/          # SFTPGo admin API adapter
│   ├── sqlite/          # SQLite repository
│   └── storage/         # Object-store adapter
├── docs/                # Generated Swagger docs
├── diagrams/            # Excalidraw source files
│   └── exported/        # Auto-generated light/dark SVGs
├── Makefile             # Make targets
├── justfile             # Just recipes
├── Dockerfile           # Multi-stage Go build
└── docker-compose.yml   # Full dev stack
```

## Testing

```bash
go test ./... -v
go test -race ./...
go test ./... -cover
```

## Development

Regenerate Swagger docs after changing handler annotations:

```bash
go install github.com/swaggo/swag/cmd/swag@latest
swag init
```
