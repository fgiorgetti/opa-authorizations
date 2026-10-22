package interconnect.authz_test

import rego.v1

import data.interconnect.authz

mock_authorizations := {
	"backend": {
		"provides": ["payment-*", "orders"],
		"requires": ["inventory", "audit-*"],
	},
	"frontend": {
		"provides": ["ui-service"],
		"requires": ["payment-*", "orders"],
	},
}

test_connector_exact_match_allowed if {
	authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Connector",
			"routing_key": "orders",
		}
	v := authz.violations with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Connector",
			"routing_key": "orders",
		}
	count(v) == 0
}

test_connector_wildcard_match_allowed if {
	authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Connector",
			"routing_key": "payment-v1",
		}
}

test_listener_exact_match_allowed if {
	authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Listener",
			"routing_key": "inventory",
		}
}

test_listener_wildcard_match_allowed if {
	authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "frontend",
			"type": "Listener",
			"routing_key": "payment-v2",
		}
}

test_denied_when_namespace_missing if {
	not authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "unknown-ns",
			"type": "Connector",
			"routing_key": "orders",
		}
	v := authz.violations with data.authorizations as mock_authorizations
		with input as {
			"namespace": "unknown-ns",
			"type": "Connector",
			"routing_key": "orders",
		}
	v == {"namespace 'unknown-ns' not found in authorizations"}
}

test_denied_when_routing_key_not_in_list if {
	not authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Connector",
			"routing_key": "unauthorized-key",
		}
	v := authz.violations with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Connector",
			"routing_key": "unauthorized-key",
		}
	v == {"routing key 'unauthorized-key' is not permitted for Connector in namespace 'backend'"}
}

test_denied_when_listener_uses_provides_key if {
	not authz.allowed with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Listener",
			"routing_key": "orders",
		}
	v := authz.violations with data.authorizations as mock_authorizations
		with input as {
			"namespace": "backend",
			"type": "Listener",
			"routing_key": "orders",
		}
	v == {"routing key 'orders' is not permitted for Listener in namespace 'backend'"}
}
