#!/bin/sh
# generate-lib.sh - Derive the Gatekeeper lib from policies/authz.rego.
#
# Rewrites:
#   package interconnect.authz  ->  package lib.interconnect.authz
#   data.authorizations         ->  input.authorizations
#
# Fails if the package line is not found or if data.authorizations
# remains in the output.
#
# Usage:
#   ./generate-lib.sh [OUTPUT_FILE]
#
# If OUTPUT_FILE is given the result is written there; otherwise it is
# printed to stdout.

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SOURCE="${REPO_ROOT}/policies/authz.rego"

if [ ! -f "${SOURCE}" ]; then
    echo "error: source file not found: ${SOURCE}" >&2
    exit 1
fi

# Verify the expected package line exists before rewriting.
if ! grep -q '^package interconnect\.authz$' "${SOURCE}"; then
    echo "error: expected 'package interconnect.authz' not found in ${SOURCE}" >&2
    exit 1
fi

# Perform substitutions: package rewrite, data.authorizations -> input.authorizations,
# and import rego.v1 -> import future.keywords (for Gatekeeper template compatibility).
result="$(sed \
    -e 's|^package interconnect\.authz$|package lib.interconnect.authz|' \
    -e 's|^import rego\.v1$|import future.keywords|' \
    -e 's|data\.authorizations|input.authorizations|g' \
    "${SOURCE}")"

# Verify no data.authorizations remain in the output.
if echo "${result}" | grep -q 'data\.authorizations'; then
    echo "error: data.authorizations still present in generated output" >&2
    exit 1
fi

# Verify the package was actually rewritten.
if ! echo "${result}" | grep -q '^package lib\.interconnect\.authz$'; then
    echo "error: package lib.interconnect.authz not found in generated output" >&2
    exit 1
fi

if [ $# -ge 1 ]; then
    printf '%s\n' "${result}" > "$1"
else
    printf '%s\n' "${result}"
fi
