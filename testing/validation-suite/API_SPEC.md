# Fulfillment Service API Specification

Complete reference for Organizations, Users, and Access Keys APIs.

## Base URLs

- **REST Gateway**: `http://localhost:8001` (development)
- **gRPC Server**: `localhost:8000` (development)

## Public API Endpoints

Base path: `/api/fulfillment/v1`

---

## Table of Contents

1. [Organizations API](#organizations-api)
2. [Users API](#users-api)
3. [Access Keys API](#access-keys-api)
4. [Authentication](#authentication)
5. [Error Responses](#error-responses)

---

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

---

## Users API

Manage users within organizations. Users are created in the identity provider (Keycloak) and tracked in the database.

### List Users
```http
GET /api/fulfillment/v1/organizations/{organization_id}/users
```

**Query Parameters:**
- `offset` (int32, optional) - Index of first result
- `limit` (int32, optional) - Maximum results to return
- `filter` (string, optional) - CEL expression filter
- `order` (string, optional) - Ordering criteria

**Response:**
```json
{
  "size": 5,
  "total": 10,
  "items": [
    {
      "id": "user-abc123",
      "metadata": {
        "name": "john-doe",
        "creation_timestamp": "2026-04-16T10:00:00Z"
      },
      "spec": {
        "username": "johndoe",
        "email": "john.doe@example.com",
        "email_verified": true,
        "enabled": true,
        "first_name": "John",
        "last_name": "Doe",
        "organization_id": "org-123"
      }
    }
  ]
}
```

### Get User
```http
GET /api/fulfillment/v1/organizations/{organization_id}/users/{id}
```

**Response:**
```json
{
  "object": {
    "id": "user-abc123",
    "metadata": {
      "name": "john-doe"
    },
    "spec": {
      "username": "johndoe",
      "email": "john.doe@example.com",
      "enabled": true,
      "organization_id": "org-123"
    }
  }
}
```

### Create User
```http
POST /api/fulfillment/v1/organizations/{organization_id}/users
Content-Type: application/json
```

**Request:**
```json
{
  "object": {
    "metadata": {
      "name": "john-doe"
    },
    "spec": {
      "username": "johndoe",
      "email": "john.doe@example.com",
      "email_verified": false,
      "enabled": true,
      "first_name": "John",
      "last_name": "Doe",
      "organization_id": "org-123",
      "password": "InitialPassword123!",
      "temporary_password": true
    }
  }
}
```

**Response:**
```json
{
  "object": {
    "id": "user-abc123",
    "metadata": {
      "name": "john-doe",
      "creation_timestamp": "2026-04-16T10:00:00Z"
    },
    "spec": {
      "username": "johndoe",
      "email": "john.doe@example.com",
      "enabled": true,
      "organization_id": "org-123",
      "password": ""
    }
  }
}
```

⚠️ **Security Note**: Password is cleared from the response and never returned in GET/LIST operations.

### Update User
```http
PATCH /api/fulfillment/v1/organizations/{organization_id}/users/{id}
Content-Type: application/json
```

**Request:**
```json
{
  "object": {
    "id": "user-abc123",
    "metadata": {
      "name": "john-doe"
    },
    "spec": {
      "email": "john.doe@newdomain.com"
    }
  },
  "update_mask": {
    "paths": ["spec.email"]
  },
  "lock": true
}
```

### Delete User
```http
DELETE /api/fulfillment/v1/organizations/{organization_id}/users/{id}
```

Deletes the user from both the identity provider and the database.

**Response:**
```json
{}
```

---

## Access Keys API

Manage programmatic API access credentials for users. Access keys provide authentication without requiring user passwords.

### List Access Keys
```http
GET /api/fulfillment/v1/organizations/{organization_id}/users/{user_id}/access-keys
```

**Query Parameters:**
- `offset` (int32, optional) - Index of first result
- `limit` (int32, optional) - Maximum results to return
- `filter` (string, optional) - CEL expression filter

**Response:**
```json
{
  "size": 2,
  "total": 2,
  "items": [
    {
      "id": "key-abc123",
      "metadata": {
        "name": "cli-access-key",
        "creation_timestamp": "2026-04-16T10:00:00Z"
      },
      "spec": {
        "user_id": "user-123",
        "organization_id": "org-123",
        "enabled": true
      },
      "status": {
        "phase": "Active",
        "last_used_time": "2026-04-16T11:30:00Z"
      }
    }
  ]
}
```

### Get Access Key
```http
GET /api/fulfillment/v1/organizations/{organization_id}/access-keys/{id}
```

**Response:**
```json
{
  "object": {
    "id": "key-abc123",
    "metadata": {
      "name": "cli-access-key"
    },
    "spec": {
      "user_id": "user-123",
      "organization_id": "org-123",
      "enabled": true
    }
  }
}
```

⚠️ **Security Note**: The secret access key is NEVER returned in GET/LIST operations. It's only returned once at creation time.

### Create Access Key
```http
POST /api/fulfillment/v1/organizations/{organization_id}/users/{user_id}/access-keys
Content-Type: application/json
```

**Request:**
```json
{
  "object": {
    "metadata": {
      "name": "cli-access-key"
    },
    "spec": {
      "user_id": "user-123",
      "organization_id": "org-123",
      "enabled": true
    }
  }
}
```

**Response (includes credentials - ONLY returned once):**
```json
{
  "object": {
    "id": "key-abc123",
    "metadata": {
      "name": "cli-access-key",
      "creation_timestamp": "2026-04-16T10:00:00Z"
    },
    "spec": {
      "user_id": "user-123",
      "organization_id": "org-123",
      "enabled": true
    }
  },
  "credentials": {
    "access_key_id": "OSACAK7X2P9Q4M5N8R1T",
    "secret_access_key": "xJ9kL2mN5pQ8rS1tU4vW7yZ0aB3cD6eF9gH2iJ5"
  }
}
```

⚠️ **CRITICAL**: Store the `secret_access_key` securely! It will never be shown again.

### Disable Access Key
```http
POST /api/fulfillment/v1/organizations/{organization_id}/access-keys/{id}:disable
Content-Type: application/json
```

Disables the access key. Disabled keys cannot be used for authentication.

**Request:**
```json
{}
```

**Response:**
```json
{
  "object": {
    "id": "key-abc123",
    "spec": {
      "enabled": false
    }
  }
}
```

### Enable Access Key
```http
POST /api/fulfillment/v1/organizations/{organization_id}/access-keys/{id}:enable
Content-Type: application/json
```

Re-enables a previously disabled access key.

**Request:**
```json
{}
```

**Response:**
```json
{
  "object": {
    "id": "key-abc123",
    "spec": {
      "enabled": true
    }
  }
}
```

### Delete Access Key
```http
DELETE /api/fulfillment/v1/organizations/{organization_id}/access-keys/{id}
```

Permanently deletes the access key. This action cannot be undone.

**Response:**
```json
{}
```

---

## Private API Endpoints

Base path: `/api/private/v1`

All public endpoints are available with `/api/private/v1` prefix, plus:

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

### Organizations

#### List Organizations
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations
```

#### Create Organization
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

### Users

#### List Users
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/users
```

#### Create User
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "object": {
      "metadata": {"name": "john-doe"},
      "spec": {
        "username": "johndoe",
        "email": "john@example.com",
        "enabled": true,
        "organization_id": "org-123",
        "password": "InitialPassword123!",
        "temporary_password": true
      }
    }
  }' \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/users
```

#### Delete User
```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/users/user-abc123
```

### Access Keys

#### List Access Keys for User
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/users/user-123/access-keys
```

#### Create Access Key
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "object": {
      "metadata": {"name": "cli-key"},
      "spec": {
        "user_id": "user-123",
        "organization_id": "org-123",
        "enabled": true
      }
    }
  }' \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/users/user-123/access-keys
```

#### Disable Access Key
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/access-keys/key-abc123:disable
```

#### Delete Access Key
```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8001/api/fulfillment/v1/organizations/org-123/access-keys/key-abc123
```

## Python Examples

### Complete Workflow: Create Org → User → Access Key

```python
import requests

BASE_URL = "http://localhost:8001"
TOKEN = "your-jwt-token"

headers = {
    "Authorization": f"Bearer {TOKEN}",
    "Content-Type": "application/json"
}

# Step 1: Create organization
response = requests.post(
    f"{BASE_URL}/api/fulfillment/v1/organizations",
    headers=headers,
    json={
        "object": {
            "metadata": {"name": "my-company"},
            "spec": {"display_name": "My Company"}
        }
    }
)
org = response.json()
org_id = org["object"]["id"]
break_glass = org["break_glass_credentials"]

print(f"✓ Created organization: {org_id}")
print(f"  Break-glass user: {break_glass['username']}")
# Store break_glass['temporary_password'] securely!

# Step 2: Create a user
response = requests.post(
    f"{BASE_URL}/api/fulfillment/v1/organizations/{org_id}/users",
    headers=headers,
    json={
        "object": {
            "metadata": {"name": "api-user"},
            "spec": {
                "username": "apiuser",
                "email": "api@mycompany.com",
                "enabled": True,
                "organization_id": org_id,
                "password": "TempPass123!",
                "temporary_password": True
            }
        }
    }
)
user = response.json()
user_id = user["object"]["id"]

print(f"✓ Created user: {user_id}")

# Step 3: Create access key for the user
response = requests.post(
    f"{BASE_URL}/api/fulfillment/v1/organizations/{org_id}/users/{user_id}/access-keys",
    headers=headers,
    json={
        "object": {
            "metadata": {"name": "cli-key"},
            "spec": {
                "user_id": user_id,
                "organization_id": org_id,
                "enabled": True
            }
        }
    }
)
access_key = response.json()
credentials = access_key["credentials"]

print(f"✓ Created access key: {credentials['access_key_id']}")
print(f"  Secret: {credentials['secret_access_key']}")
# CRITICAL: Store the secret securely! It won't be shown again.

# Step 4: List all access keys for the user
response = requests.get(
    f"{BASE_URL}/api/fulfillment/v1/organizations/{org_id}/users/{user_id}/access-keys",
    headers=headers
)
keys = response.json()
print(f"✓ User has {keys['total']} access key(s)")

# Step 5: Disable the access key
key_id = access_key["object"]["id"]
response = requests.post(
    f"{BASE_URL}/api/fulfillment/v1/organizations/{org_id}/access-keys/{key_id}:disable",
    headers=headers,
    json={}
)
print(f"✓ Access key disabled")

# Step 6: Re-enable it
response = requests.post(
    f"{BASE_URL}/api/fulfillment/v1/organizations/{org_id}/access-keys/{key_id}:enable",
    headers=headers,
    json={}
)
print(f"✓ Access key re-enabled")
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
