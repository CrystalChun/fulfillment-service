# Fulfillment Service Validation Suite Integration

This directory contains scripts and configuration for integrating the fulfillment-service Organizations/IdP APIs with the ISV-NCP-Validation-Suite.

## Overview

The fulfillment-service provides IdP management capabilities through its Organizations API, which includes:

- **Organization CRUD**: Create, read, update, delete organizations
- **IdP Integration**: Automatic identity provider setup (Keycloak)
- **Break-glass Credentials**: Emergency admin accounts with IdP management permissions
- **Multi-tenancy**: Tenant-scoped access control

## Files

- `check_api.py` - API health check script that tests Organizations endpoints
- `fulfillment-service.yaml` - Test configuration for ISV-NCP-Validation-Suite
- `README.md` - This file

## API Endpoints

### Public API (`/api/fulfillment/v1`)

- `GET /organizations` - List organizations (user-scoped)
- `GET /organizations/{id}` - Get organization details
- `POST /organizations` - Create organization (returns break-glass credentials)
- `PATCH /organizations/{id}` - Update organization
- `DELETE /organizations/{id}` - Delete organization

### Private API (`/api/private/v1`)

- Same endpoints as public, plus:
- `POST /organizations/{id}:signal` - Signal controller for reconciliation

## Quick Start

### 1. Install Dependencies

```bash
pip install requests
```

### 2. Start Fulfillment Service

Ensure the fulfillment-service is running with both gRPC and REST gateway:

```bash
# Terminal 1: Start gRPC server
go run ./cmd/fulfillment-service start grpcserver \
    --grpc-listener-address=localhost:8000 \
    --db-url=postgres://user:pass@localhost:5432/db

# Terminal 2: Start REST gateway
go run ./cmd/fulfillment-service start restgateway \
    --http-listener-address=localhost:8001 \
    --grpc-server-address=localhost:8000 \
    --grpc-server-plaintext
```

### 3. Test API Health

```bash
# Test public API
python3 check_api.py --base-url http://localhost:8001

# Test private API
python3 check_api.py --base-url http://localhost:8001 --use-private-api

# Test with authentication
export FULFILLMENT_SERVICE_TOKEN="your-jwt-token"
python3 check_api.py --base-url http://localhost:8001
```

Expected output:
```json
{
  "success": true,
  "platform": "control_plane",
  "account_id": "fulfillment-service (public)",
  "base_url": "http://localhost:8001",
  "tests": {
    "organizations_list": {
      "passed": true,
      "latency_ms": 45.23,
      "status_code": 200
    },
    "organizations_get": {
      "passed": true,
      "latency_ms": 23.12,
      "status_code": 404,
      "note": "API reachable (resource not found)"
    },
    "organizations_create_endpoint": {
      "passed": true,
      "latency_ms": 12.34,
      "status_code": 401,
      "note": "API reachable (authentication required)"
    }
  },
  "summary": "3/3 endpoints reachable"
}
```

## Integration with ISV-NCP-Validation-Suite

### Option 1: Copy to Existing Test Suite

Copy this directory to the ISV-NCP-Validation-Suite repository:

```bash
# From the fulfillment-service root directory
cp -r testing/validation-suite ../ISV-NCP-Validation-Suite/isvctl/configs/osac-fulfillment/

cd ../ISV-NCP-Validation-Suite
uv run isvctl test run -f isvctl/configs/osac-fulfillment/fulfillment-service.yaml
```

### Option 2: Reference from Current Location

Run directly from this directory:

```bash
cd testing/validation-suite
python3 check_api.py
```

### Option 3: Integrate into Custom Platform Config

Add the fulfillment-service health check to your own platform's control-plane validation:

```yaml
# In your custom platform config (e.g., isvctl/configs/my-platform/control-plane.yaml)
steps:
  - name: check_fulfillment_service
    phase: setup
    command: "python3 /path/to/fulfillment-service/testing/validation-suite/check_api.py"
    args:
      - "--base-url"
      - "https://your-fulfillment-service.example.com"
      - "--token"
      - "{{fulfillment_service_token}}"
    validations:
      - type: json_field
        field: success
        expected: true
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `FULFILLMENT_SERVICE_URL` | Base URL for REST gateway | `http://localhost:8001` |
| `FULFILLMENT_SERVICE_TOKEN` | JWT bearer token for authentication | (none) |

## Authentication

The Organizations API supports JWT-based authentication. To get a token:

1. **Development/Testing**: Use the test token from integration tests
2. **Production**: Obtain from your identity provider (Keycloak, etc.)

Example with token:
```bash
export FULFILLMENT_SERVICE_TOKEN="eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."
python3 check_api.py
```

## API Contract

The Organizations API follows the OSAC resource model:

### Create Organization Request
```json
{
  "object": {
    "metadata": {
      "name": "my-organization",
      "labels": {"env": "prod"}
    },
    "spec": {
      "display_name": "My Organization"
    }
  }
}
```

### Create Organization Response
```json
{
  "object": {
    "id": "org-abc123",
    "metadata": {
      "name": "my-organization",
      "creation_timestamp": "2026-04-16T10:00:00Z"
    },
    "spec": {
      "display_name": "My Organization"
    }
  },
  "break_glass_credentials": {
    "user_id": "breakglass-user-id",
    "username": "breakglass-admin",
    "email": "breakglass@my-organization.example.com",
    "temporary_password": "ChangeMe123!"
  }
}
```

## Testing Organization Lifecycle

To extend the validation to test full organization lifecycle (create, list, get, delete):

1. Implement the stub scripts:
   - `create_organization.py`
   - `list_organizations.py`
   - `get_organization.py`
   - `delete_organization.py`

2. Uncomment the corresponding steps in `fulfillment-service.yaml`

3. Run the full suite:
   ```bash
   uv run isvctl test run -f fulfillment-service.yaml
   ```

See the ISV-NCP-Validation-Suite documentation for more details on implementing stub scripts:
- [Templates README](https://github.com/NVIDIA/ISV-NCP-Validation-Suite/tree/main/isvctl/configs/templates)
- [AWS Reference Implementation](https://github.com/NVIDIA/ISV-NCP-Validation-Suite/blob/main/docs/references/aws.md)

## Protocol Buffers

The API is defined in Protocol Buffers:

- **Public API**: `proto/public/osac/public/v1/organizations_service.proto`
- **Private API**: `proto/private/osac/private/v1/organizations_service.proto`
- **Types**: `proto/*/osac/*/v1/organization_type.proto`, `identity_type.proto`

Generate updated Go code:
```bash
buf generate
```

## Support

For issues or questions:
- Fulfillment Service: [fulfillment-service repository](https://github.com/osac-project/fulfillment-service)
- Validation Suite: [ISV-NCP-Validation-Suite repository](https://github.com/NVIDIA/ISV-NCP-Validation-Suite)
