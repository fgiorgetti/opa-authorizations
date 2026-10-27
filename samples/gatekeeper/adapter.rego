package interconnect.gatekeeper.authz

import future.keywords

import data.lib.interconnect.authz

# Parameters supplied by the Constraint:
#   input.parameters.configmap_namespace  - namespace that holds van-authorizations
#   (default: "opa-authorizations")

# Resolve the ConfigMap namespace from the Constraint parameter, defaulting to
# "opa-authorizations" when the parameter is absent or empty.
cm_namespace := ns if {
    ns := input.parameters.configmap_namespace
    ns != ""
} else := "opa-authorizations"

# Read the van-authorizations ConfigMap from the Gatekeeper inventory.
# The path follows the Gatekeeper sync cache layout:
#   data.inventory.namespace[<ns>]["v1"]["ConfigMap"]["van-authorizations"]
# A missing ConfigMap produces an empty object, which means all namespaces
# are absent from authorizations and every request is denied. This matches
# the controller behaviour: "until the ConfigMap exists, every resource is denied".
raw_cm := data.inventory.namespace[cm_namespace]["v1"]["ConfigMap"]["van-authorizations"]

authorizations := authz_data if {
    raw_json := raw_cm.data["authorizations.json"]
    authz_data := json.unmarshal(raw_json)
} else := {}

# Resolve routingKey exactly as the controller does (internal/controller):
# the value of spec.routingKey, or an empty string when absent. No default is
# applied, so an empty routing key is evaluated as an empty string and denied
# unless a pattern matches it.
routing_key(obj) := rk if {
    rk := obj.spec.routingKey
} else := ""

# Build the flat input expected by lib.interconnect.authz.
lib_input(obj) := {
    "namespace":      obj.metadata.namespace,
    "type":           obj.kind,
    "routing_key":    routing_key(obj),
    "authorizations": authorizations,
}

# Emit a Gatekeeper violation for every message produced by the lib.
# Each violation uses the exact message text from the lib, which matches
# the messages the controller returns at the HTTP API.
violation contains {"msg": msg} if {
    obj := input.review.object
    some msg in authz.violations with input as lib_input(obj)
}
