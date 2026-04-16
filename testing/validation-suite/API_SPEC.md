# Fulfillment Service API Specification

Quick reference for the Organizations/IdP API endpoints.

## Base URLs

- **REST Gateway**: `http://localhost:8001` (development)
- **gRPC Server**: `localhost:8000` (development)

## Public API Endpoints

Base path: `/api/fulfillment/v1`

### List Organizations
```http
GET /api/fulfillment/v1/organizations
```

**Query Parameters:**
- `offset` (int32, optional) - Index of first result
- `limit` (int32, optional) - Maximum results to return
- `filter` (string, optional) - CEL expression filter
- `order` (string, optional) - Ordering criteria

**Response:**
```json
{
  "size": 10,
  "total": 42,
  "items": [
    {
      "id": "org-abc123",
      "metadata": {
        "name": "my-org",
        "creation_timestamp": "2026-04-16T10:00:00Z"
      },
      "spec": {
        "display_name": "My Organization"
      }
    }
  ]
}
```

### Get Organization
```http
GET /api/fulfillment/v1/organizations/{id}
```

**Response:**
```json
{
  "object": {
    "id": "org-abc123",
    "metadata": {
      "name": "my-org",
      "creation_timestamp": "2026-04-16T10:00:00Z"
    },
    "spec": {
      "display_name": "My Organization"
    }
  }
}
```

### Create Organization
```http
POST /api/fulfillment/v1/organizations
Content-Type: application/json
```

**Request:**
```json
{
  "object": {
    "metadata": {
      "name": "my-org",
      "labels": {
        "env": "production"
      }
    },
    "spec": {
      "display_name": "My Organization"
    }
  }
}
```

**Response (includes break-glass credentials):**
```json
{
  "object": {
    "id": "org-abc123",
    "metadata": {
      "name": "my-org",
      "creation_timestamp": "2026-04-16T10:00:00Z"
    },
    "spec": {
      "display_name": "My Organization"
    }
  },
  "break_glass_credentials": {
    "user_id": "breakglass-user-abc",
    "username": "breakglass-admin",
    "email": "breakglass@my-org.example.com",
    "temporary_password": "ChangeMe123!"
  }
}
```

⚠️ **Security Note**: The `temporary_password` is only returned once on creation and must be stored securely.

### Update Organization
```http
PATCH /api/fulfillment/v1/organizations/{object.id}
Content-Type: application/json
```

**Request:**
```json
{
  "object": {
    "id": "org-abc123",
    "metadata": {
      "name": "my-org"
    },
    "spec": {
      "display_name": "My Updated Organization"
    }
  },
  "update_mask": {
    "paths": ["spec.display_name"]
  },
  "lock": true
}
```

**Response:**
```json
{
  "object": {
    "id": "org-abc123",
    "metadata": {
      "name": "my-org",
      "version": "2"
    },
    "spec": {
      "display_name": "My Updated Organization"
    }
  }
}
```

### Delete Organization
```http
DELETE /api/fulfillment/v1/organizations/{id}
```

**Response:**
```json
{}
```

## Private API Endpoints

Base path: `/api/private/v1`

All public endpoints plus:

### Signal Organization
```http
POST /api/private/v1/organizations/{id}:signal
```

Signals the controller that reconciliation may be needed.

**Response:**
```json
{}
```

## Authentication

All endpoints require JWT authentication via Bearer token:

```http
Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...
```

## Error Responses

```json
{
  "code": "NOT_FOUND",
  "message": "Organization not found: org-invalid",
  "details": []
}
```

Common HTTP status codes:
- `200 OK` - Success
- `201 Created` - Resource created
- `400 Bad Request` - Invalid input
- `401 Unauthorized` - Missing or invalid authentication
- `403 Forbidden` - Insufficient permissions
- `404 Not Found` - Resource not found
- `409 Conflict` - Resource already exists or version conflict

## Break-Glass Credentials

The `BreakGlassCredentials` message contains emergency administrative credentials:

```protobuf
message BreakGlassCredentials {
  string user_id = 1;              // IdP user ID
  string username = 2;             // Username for authentication
  string email = 3;                // Email address
  string temporary_password = 4;   // MUST be changed on first login
}
```

**Usage:**
1. Returned only on organization creation
2. Provides IdP management permissions (users, identity providers)
3. Cannot modify critical organization settings
4. Store securely (Vault, Kubernetes Secrets, AWS Secrets Manager)
5. Never log the password
6. Change password on first login

## Protocol Buffer Definitions

- [organizations_service.proto](../../proto/public/osac/public/v1/organizations_service.proto) (Public)
- [organizations_service.proto](../../proto/private/osac/private/v1/organizations_service.proto) (Private)
- [identity_type.proto](../../proto/public/osac/public/v1/identity_type.proto) (Types)

## cURL Examples

### List Organizations
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations
```

### Create Organization
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "object": {
      "metadata": {"name": "test-org"},
      "spec": {"display_name": "Test Organization"}
    }
  }' \
  http://localhost:8001/api/fulfillment/v1/organizations
```

### Get Organization
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations/org-abc123
```

### Delete Organization
```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations/org-abc123
```

## Python Example

```python
import requests

BASE_URL = "http://localhost:8001"
TOKEN = "your-jwt-token"

headers = {
    "Authorization": f"Bearer {TOKEN}",
    "Content-Type": "application/json"
}

# Create organization
response = requests.post(
    f"{BASE_URL}/api/fulfillment/v1/organizations",
    headers=headers,
    json={
        "object": {
            "metadata": {"name": "test-org"},
            "spec": {"display_name": "Test Organization"}
        }
    }
)

result = response.json()
org_id = result["object"]["id"]
break_glass = result["break_glass_credentials"]

print(f"Created organization: {org_id}")
print(f"Break-glass username: {break_glass['username']}")
# Store break_glass['temporary_password'] securely!

# List organizations
response = requests.get(
    f"{BASE_URL}/api/fulfillment/v1/organizations",
    headers=headers
)
orgs = response.json()
print(f"Total organizations: {orgs['total']}")
```

## gRPC Examples

Using `grpcurl`:

```bash
# List services
grpcurl -plaintext localhost:8000 list

# List Organizations service methods
grpcurl -plaintext localhost:8000 list osac.public.v1.Organizations

# Call List method
grpcurl -plaintext \
  -d '{"limit": 10}' \
  localhost:8000 \
  osac.public.v1.Organizations/List

# Call Create method
grpcurl -plaintext \
  -d '{
    "object": {
      "metadata": {"name": "test-org"},
      "spec": {"display_name": "Test Organization"}
    }
  }' \
  localhost:8000 \
  osac.public.v1.Organizations/Create
```
