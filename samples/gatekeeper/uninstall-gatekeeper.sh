#!/bin/sh
# uninstall-gatekeeper.sh - Remove Gatekeeper admission controller from Kubernetes.
#
# Usage:
#   ./uninstall-gatekeeper.sh
#
# Environment variables:
#   KUBECTL             Path to kubectl or oc binary (default: auto-detected)
#   GATEKEEPER_VERSION  Gatekeeper release version used for manifest removal (default: v3.23.1)

set -eu

# Determine CLI tool
if [ -n "${KUBECTL:-}" ]; then
    CMD="${KUBECTL}"
elif command -v kubectl >/dev/null 2>&1; then
    CMD="kubectl"
elif command -v oc >/dev/null 2>&1; then
    CMD="oc"
else
    echo "error: neither kubectl nor oc found in PATH" >&2
    exit 1
fi

GATEKEEPER_VERSION="${GATEKEEPER_VERSION:-v3.23.1}"

# Validate GATEKEEPER_VERSION format (e.g. v3.23.1)
if ! printf '%s' "${GATEKEEPER_VERSION}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$'; then
    echo "error: invalid GATEKEEPER_VERSION '${GATEKEEPER_VERSION}' (expected format vX.Y.Z)" >&2
    exit 1
fi

MANIFEST_URL="https://raw.githubusercontent.com/open-policy-agent/gatekeeper/${GATEKEEPER_VERSION}/deploy/gatekeeper.yaml"

echo "Removing Gatekeeper policies and constraints (if present)..."
# Delete Constraints before ConstraintTemplates (deleting a template removes the constraint CRD)
"${CMD}" delete skupperauthorization --all --ignore-not-found=true 2>/dev/null || true
"${CMD}" delete constrainttemplate skupperauthorization --ignore-not-found=true 2>/dev/null || true
"${CMD}" delete config.config.gatekeeper.sh -n gatekeeper-system config --ignore-not-found=true 2>/dev/null || true

echo "Removing Gatekeeper ${GATEKEEPER_VERSION} manifests..."
"${CMD}" delete -f "${MANIFEST_URL}" --ignore-not-found=true

echo "Gatekeeper ${GATEKEEPER_VERSION} uninstalled successfully."
