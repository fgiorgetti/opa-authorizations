#!/bin/sh
# install-gatekeeper.sh - Install Gatekeeper admission controller on Kubernetes.
#
# Usage:
#   ./install-gatekeeper.sh
#
# Environment variables:
#   KUBECTL             Path to kubectl or oc binary (default: auto-detected)
#   GATEKEEPER_VERSION  Gatekeeper release version to install (default: v3.23.1)

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

echo "Installing Gatekeeper ${GATEKEEPER_VERSION}..."
"${CMD}" apply -f "${MANIFEST_URL}"

echo "Waiting for Gatekeeper CRDs to be established..."
"${CMD}" wait --for=condition=Established crd/constrainttemplates.templates.gatekeeper.sh --timeout=120s
"${CMD}" wait --for=condition=Established crd/configs.config.gatekeeper.sh --timeout=120s

echo "Waiting for Gatekeeper deployments to be ready..."
"${CMD}" -n gatekeeper-system rollout status deployment/gatekeeper-controller-manager --timeout=180s
"${CMD}" -n gatekeeper-system rollout status deployment/gatekeeper-audit --timeout=180s

echo "Gatekeeper ${GATEKEEPER_VERSION} installed successfully."
