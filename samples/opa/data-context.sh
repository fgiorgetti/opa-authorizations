data=$(kubectl -n opa-authorizations get cm van-authorizations -o json | jq -r '.data."authorizations.json"')
curl -X PUT http://localhost:8181/v1/data/authorizations \
  --data-binary "${data:?}" \
  -H 'Content-Type: application/json'

