# Good First Issues

These are starter contributions that are valuable but intentionally non-critical. They should help new contributors learn the codebase without needing to untangle core architecture or security work first.

Each issue below includes scope, suggested files, and an expected outcome.

## 1. Add Config Load Tests

**Goal**

Add direct tests for `config.Load()` and environment-variable handling.

**Why it is good first**

- very small scope
- no external services required
- helps contributors learn the configuration surface

**Suggested files**

- [config.go](internal/config/config.go)
- [config_test.go](internal/config/config_test.go)

**Definition of done**

- tests cover default values
- tests cover env override behavior
- tests do not leak environment changes across cases

## 2. Add HTTP API Negative-Path Tests

**Goal**

Extend router tests for unsupported methods, malformed IDs, and missing JSON payloads.

**Why it is good first**

- works within one package
- improves API reliability
- easy to verify with `go test`

**Suggested files**

- [router.go](internal/httpapi/router.go)
- [router_integration_test.go](internal/httpapi/router_integration_test.go)

**Definition of done**

- add focused failing-path tests
- keep assertions readable
- no production behavior regressions

## 3. Add MinIO Store Unit Tests

**Goal**

Add tests for the MinIO adapter constructor and basic object retrieval behavior through a mocked transport or narrow test seam.

**Why it is good first**

- isolated package
- currently uncovered code
- good entry point into adapters

**Suggested files**

- [minio.go](internal/storage/minio.go)

**Definition of done**

- constructor failure paths are tested
- successful initialization path is tested
- tests do not require a real MinIO container

## 4. Add List Endpoint Coverage

**Goal**

Add direct test coverage for tenant listing and record listing paths that are currently lightly covered.

**Why it is good first**

- reinforces API contracts
- helps improve contributor familiarity with seeded test data

**Suggested files**

- [router_integration_test.go](internal/httpapi/router_integration_test.go)
- [services_test.go](internal/service/services_test.go)

**Definition of done**

- tenant listing behavior is explicitly asserted
- record listing behavior is explicitly asserted
- empty-list behavior is covered

## 5. Add Make Target For Full Verify

**Goal**

Add a single local command that runs the main verification suite contributors should use before opening a PR.

**Why it is good first**

- simple change
- useful for every contributor
- touches tooling rather than core logic

**Suggested files**

- [Makefile](Makefile)
- [justfile](justfile)
- [CONTRIBUTING.md](CONTRIBUTING.md)

**Definition of done**

- one command runs build/test/race/vet
- docs mention the command
- command output is readable

## 6. Improve Swagger/README Consistency

**Goal**

Review the current README and generated API docs for any mismatches in auth headers, endpoint descriptions, or setup steps, then fix the source annotations and regenerate docs.

**Why it is good first**

- good onboarding path
- teaches the contributor the HTTP surface
- useful documentation improvement

**Suggested files**

- [README.md](README.md)
- [main.go](main.go)
- [router.go](internal/httpapi/router.go)
- [docs.go](docs/docs.go)

**Definition of done**

- mismatches are corrected
- generated docs are updated if needed
- no undocumented auth or setup behavior remains

## Issue Labeling Guidance

When maintainers open these as GitHub issues, apply:

- `good first issue`
- `help wanted`
- one area label such as `http`, `tests`, `docs`, `tooling`, or `storage`

Avoid labeling architecture, auth, or data-consistency work as `good first issue` unless it has already been broken into a narrow and low-risk task.
