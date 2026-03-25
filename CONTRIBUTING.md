# Contributing

This repository is intended to be approachable for new contributors without lowering the quality bar. Contributions should improve correctness, maintainability, and test coverage in the current layered architecture.

## Before You Start

Read these first:

- [README.md](README.md)

The current application structure is:

- `internal/config`: runtime configuration
- `internal/domain`: core contracts and shared models
- `internal/httpapi`: HTTP transport and integration-style tests
- `internal/service`: business use cases
- `internal/sftpgo`: SFTPGo admin client
- `internal/sqlite`: SQLite persistence
- `internal/storage`: object-store adapter

Keep new code aligned with these boundaries. Avoid putting new business logic into `main.go` or directly into transport adapters.

## Local Setup

Requirements:

- Go `1.25.x`
- Docker and Docker Compose
- `sshpass` for the SFTP smoke flow in the README

Useful commands:

```bash
go test ./...
go test -race ./...
go vet ./...
make docker-up
make docker-down
```

For a full local stack:

```bash
docker compose up -d --build
```

The default local bootstrap token is:

```text
local-bootstrap-token
```

## Contribution Workflow

1. Create a branch from `main`.
2. Make a focused change.
3. Add or update tests for behavior changes.
4. Run the relevant checks locally.
5. Open a pull request with a clear summary and testing notes.

Prefer small pull requests. If a change spans layers, explain why.

## Quality Expectations

Contributions should meet these standards:

- Preserve the current layered design.
- Prefer explicit interfaces and small focused functions.
- Add tests for new behavior and regressions.
- Keep secrets out of logs, docs, fixtures, and responses.
- Avoid silent cross-system inconsistency where external systems are involved.
- Update documentation when behavior or setup changes.

## Testing Expectations

At minimum, contributors should run:

```bash
go test ./...
go vet ./...
```

For changes touching concurrency, auth, persistence, or the SFTP/S3 flow, also run:

```bash
go test -race ./...
```

For Docker or integration-affecting changes, include the commands used and the observed results in the PR description.

## Pull Request Guidelines

A good PR description includes:

- What changed
- Why it changed
- Which layers were touched
- How it was tested
- Any follow-up work that remains

If the PR closes an issue, reference it directly.

## Good First Issues

Starter tasks are tracked as GitHub issues labeled `good first issue` and `help wanted`.

These are intentionally scoped to be:

- self-contained
- low-risk
- useful to the project
- consistent with the current architecture

## What Not To Do

- Do not reintroduce plaintext credential storage or credential leaks in API responses.
- Do not collapse service logic back into HTTP handlers.
- Do not ship broad refactors with no tests.
- Do not use “good first issue” for core correctness or security work that blocks release quality.

## Questions

If an issue is underspecified, ask for clarification in the issue or PR before widening scope. Keeping boundaries tight is better than guessing.
