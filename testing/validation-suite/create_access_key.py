#!/usr/bin/env python3
"""Create a test user and generate an access key.

Output JSON:
{
    "success": true,
    "platform": "control_plane",
    "username": "isv-test-user-abc123",
    "access_key_id": "OSAC...",
    "secret_access_key": "...",
    "user_id": "user-uuid"
}
"""

import argparse
import json
import os
import sys
import uuid
from typing import Any

try:
    import requests
except ImportError:
    print("Error: requests library required. Install with: pip install requests", file=sys.stderr)
    sys.exit(1)


def main() -> int:
    parser = argparse.ArgumentParser(description="Create test user and access key")
    parser.add_argument(
        "--base-url",
        default=os.environ.get("FULFILLMENT_SERVICE_URL", "http://localhost:8001"),
        help="Base URL for fulfillment-service REST gateway",
    )
    parser.add_argument(
        "--token",
        default=os.environ.get("FULFILLMENT_SERVICE_TOKEN", ""),
        help="Bearer token for authentication",
    )
    parser.add_argument(
        "--organization-id",
        default=os.environ.get("FULFILLMENT_ORG_ID", ""),
        required=False,
        help="Organization ID (required)",
    )
    parser.add_argument(
        "--username-prefix",
        default="isv-test-user",
        help="Username prefix for the test user",
    )
    parser.add_argument("--region", help="Region (for compatibility, not used)")
    args = parser.parse_args()

    result: dict[str, Any] = {
        "success": False,
        "platform": "control_plane",
        "username": "",
        "access_key_id": "",
        "secret_access_key": "",
    }

    if not args.organization_id:
        result["error"] = "Organization ID required (--organization-id or FULFILLMENT_ORG_ID)"
        print(json.dumps(result, indent=2))
        return 1

    headers = {"Content-Type": "application/json"}
    if args.token:
        headers["Authorization"] = f"Bearer {args.token}"

    # Generate unique username
    suffix = uuid.uuid4().hex[:8]
    username = f"{args.username_prefix}-{suffix}"
    result["username"] = username

    try:
        # Step 1: Create user
        user_payload = {
            "object": {
                "metadata": {"name": username},
                "spec": {
                    "username": username,
                    "email": f"{username}@example.com",
                    "email_verified": False,
                    "enabled": True,
                    "first_name": "ISV",
                    "last_name": "Test",
                    "organization_id": args.organization_id,
                    "password": f"Password123!{suffix}",
                    "temporary_password": False,
                },
            }
        }

        response = requests.post(
            f"{args.base_url}/api/fulfillment/v1/organizations/{args.organization_id}/users",
            headers=headers,
            json=user_payload,
            timeout=30,
        )

        if response.status_code not in (200, 201):
            result["error"] = f"Failed to create user: HTTP {response.status_code}"
            try:
                error_detail = response.json()
                result["error_detail"] = str(error_detail)
            except Exception:
                result["error_detail"] = response.text[:200]
            print(json.dumps(result, indent=2))
            return 1

        user_data = response.json()
        user_id = user_data.get("object", {}).get("id")
        if not user_id:
            result["error"] = "User created but no ID returned"
            print(json.dumps(result, indent=2))
            return 1

        result["user_id"] = user_id

        # Step 2: Create access key for the user
        access_key_payload = {
            "object": {
                "metadata": {"name": f"{username}-key"},
                "spec": {
                    "user_id": user_id,
                    "organization_id": args.organization_id,
                    "enabled": True,
                },
            }
        }

        response = requests.post(
            f"{args.base_url}/api/fulfillment/v1/organizations/{args.organization_id}/users/{user_id}/access-keys",
            headers=headers,
            json=access_key_payload,
            timeout=30,
        )

        if response.status_code not in (200, 201):
            result["error"] = f"Failed to create access key: HTTP {response.status_code}"
            try:
                error_detail = response.json()
                result["error_detail"] = str(error_detail)
            except Exception:
                result["error_detail"] = response.text[:200]
            print(json.dumps(result, indent=2))
            return 1

        access_key_data = response.json()
        credentials = access_key_data.get("credentials", {})

        result["access_key_id"] = credentials.get("access_key_id", "")
        result["secret_access_key"] = credentials.get("secret_access_key", "")

        if not result["access_key_id"] or not result["secret_access_key"]:
            result["error"] = "Access key created but credentials not returned"
            print(json.dumps(result, indent=2))
            return 1

        result["success"] = True

    except requests.exceptions.ConnectionError as e:
        result["error"] = f"Connection failed: {str(e)}"
    except requests.exceptions.Timeout:
        result["error"] = "Request timeout"
    except Exception as e:
        result["error"] = f"Unexpected error: {str(e)}"

    print(json.dumps(result, indent=2))
    return 0 if result["success"] else 1


if __name__ == "__main__":
    sys.exit(main())
