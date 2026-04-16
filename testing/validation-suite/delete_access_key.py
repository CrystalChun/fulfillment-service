#!/usr/bin/env python3
"""Delete an access key.

Output JSON:
{
    "success": true,
    "platform": "control_plane"
}
"""

import argparse
import json
import os
import sys
from typing import Any

try:
    import requests
except ImportError:
    print("Error: requests library required. Install with: pip install requests", file=sys.stderr)
    sys.exit(1)


def main() -> int:
    parser = argparse.ArgumentParser(description="Delete access key")
    parser.add_argument("--username", required=True, help="Username (for compatibility)")
    parser.add_argument("--access-key-id", required=True)
    parser.add_argument(
        "--base-url",
        default=os.environ.get("FULFILLMENT_SERVICE_URL", "http://localhost:8001"),
    )
    parser.add_argument(
        "--token",
        default=os.environ.get("FULFILLMENT_SERVICE_TOKEN", ""),
    )
    parser.add_argument(
        "--organization-id",
        default=os.environ.get("FULFILLMENT_ORG_ID", ""),
    )
    args = parser.parse_args()

    result: dict[str, Any] = {
        "success": False,
        "platform": "control_plane",
    }

    if not args.organization_id:
        result["error"] = "Organization ID required"
        print(json.dumps(result, indent=2))
        return 1

    headers = {"Content-Type": "application/json"}
    if args.token:
        headers["Authorization"] = f"Bearer {args.token}"

    try:
        response = requests.delete(
            f"{args.base_url}/api/fulfillment/v1/organizations/{args.organization_id}/access-keys/{args.access_key_id}",
            headers=headers,
            timeout=30,
        )

        if response.status_code not in (200, 204):
            result["error"] = f"Failed to delete access key: HTTP {response.status_code}"
            try:
                error_detail = response.json()
                result["error_detail"] = str(error_detail)
            except Exception:
                result["error_detail"] = response.text[:200]
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
