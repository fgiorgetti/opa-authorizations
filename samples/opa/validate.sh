#!/usr/bin/env bash
set -euo pipefail

read -rp "Namespace: " namespace

while true; do
    read -rp "Type (Listener or Connector): " type
    if [[ "$type" == "Listener" || "$type" == "Connector" ]]; then
        break
    fi
    echo "Invalid type. Please enter exactly 'Listener' or 'Connector'."
done

read -rp "Routing key: " routing_key

payload=$(printf '{"input": {"namespace": "%s", "type": "%s", "routing_key": "%s"}}' \
    "$namespace" "$type" "$routing_key")

echo ""
curl -s -X POST http://localhost:8181/v1/data/interconnect/authz \
    -H "Content-Type: application/json" \
    -d "$payload" | python3 -m json.tool
