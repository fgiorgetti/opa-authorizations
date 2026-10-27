# opa-authorizations

A small Kubernetes controller that checks Skupper `Listener` and `Connector`
resources against an authorization policy written in Rego (Open Policy Agent).
It shows the result of each check in a web page and exposes a JSON API. It also
serves its policies as an OPA bundle so other applications can reuse them.

This project is meant for quick evaluation. It reports on resources. It does not
block or modify them.

For a comparison of the ways to use these policies (embedded, REST and bundle,
Gatekeeper), the lessons learned and a recommendation for the Skupper projects,
see [`OPA-USAGE-GUIDE.md`](OPA-USAGE-GUIDE.md).

## How it works

```
 Skupper Listeners / Connectors (all namespaces)
                 |
                 v
        +-----------------+      van-authorizations ConfigMap
        |   controller    | <--- (authorizations.json)
        |  (OPA evaluate) |
        +-----------------+
                 |
                 v
          in-memory cache ---> HTTP server (:8080)
                                 /  /authorizations
                                 /api/endpoints
                                 /api/authorizations
                                 /api/bundle
```

1. The controller watches `Listener` and `Connector` resources of the
   `skupper.io/v2alpha1` API in every namespace.
2. For each resource it builds an input document with the resource namespace,
   its type (`Listener` or `Connector`) and its `spec.routingKey`.
3. The input is evaluated against the embedded policy
   [`policies/authz.rego`](policies/authz.rego). The result is `allowed` plus a
   list of `violations`, and it is stored in an in-memory cache.
4. The authorization data comes from a ConfigMap named `van-authorizations`
   in the controller namespace, under the key `authorizations.json`. A watcher
   loads it into OPA whenever it changes, and all resources are evaluated again.
5. The HTTP server shows the cached results and lets you edit the authorization
   data.

### Authorization data

The data maps a namespace to the routing keys it may provide and require:

```json
{
  "team-a": {
    "provides": ["orders", "payments-*"],
    "requires": ["inventory"]
  },
  "team-b": {
    "provides": ["inventory"],
    "requires": ["orders"]
  }
}
```

- A `Connector` offers a service, so its routing key is checked against
  `provides`.
- A `Listener` consumes a service, so its routing key is checked against
  `requires`.
- Each entry is a glob pattern (`glob.match` with no delimiters), so `payments-*`
  matches `payments-eu`.
- A resource is allowed only when its namespace is present in the data and its
  routing key matches a pattern. Otherwise it is denied, with one of these
  violations:
  - `namespace '<ns>' not found in authorizations`
  - `routing key '<key>' is not permitted for <type> in namespace '<ns>'`

Until the ConfigMap exists, the data is empty and every resource is denied. If
the ConfigMap is deleted, the controller keeps the last data it loaded. If its
content is not valid JSON, the update is skipped and the previous data is kept.

### HTTP endpoints

| Method | Path                  | Description                                                    |
|--------|-----------------------|----------------------------------------------------------------|
| GET    | `/`                   | HTML table of all evaluated Listeners and Connectors.          |
| GET    | `/authorizations`     | HTML page to edit the authorization data.                      |
| GET    | `/api/endpoints`      | Evaluation results as JSON.                                    |
| GET    | `/api/authorizations` | Current content of `authorizations.json` (`{}` when missing).  |
| PUT    | `/api/authorizations` | Replace the data. Creates the ConfigMap if it does not exist.  |
| GET    | `/api/bundle`         | The policies as an OPA bundle (`bundle.tar.gz`).               |
| POST   | `/api/authorize`      | Asks the policy if a namespace, type and routing key is allowed. |

The `PUT` payload must be a JSON object in the shape shown above. Unknown fields,
empty namespace names and trailing data are rejected with `400`. The body is
limited to 1 MiB.

The bundle contains the policies only (test files are excluded) and its
manifest claims the `interconnect` root. It carries no data, so a consumer
supplies its own `data.authorizations` in the same shape and evaluates
`data.interconnect.authz` with `input.namespace`, `input.type` and
`input.routing_key`.

#### Authorize request

`POST /api/authorize` evaluates the policy with the current authorization data
and returns the decision. The request body is:

```json
{"namespace": "team-a", "type": "connector", "routing_key": "orders"}
```

- All three fields are required and must be strings. `namespace` and
  `routing_key` must not be blank.
- `type` is `connector` or `listener`, in any letter case.
- Unknown fields, trailing data and bodies over 1 MiB are rejected.

The response is `200` with `Content-Type: application/json`:

```json
{"allowed": false, "violations": ["routing key 'orders' is not permitted for Listener in namespace 'team-a'"]}
```

A denial is a normal `200` response with `allowed` set to `false`.
`violations` is always an array and is empty when the request is allowed.
Invalid requests return `400` and evaluation failures return `500`. Both use a
plain-text body, not JSON.

## Deploy

All Kubernetes objects are in
[`deploy/opa-authorizations.yaml`](deploy/opa-authorizations.yaml).

### Prerequisites

- A Kubernetes or OpenShift cluster and `kubectl` (or `oc`) with cluster-admin
  rights, because the file creates a ClusterRole and a ClusterRoleBinding.
- The Skupper v2 CRDs installed (`listeners.skupper.io` and
  `connectors.skupper.io`, version `v2alpha1`). Without them the watch cannot
  sync.
- A reachable container image. The manifest uses
  `quay.io/fgiorgetti/opa-authorizations:latest`.

### Build and push the image (optional)

Use this if you want to run your own build instead of the default image. The
[`Dockerfile`](Dockerfile) uses Red Hat UBI base images, so log in to
`registry.redhat.io` first.

```bash
podman login registry.redhat.io
podman build -t quay.io/<your-user>/opa-authorizations:latest .
podman push quay.io/<your-user>/opa-authorizations:latest
```

Then change the `image` field in `deploy/opa-authorizations.yaml` to your image.

### Apply the manifest

```bash
kubectl apply -f deploy/opa-authorizations.yaml
kubectl -n opa-authorizations rollout status deployment/opa-authorizations
```

### What the manifest creates

| Object                         | Purpose                                                                                          |
|--------------------------------|--------------------------------------------------------------------------------------------------|
| `Namespace`                    | `opa-authorizations`, where everything runs.                                                     |
| `ServiceAccount`               | Identity of the controller pod.                                                                  |
| `ClusterRole` and binding      | Read-only `get`, `list`, `watch` on `listeners` and `connectors` in `skupper.io`, in all namespaces. |
| `Role` and `RoleBinding`       | Access to the `van-authorizations` ConfigMap in its own namespace (`get`, `list`, `watch`, `update`, plus `create`). |
| `Deployment`                   | One replica. Runs as non-root user 1001, read-only root filesystem, all capabilities dropped, default seccomp profile. The pod namespace is passed in `POD_NAMESPACE`. |
| `Service`                      | Type `LoadBalancer`, port 8080.                                                                  |

The ConfigMap is not part of the manifest. It is created the first time you save
data through the UI or the API.

### Access the UI

If your cluster has no load balancer, use a port-forward:

```bash
kubectl -n opa-authorizations port-forward svc/opa-authorizations 8080:8080
```

Otherwise read the external address of the Service:

```bash
kubectl -n opa-authorizations get svc opa-authorizations
```

Open `http://localhost:8080/authorizations` to define the authorizations, then
`http://localhost:8080/` to see the results.

### Try it

Set the authorization data through the API:

```bash
curl -X PUT http://localhost:8080/api/authorizations \
  -H 'Content-Type: application/json' \
  -d '{"team-a": {"provides": ["orders"], "requires": ["inventory"]}}'
```

Create a connector in a namespace named `team-a`:

```bash
kubectl -n team-a apply -f - <<EOF
apiVersion: skupper.io/v2alpha1
kind: Connector
metadata:
  name: orders
spec:
  routingKey: orders
  port: 8080
  selector: app=orders
EOF
```

Check the result:

```bash
curl -s http://localhost:8080/api/endpoints
```

Change `routingKey` to a value that is not in `provides` and the entry turns
to `Allowed: false` with a violation message.

### Python sample

[`samples/python/check_authorization.py`](samples/python/check_authorization.py)
asks for a namespace, an endpoint type (`connector` or `listener`) and a routing
key, then calls `POST /api/authorize` and prints `ALLOWED` or `DENIED` with the
violations. It needs Python 3 and no extra packages.

With the port-forward from the previous section running:

```bash
python3 samples/python/check_authorization.py
```

The controller address is read from `OPA_AUTHZ_URL` (default
`http://127.0.0.1:8080`). Only `http` and `https` URLs are accepted:

```bash
OPA_AUTHZ_URL=https://opa-authorizations.example.com python3 samples/python/check_authorization.py
```

The exit code is `0` when allowed, `3` when denied, `1` when the controller
cannot be reached or answers with an error, and `2` when `OPA_AUTHZ_URL` is
invalid.

Run the sample tests with:

```bash
cd samples/python && python3 -m unittest
```

### Gatekeeper sample

[`samples/gatekeeper`](samples/gatekeeper/README.md) uses OPA Gatekeeper as an
admission controller to reject `Listener` and `Connector` resources that the
policy does not allow. It reads the same `van-authorizations` ConfigMap and
generates its Gatekeeper library from [`policies/authz.rego`](policies/authz.rego),
so the decisions and messages match the controller. Scripts install and remove
Gatekeeper and the policies.

### OPA standalone sample

[`samples/opa`](samples/opa/README.md) shows how to use the OPA binary directly
without deploying the controller. Three scripts cover the full workflow: start
the OPA server with the bundled policies, upload the authorization data from the
`van-authorizations` ConfigMap, and interactively validate a namespace, type and
routing key against the policy.

### Remove

```bash
kubectl delete -f deploy/opa-authorizations.yaml
```

The `van-authorizations` ConfigMap lives in the `opa-authorizations` namespace, so
deleting the namespace removes it too.

### Evaluation limits

- The HTTP API has no authentication or TLS, and `PUT /api/authorizations`
  changes the policy data. `POST /api/authorize` has the same limit: anyone who
  can reach the Service can query the decisions. The Service is a
  `LoadBalancer`, so the API may be
  reachable outside the cluster. For anything beyond a short evaluation, change
  the Service to `ClusterIP` and put an authenticating proxy or ingress with TLS
  in front of it.
- The image tag is `latest` with `imagePullPolicy: Always`. Pin a version for
  repeatable deployments.
- Results are kept in memory and rebuilt at startup, so run a single replica.
- The listen address is `0.0.0.0:8080` inside the container. Change it with the
  `HTTP_ADDR` environment variable or the `--addr` flag.

## Development

Requires Go 1.26 or later.

```bash
go build ./...
go vet ./...
go test ./...
go run ./cmd/controller --namespace opa-authorizations --addr 127.0.0.1:8080
```

Outside a cluster, the controller falls back to `~/.kube/config`. The policy
tests in [`policies/authz_test.rego`](policies/authz_test.rego) can also be run
with `opa test policies/`, if the OPA CLI is installed.

### Layout

| Path                         | Content                                                          |
|------------------------------|------------------------------------------------------------------|
| `cmd/controller`             | Entry point. Wires the components and handles shutdown.          |
| `internal/controller`        | Informers for Listeners and Connectors, evaluation on change.    |
| `internal/configmapwatcher`  | Watches `van-authorizations` and loads the data into OPA.        |
| `internal/validator`         | Compiles the embedded policies and evaluates inputs.             |
| `internal/cache`             | Thread-safe store of evaluation results.                         |
| `internal/server`            | HTTP handlers, HTML pages and the bundle builder.                |
| `internal/skupper`           | Skupper `v2alpha1` types and resource definitions.               |
| `policies`                   | Rego policy and its tests. Embedded by [`embed.go`](embed.go).   |
| `samples/python`             | Python client for `POST /api/authorize` and its tests.           |
| `samples/gatekeeper`         | Gatekeeper admission policy, adapter, tests and install scripts. |
| `deploy`                     | Kubernetes manifest.                                             |
