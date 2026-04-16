#!/usr/bin/env python3
"""Check fulfillment-service API connectivity and health.

Tests the fulfillment-service APIs:
- Organizations API: IdP management, break-glass credentials
- Users API: User CRUD operations within organizations
- Access Keys API: Programmatic API access credentials

Required environment variables:
    FULFILLMENT_SERVICE_URL: Base URL for the REST gateway (default: http://localhost:8001)
    FULFILLMENT_SERVICE_TOKEN: Bearer token for authentication (optional for health checks)

Output JSON:
{
    "success": true,
    "platform": "control_plane",
    "account_id": "fulfillment-service",
    "tests": {
        "organizations_list": {"passed": true, "latency_ms": 123},
        "users_list": {"passed": true, "latency_ms": 89},
        "access_keys_list": {"passed": true, "latency_ms": 45}
    }
}

Usage:
    python check_api.py --base-url http://localhost:8001 --token <jwt-token>
"""

import argparse
import json
import os
import sys
import time
from typing import Any
from urllib.parse import urljoin

try:
    import requests
except ImportError:
    print("Error: requests library required. Install with: pip install requests", file=sys.stderr)
    sys.exit(1)


def test_endpoint(
    base_url: str,
    path: str,
    method: str = "GET",
    headers: dict[str, str] | None = None,
    json_body: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Test a single API endpoint."""
    result: dict[str, Any] = {"passed": False}
    start = time.time()

    try:
        url = urljoin(base_url, path)
        response = requests.request(
            method=method,
            url=url,
            headers=headers or {},
            json=json_body,
            timeout=10,
        )

        latency_ms = (time.time() - start) * 1000
        result["latency_ms"] = round(latency_ms, 2)
        result["status_code"] = response.status_code

        # Success conditions:
        # - 2xx for successful operations
        # - 401/403 means API is reachable but not authorized (still counts as passed)
        # - 404 for GET operations on non-existent resources is acceptable
        if response.status_code in (200, 201, 204):
            result["passed"] = True
        elif response.status_code in (401, 403):
            result["passed"] = True
            result["note"] = "API reachable (authentication required)"
        elif response.status_code == 404 and method == "GET":
            result["passed"] = True
            result["note"] = "API reachable (resource not found)"
        else:
            result["error"] = f"HTTP {response.status_code}"
            try:
                error_body = response.json()
                result["error_detail"] = error_body.get("message", str(error_body))
            except Exception:
                result["error_detail"] = response.text[:200]

    except requests.exceptions.ConnectionError as e:
        result["error"] = f"Connection failed: {str(e)}"
    except requests.exceptions.Timeout:
        result["error"] = "Request timeout"
    except Exception as e:
        result["error"] = f"Unexpected error: {str(e)}"

    return result


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Check fulfillment-service API health (Organizations/Users/Access Keys)"
    )
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
        "--api-version",
        default="v1",
        choices=["v1"],
        help="API version to test",
    )
    parser.add_argument(
        "--use-private-api",
        action="store_true",
        help="Test private API endpoints instead of public",
    )
    args = parser.parse_args()

    # Prepare headers
    headers = {"Content-Type": "application/json"}
    if args.token:
        headers["Authorization"] = f"Bearer {args.token}"

    # Determine API path prefix
    if args.use_private_api:
        api_prefix = f"/api/private/{args.api_version}"
        platform_name = "fulfillment-service (private)"
    else:
        api_prefix = f"/api/fulfillment/{args.api_version}"
        platform_name = "fulfillment-service (public)"

    result: dict[str, Any] = {
        "success": False,
        "platform": "control_plane",
        "account_id": platform_name,
        "base_url": args.base_url,
        "tests": {},
    }

    # Test Organizations endpoints
    result["tests"]["organizations_list"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations",
        method="GET",
        headers=headers,
    )

    result["tests"]["organizations_get"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/health-check-test-id",
        method="GET",
        headers=headers,
    )

    result["tests"]["organizations_create_endpoint"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations",
        method="POST",
        headers=headers,
        json_body={},  # Empty body will fail validation, but proves endpoint exists
    )

    # Test Users endpoints
    # Use a test org ID for the path
    test_org_id = "test-org"

    result["tests"]["users_list"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/{test_org_id}/users",
        method="GET",
        headers=headers,
    )

    result["tests"]["users_get"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/{test_org_id}/users/health-check-user-id",
        method="GET",
        headers=headers,
    )

    result["tests"]["users_create_endpoint"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/{test_org_id}/users",
        method="POST",
        headers=headers,
        json_body={},
    )

    # Test Access Keys endpoints
    test_user_id = "test-user"

    result["tests"]["access_keys_list"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/{test_org_id}/users/{test_user_id}/access-keys",
        method="GET",
        headers=headers,
    )

    result["tests"]["access_keys_get"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/{test_org_id}/access-keys/health-check-key-id",
        method="GET",
        headers=headers,
    )

    result["tests"]["access_keys_create_endpoint"] = test_endpoint(
        base_url=args.base_url,
        path=f"{api_prefix}/organizations/{test_org_id}/users/{test_user_id}/access-keys",
        method="POST",
        headers=headers,
        json_body={},
    )

    # Count passed tests
    passed = sum(1 for t in result["tests"].values() if t.get("passed", False))
    total = len(result["tests"])

    result["summary"] = f"{passed}/{total} endpoints reachable"
    result["success"] = passed >= 2  # At least List and Get should be reachable

    # Add recommendation if not all tests passed
    if not result["success"]:
        result["recommendation"] = (
            "Ensure fulfillment-service is running and accessible at the specified URL. "
            "If authentication is required, provide a valid token with --token or "
            "FULFILLMENT_SERVICE_TOKEN environment variable."
        )

    print(json.dumps(result, indent=2))
    return 0 if result["success"] else 1


if __name__ == "__main__":
    sys.exit(main())
