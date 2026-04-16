#!/usr/bin/env python3
"""Verify that a disabled access key is rejected for authentication.

Output JSON:
{
    "success": true,
    "platform": "control_plane",
    "key_rejected": true
}
"""

import argparse
import json
import os
import sys
import time
from typing import Any

try:
    import requests
except ImportError:
    print("Error: requests library required. Install with: pip install requests", file=sys.stderr)
    sys.exit(1)


def main() -> int:
    parser = argparse.ArgumentParser(description="Verify disabled access key is rejected")
    parser.add_argument("--access-key-id", required=True)
    parser.add_argument("--secret-access-key", required=True)
    parser.add_argument(
        "--base-url",
        default=os.environ.get("FULFILLMENT_SERVICE_URL", "http://localhost:8001"),
    )
    parser.add_argument(
        "--organization-id",
        default=os.environ.get("FULFILLMENT_ORG_ID", ""),
    )
    parser.add_argument("--wait", type=int, default=2, help="Seconds to wait for disable to propagate")
    parser.add_argument("--retries", type=int, default=3, help="Number of retry attempts")
    parser.add_argument("--region", help="Region (for compatibility, not used)")
    args = parser.parse_args()

    result: dict[str, Any] = {
        "success": False,
        "platform": "control_plane",
        "key_rejected": False,
    }

    if not args.organization_id:
        result["error"] = "Organization ID required"
        print(json.dumps(result, indent=2))
        return 1

    # Wait for disable to propagate
    if args.wait > 0:
        time.sleep(args.wait)

    # Retry with exponential backoff to confirm rejection
    last_response_code = None
    for attempt in range(args.retries):
        try:
            headers = {
                "Content-Type": "application/json",
                "X-Access-Key-ID": args.access_key_id,
                "X-Secret-Access-Key": args.secret_access_key,
            }

            response = requests.get(
                f"{args.base_url}/api/fulfillment/v1/organizations/{args.organization_id}/users",
                headers=headers,
                timeout=10,
            )

            last_response_code = response.status_code

            # The key should be rejected (401, 403, or similar)
            # 401 Unauthorized or 403 Forbidden = key is disabled/rejected
            if response.status_code in (401, 403):
                result["key_rejected"] = True
                result["success"] = True
                result["rejection_code"] = response.status_code
                break
            elif response.status_code == 200:
                # This is bad - the key still works!
                result["key_rejected"] = False
                result["error"] = "Access key still works after disable (unexpected)"
                break

        except requests.exceptions.ConnectionError:
            # Connection error doesn't prove the key is rejected
            pass
        except Exception:
            pass

        if attempt < args.retries - 1:
            time.sleep(2 ** (attempt + 1))

    if not result["success"] and last_response_code:
        result["error"] = f"Key not properly rejected, last status: {last_response_code}"

    print(json.dumps(result, indent=2))
    return 0 if result["success"] else 1


if __name__ == "__main__":
    sys.exit(main())
