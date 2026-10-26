#!/usr/bin/env python3
"""Ask the opa-authorizations controller if an endpoint is allowed.

The script prompts for a namespace, an endpoint type (connector or listener)
and a routing key. It sends them to POST /api/authorize and prints the
decision.

The controller address is read from OPA_AUTHZ_URL and defaults to
http://127.0.0.1:8080.
"""

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_URL = "http://127.0.0.1:8080"
URL_ENV = "OPA_AUTHZ_URL"
TIMEOUT_SECONDS = 10
ENDPOINT_TYPES = ("connector", "listener")


def prompt_value(label, input_fn=input):
    """Ask for a value until a non-empty one is given."""
    while True:
        value = input_fn(label + ": ").strip()
        if value:
            return value
        print("A value is required.")


def prompt_type(input_fn=input):
    """Ask for the endpoint type until connector or listener is given."""
    while True:
        value = input_fn("Endpoint type (connector or listener): ").strip().lower()
        if value in ENDPOINT_TYPES:
            return value
        print("The endpoint type must be 'connector' or 'listener'.")


def collect_input(input_fn=input):
    """Return the request body built from the user answers."""
    return {
        "namespace": prompt_value("Namespace", input_fn),
        "type": prompt_type(input_fn),
        "routing_key": prompt_value("Routing key", input_fn),
    }


def base_url(environ=os.environ):
    """Return the controller base URL, accepting only http and https."""
    url = environ.get(URL_ENV, DEFAULT_URL).strip().rstrip("/")
    parsed = urllib.parse.urlparse(url)
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        raise ValueError(URL_ENV + " must be an http or https URL")
    return url


def call_api(url, body):
    """Send the request and return the decoded response."""
    request = urllib.request.Request(
        url + "/api/authorize",
        data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
        if response.status != 200:
            raise RuntimeError("unexpected status " + str(response.status))
        result = json.load(response)
    if not isinstance(result, dict) or not isinstance(result.get("allowed"), bool):
        raise RuntimeError("unexpected response format")
    return result


def format_result(result):
    """Return the lines to print for a decision."""
    lines = ["ALLOWED" if result["allowed"] else "DENIED"]
    for violation in result.get("violations") or []:
        lines.append("  - " + str(violation))
    return lines


def main():
    try:
        url = base_url()
    except ValueError as err:
        print("Error: " + str(err), file=sys.stderr)
        return 2

    try:
        body = collect_input()
    except (EOFError, KeyboardInterrupt):
        print("", file=sys.stderr)
        return 130

    try:
        result = call_api(url, body)
    except urllib.error.HTTPError as err:
        print("Error: the controller answered with status " + str(err.code), file=sys.stderr)
        return 1
    except (urllib.error.URLError, TimeoutError, OSError):
        print("Error: cannot reach the controller at " + url, file=sys.stderr)
        return 1
    except (RuntimeError, ValueError) as err:
        print("Error: " + str(err), file=sys.stderr)
        return 1

    print("\n".join(format_result(result)))
    return 0 if result["allowed"] else 3


if __name__ == "__main__":
    sys.exit(main())
