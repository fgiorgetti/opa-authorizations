#!/bin/sh
# remove-policies.sh - Remove Gatekeeper Constraint, ConstraintTemplate, and Config CR.
#
# Deletes resources in order: Constraint, then ConstraintTemplate, then Config CR,
# tolerating resources that are already absent.
#
# Usage:
#   ./remove-policies.sh
#
# Environment variables:
#   KUBECTL  Path to kubectl or oc binary (default: auto-detected)

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

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

echo "Removing SkupperAuthorization Constraint..."
"${CMD}" delete skupperauthorization skupper-authz --ignore-not-found=true 2>/dev/null || \
"${CMD}" delete -f "${SCRIPT_DIR}/skupper-authorization-constraint.yaml" --ignore-not-found=true 2>/dev/null || true

echo "Removing ConstraintTemplate (skupperauthorization)..."
"${CMD}" delete constrainttemplate skupperauthorization --ignore-not-found=true 2>/dev/null || true

echo "Removing Gatekeeper Config CR..."
"${CMD}" delete -f "${SCRIPT_DIR}/gatekeeper-config.yaml" --ignore-not-found=true 2>/dev/null || true

echo "Policies removed successfully."
