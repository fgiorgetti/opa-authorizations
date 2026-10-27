#!/bin/sh
# apply-policies.sh - Apply Gatekeeper configuration, ConstraintTemplate, and Constraint.
#
# Generates the ConstraintTemplate from the policy source, applies the
# Config CR and ConstraintTemplate, waits for the CRD to be established,
# and applies the SkupperAuthorization Constraint.
#
# Usage:
#   ./apply-policies.sh
#
# Environment variables:
#   KUBECTL              Path to kubectl or oc binary (default: auto-detected)
#   ENFORCEMENT_ACTION   Enforcement action: deny, warn, or dryrun (default: deny)
#   CONFIGMAP_NAMESPACE  Namespace holding the van-authorizations ConfigMap (default: opa-authorizations)

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

ENFORCEMENT_ACTION="${ENFORCEMENT_ACTION:-deny}"
CONFIGMAP_NAMESPACE="${CONFIGMAP_NAMESPACE:-opa-authorizations}"

# Validate ENFORCEMENT_ACTION
case "${ENFORCEMENT_ACTION}" in
    deny|warn|dryrun)
        ;;
    *)
        echo "error: invalid ENFORCEMENT_ACTION '${ENFORCEMENT_ACTION}' (must be deny, warn, or dryrun)" >&2
        exit 1
        ;;
esac

# Validate CONFIGMAP_NAMESPACE format
if ! printf '%s' "${CONFIGMAP_NAMESPACE}" | grep -Eq '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'; then
    echo "error: invalid CONFIGMAP_NAMESPACE '${CONFIGMAP_NAMESPACE}' (must be a valid Kubernetes namespace name)" >&2
    exit 1
fi

echo "Applying Gatekeeper Config CR (ConfigMap sync)..."
"${CMD}" apply -f "${SCRIPT_DIR}/gatekeeper-config.yaml"

echo "Generating and applying ConstraintTemplate (skupperauthorization)..."
"${SCRIPT_DIR}/generate-template.sh" | "${CMD}" apply -f -

echo "Waiting for SkupperAuthorization CRD to be established..."
# Gatekeeper creates the CRD asynchronously after the ConstraintTemplate is applied.
# Poll until the CRD exists, then wait for the Established condition.
i=0
while [ "$i" -lt 30 ]; do
    if "${CMD}" get crd skupperauthorization.constraints.gatekeeper.sh >/dev/null 2>&1; then
        break
    fi
    sleep 1
    i=$(( i + 1 ))
done

"${CMD}" wait --for=condition=Established crd/skupperauthorization.constraints.gatekeeper.sh --timeout=60s

echo "Applying SkupperAuthorization Constraint (enforcementAction=${ENFORCEMENT_ACTION}, configmap_namespace=${CONFIGMAP_NAMESPACE})..."
sed \
    -e "s|^  enforcementAction:.*|  enforcementAction: ${ENFORCEMENT_ACTION}|" \
    -e "s|^    configmap_namespace:.*|    configmap_namespace: ${CONFIGMAP_NAMESPACE}|" \
    "${SCRIPT_DIR}/skupper-authorization-constraint.yaml" | "${CMD}" apply -f -

echo "Policies applied successfully."
