package lib.interconnect.authz

import future.keywords

default allowed := false

# Get permitted routing key patterns for the given resource type
permitted_keys := input.authorizations[input.namespace].provides if {
	input.type == "Connector"
}

permitted_keys := input.authorizations[input.namespace].requires if {
	input.type == "Listener"
}

# Check if the input routing key matches any permitted pattern (exact or glob)
routing_key_permitted if {
	some pattern in permitted_keys
	glob.match(pattern, [], input.routing_key)
}

# Allow when namespace exists in authorizations and routing key is permitted
allowed if {
	input.authorizations[input.namespace]
	routing_key_permitted
}

# Violations when namespace is not found in authorizations
violations contains msg if {
	not input.authorizations[input.namespace]
	msg := sprintf("namespace '%v' not found in authorizations", [input.namespace])
}

# Violations when namespace exists but routing key is not permitted for the resource type
violations contains msg if {
	input.authorizations[input.namespace]
	not routing_key_permitted
	msg := sprintf("routing key '%v' is not permitted for %v in namespace '%v'", [input.routing_key, input.type, input.namespace])
}
