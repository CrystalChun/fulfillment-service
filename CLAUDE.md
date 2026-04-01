# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

The fulfillment-service is a gRPC server with REST gateway for managing infrastructure resources (clusters, hosts, compute instances, networking). It uses PostgreSQL for storage, OPA for authorization, and supports Kubernetes deployment via Helm/Kustomize.

## Build and Test Commands

```bash
# Build binaries
go build ./cmd/fulfillment-service
go build ./cmd/osac

# Run unit tests only (excludes integration tests in it/)
ginkgo run -r internal

# Run a specific package's tests
ginkgo run internal/servers

# Run tests matching a name pattern
ginkgo run -r internal --focus="CreateCluster"

# Run tests with verbose output
ginkgo run -v internal/servers

# Skip tests matching a pattern
ginkgo run -r internal --skip="database"

# Proto: lint and generate
buf lint
buf generate

# Run all tests including integration (requires kind cluster)
ginkgo run -r
```

### Integration Tests

```bash
# Run integration tests (creates a kind cluster)
ginkgo run it

# Preserve cluster for debugging
IT_KEEP_KIND=true ginkgo run it

# Run only setup (create cluster without tests)
IT_KEEP_KIND=true ginkgo run --label-filter=setup it

# Use kustomize instead of default Helm deployment
IT_DEPLOY_MODE=kustomize ginkgo run it

# Clean up preserved cluster
kind delete cluster --name fulfillment-service-it
```

Requires `/etc/hosts` entries:
- `127.0.0.1 keycloak.keycloak.svc.cluster.local`
- `127.0.0.1 fulfillment-api.innabox.svc.cluster.local`

### Running Locally

See [README.md](README.md) for instructions on running the service locally, including PostgreSQL setup and starting the gRPC server and REST gateway.

### Identity Provider Configuration

The service supports integration with external identity providers (e.g., Keycloak) for managing organizations and users. Configuration is done via environment variables:

**Required:**
- `IDP_URL` - Base URL of the identity provider (e.g., `https://keycloak.example.com`)
- `IDP_TYPE` - Type of identity provider (default: `keycloak`)

**Authentication (OAuth2 Client Credentials Flow):**
- `IDP_AUTH_FLOW=credentials` (default)
- `IDP_CLIENT_ID` - Client ID with service account enabled
- `IDP_CLIENT_SECRET` - Client secret

**Optional:**
- `IDP_REALM` - Realm for authentication (default: `master`)
- `IDP_CA_FILE` - Path to CA bundle for TLS verification

**Note:** The service account needs appropriate admin roles assigned (e.g., `realm-management` client roles for Keycloak: `manage-realm`, `manage-users`, `view-realm`, `view-users`).

## Architecture

### Code Organization

- `cmd/fulfillment-service/` - Service binary entry point (calls `internal/cmd/service.Root()`)
- `cmd/osac/` - CLI binary entry point (calls `internal/cmd/cli.Root()`)
- `internal/cmd/service/start/` - Server startup commands (grpcserver, restgateway, controller)
- `internal/servers/` - gRPC service implementations (one `*_server.go` per resource)
- `internal/database/` - PostgreSQL access layer with generic DAO
- `internal/database/dao/` - Generic type-safe DAO (`GenericDAO[O Object]`)
- `internal/database/migrations/` - SQL migration files
- `internal/api/` - Generated Go code from protobuf (do not edit)
- `internal/auth/` - Authentication, tenancy, and attribution logic
- `internal/controllers/` - Kubernetes controllers
- `internal/testing/` - Test utilities (test server, database helpers, kind helpers)
- `proto/` - Protocol Buffer definitions
- `it/` - Integration tests
- `charts/` - Helm charts

### Proto Structure

Protos are split into public and private APIs under `proto/`:

```text
proto/public/osac/public/v1/   - User-facing API (read-heavy, limited write)
proto/private/osac/private/v1/ - Admin/controller API (full CRUD + Signal RPC)
proto/tests/osac/tests/v1/     - Test-only proto definitions
```

Each resource has `<resource>_type.proto` (message definitions) and `<resource>s_service.proto` (RPC methods). Generated Go code lands in `internal/api/osac/{public,private}/v1/`.

### Server Implementation Pattern

Public servers delegate to private servers and add tenant/auth logic:
- `ClustersServer` (public) wraps `PrivateClustersServer` (private)
- Builder pattern: `ClustersServerBuilder` configures dependencies
- Both server files live in `internal/servers/` (e.g., `clusters_server.go` + `private_clusters_server.go`)

### Database Layer

Uses `pgx/v5` with a generic DAO pattern:
- `GenericDAO[O Object]` provides type-safe CRUD for any protobuf message
- Resources stored as JSON-serialized protobuf in a `data` column
- Standard columns: `id`, `name`, `creation_timestamp`, `deletion_timestamp`, `finalizers`, `creators`, `tenants`, `labels`, `annotations`, `data`
- CEL filter expressions translated to SQL WHERE clauses via `FilterTranslator`
- Migrations in `internal/database/migrations/` (numbered `*.up.sql` files)

### gRPC Interceptor Chain

The gRPC server uses chained interceptors (configured in `internal/cmd/service/start/grpcserver/`):
1. Panic recovery
2. Prometheus metrics
3. Structured logging (slog)
4. Authentication (JWT validation)
5. Database transaction management

### Mock Generation

Uses `go.uber.org/mock` (uber-go/mock). Mocks are generated with `//go:generate mockgen` directives and live alongside source files (e.g., `attribution_logic_mock.go`).

### Testing Pattern

Tests use Ginkgo v2 + Gomega. Typical suite setup in `*_suite_test.go`:
- `BeforeSuite` initializes logger, auth logic, database
- `DeferCleanup` for teardown
- `dao.CreateTables[T]()` dynamically creates test schemas

## Claude Code Hooks

Hooks are configured in `.claude/settings.json` and run automatically during Claude Code sessions:

- **Proto changes** (`PostToolUse`): When a `.proto` file is edited, `buf lint && buf generate` runs automatically to keep generated Go code in sync.
- **Go module changes** (`PostToolUse`): When `go.mod` is edited, `go mod tidy` runs automatically.
- **Pre-commit** (`PreToolUse`): `buf lint` runs before every `git commit` to catch proto issues early.
- **Pre-PR** (`PreToolUse`): `buf lint && ginkgo run -r internal` runs before `gh pr create` to ensure tests pass.

To replicate this setup in another OSAC repo, copy `.claude/settings.json` and adjust the commands as needed.

## Common Pitfalls

- Proto changes require both `buf lint` and `buf generate` before committing
- `SERVICE_SUFFIX` lint rule is intentionally excluded in `buf.yaml`
- Unit tests: run `ginkgo run -r internal` (not `ginkgo run -r`) to avoid triggering integration tests
- The `internal/api/` directory is fully generated - never edit files there manually
