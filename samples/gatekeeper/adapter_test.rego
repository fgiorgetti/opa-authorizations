package interconnect.gatekeeper.authz_test

import rego.v1

import data.interconnect.gatekeeper.authz

# ---------------------------------------------------------------------------
# Mock inventory and parameters. These replicate the ConfigMap data the
# Gatekeeper inventory would surface via data.inventory.
# ---------------------------------------------------------------------------

mock_inventory := {
    "namespace": {
        "opa-authorizations": {
            "v1": {
                "ConfigMap": {
                    "van-authorizations": {
                        "data": {
                            "authorizations.json": "{\"backend\":{\"provides\":[\"payment-*\",\"orders\"],\"requires\":[\"inventory\",\"audit-*\"]},\"frontend\":{\"provides\":[\"ui-service\"],\"requires\":[\"payment-*\",\"orders\"]}}"
                        }
                    }
                }
            }
        }
    }
}

mock_params := {"configmap_namespace": "opa-authorizations"}

# Helper: build an AdmissionReview-like input for a given kind/ns/rk.
review(kind, ns, rk) := {
    "parameters": mock_params,
    "review": {
        "object": {
            "apiVersion": "skupper.io/v2alpha1",
            "kind": kind,
            "metadata": {"name": "test-resource", "namespace": ns},
            "spec": {"routingKey": rk, "host": "localhost", "port": 8080}
        }
    }
}

# ---------------------------------------------------------------------------
# Cases from policies/authz_test.rego - all must produce the same outcome.
# ---------------------------------------------------------------------------

test_connector_exact_match_allowed if {
    result := authz.violation
        with input as review("Connector", "backend", "orders")
        with data.inventory as mock_inventory
    count(result) == 0
}

test_connector_wildcard_match_allowed if {
    result := authz.violation
        with input as review("Connector", "backend", "payment-v1")
        with data.inventory as mock_inventory
    count(result) == 0
}

test_listener_exact_match_allowed if {
    result := authz.violation
        with input as review("Listener", "backend", "inventory")
        with data.inventory as mock_inventory
    count(result) == 0
}

test_listener_wildcard_match_allowed if {
    result := authz.violation
        with input as review("Listener", "frontend", "payment-v2")
        with data.inventory as mock_inventory
    count(result) == 0
}

test_denied_when_namespace_missing if {
    result := authz.violation
        with input as review("Connector", "unknown-ns", "orders")
        with data.inventory as mock_inventory
    result == {{"msg": "namespace 'unknown-ns' not found in authorizations"}}
}

test_denied_when_routing_key_not_in_list if {
    result := authz.violation
        with input as review("Connector", "backend", "unauthorized-key")
        with data.inventory as mock_inventory
    result == {{"msg": "routing key 'unauthorized-key' is not permitted for Connector in namespace 'backend'"}}
}

test_denied_when_listener_uses_provides_key if {
    result := authz.violation
        with input as review("Listener", "backend", "orders")
        with data.inventory as mock_inventory
    result == {{"msg": "routing key 'orders' is not permitted for Listener in namespace 'backend'"}}
}

# ---------------------------------------------------------------------------
# Adapter-specific cases.
# ---------------------------------------------------------------------------

# Missing ConfigMap -> empty authorizations -> every request denied.
test_denied_when_configmap_missing if {
    result := authz.violation
        with input as review("Connector", "backend", "orders")
        with data.inventory as {"namespace": {}}
    result == {{"msg": "namespace 'backend' not found in authorizations"}}
}

# ConfigMap present but authorizations.json key is absent -> empty authorizations.
test_denied_when_authz_key_missing if {
    inventory := {
        "namespace": {
            "opa-authorizations": {
                "v1": {
                    "ConfigMap": {
                        "van-authorizations": {"data": {}}
                    }
                }
            }
        }
    }
    result := authz.violation
        with input as review("Connector", "backend", "orders")
        with data.inventory as inventory
    result == {{"msg": "namespace 'backend' not found in authorizations"}}
}

# Invalid JSON in authorizations.json -> empty authorizations.
test_denied_when_authz_json_invalid if {
    inventory := {
        "namespace": {
            "opa-authorizations": {
                "v1": {
                    "ConfigMap": {
                        "van-authorizations": {"data": {"authorizations.json": "not-json"}}
                    }
                }
            }
        }
    }
    result := authz.violation
        with input as review("Connector", "backend", "orders")
        with data.inventory as inventory
    result == {{"msg": "namespace 'backend' not found in authorizations"}}
}

# Empty or absent routingKey is evaluated as an empty string, like the
# controller does. The resource name is not used as a fallback.
test_empty_routing_key_is_denied if {
    input_val := {
        "parameters": mock_params,
        "review": {
            "object": {
                "apiVersion": "skupper.io/v2alpha1",
                "kind": "Connector",
                "metadata": {"name": "orders", "namespace": "backend"},
                "spec": {"routingKey": "", "host": "localhost", "port": 8080}
            }
        }
    }
    result := authz.violation
        with input as input_val
        with data.inventory as mock_inventory
    result == {{"msg": "routing key '' is not permitted for Connector in namespace 'backend'"}}
}

test_absent_routing_key_is_denied if {
    input_val := {
        "parameters": mock_params,
        "review": {
            "object": {
                "apiVersion": "skupper.io/v2alpha1",
                "kind": "Connector",
                "metadata": {"name": "orders", "namespace": "backend"},
                "spec": {"host": "localhost", "port": 8080}
            }
        }
    }
    result := authz.violation
        with input as input_val
        with data.inventory as mock_inventory
    result == {{"msg": "routing key '' is not permitted for Connector in namespace 'backend'"}}
}

# Absent configmap_namespace parameter defaults to "opa-authorizations".
test_default_configmap_namespace if {
    input_val := {
        "parameters": {},
        "review": {
            "object": {
                "apiVersion": "skupper.io/v2alpha1",
                "kind": "Connector",
                "metadata": {"name": "test-resource", "namespace": "backend"},
                "spec": {"routingKey": "orders", "host": "localhost", "port": 8080}
            }
        }
    }
    result := authz.violation
        with input as input_val
        with data.inventory as mock_inventory
    count(result) == 0
}
