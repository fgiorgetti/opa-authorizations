#!/bin/sh
# generate-template.sh - Assemble the Gatekeeper ConstraintTemplate YAML.
#
# Generates samples/gatekeeper/lib/authz.rego from policies/authz.rego via
# generate-lib.sh, then embeds the lib and the adapter into a ConstraintTemplate.
#
# Usage:
#   ./generate-template.sh [OUTPUT_FILE]
#
# If OUTPUT_FILE is given the result is written there; otherwise it is
# printed to stdout.

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LIB_FILE="${SCRIPT_DIR}/lib/authz.rego"
ADAPTER_FILE="${SCRIPT_DIR}/adapter.rego"

if [ ! -f "${ADAPTER_FILE}" ]; then
    echo "error: adapter not found: ${ADAPTER_FILE}" >&2
    exit 1
fi

# Generate (or regenerate) the lib from the policy source.
mkdir -p "${SCRIPT_DIR}/lib"
"${SCRIPT_DIR}/generate-lib.sh" "${LIB_FILE}"

# indent_rego FILE SPACES
# Reads FILE, converts any leading tab indentation to spaces (4 spaces per tab),
# then prefixes each non-blank line with SPACES additional spaces.
# YAML block scalars do not allow tab characters as indentation.
indent_rego() {
    pad=""
    i=0
    while [ $i -lt "$2" ]; do
        pad="${pad} "
        i=$(( i + 1 ))
    done
    while IFS= read -r line; do
        if [ -z "${line}" ]; then
            printf '\n'
        else
            # Expand leading tabs to 4 spaces each.
            expanded="$(printf '%s' "${line}" | sed 's/\t/    /g')"
            printf '%s%s\n' "${pad}" "${expanded}"
        fi
    done < "$1"
}

lib_block="$(indent_rego "${LIB_FILE}" 10)"
adapter_block="$(indent_rego "${ADAPTER_FILE}" 8)"

# Build the YAML via a heredoc so that embedded double-quotes and special
# characters inside the Rego blocks are not misinterpreted by the shell.
assemble() {
cat <<YAML
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: skupperauthorization
  labels:
    app.kubernetes.io/name: skupper-authorization
spec:
  crd:
    spec:
      names:
        kind: SkupperAuthorization
      validation:
        openAPIV3Schema:
          type: object
          properties:
            configmap_namespace:
              type: string
              description: Namespace that holds the van-authorizations ConfigMap.
  targets:
    - target: admission.k8s.gatekeeper.sh
      libs:
        - |
${lib_block}
      rego: |
${adapter_block}
YAML
}

if [ $# -ge 1 ]; then
    assemble > "$1"
    echo "template written to $1" >&2
else
    assemble
fi
