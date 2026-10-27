# OPA standalone sample

This sample shows how to run the OPA binary directly, load the authorization
data from the cluster, and then validate an input against the policy — without
deploying the full controller.

## Prerequisites

- [OPA CLI](https://www.openpolicyagent.org/docs/latest/#running-opa) installed
  and on your `PATH`.
- `kubectl` configured and pointing at a cluster where the controller is
  running (needed only for `data-context.sh`).
- `jq` installed (used by `data-context.sh` to extract the JSON from the
  ConfigMap).
- `curl` and `python3` installed.

## Step 1 — Start the OPA server

[`run.sh`](run.sh) starts OPA in server mode and loads the policies from the
`policies/` directory at the root of the repository:

```bash
bash samples/opa/run.sh
```

OPA listens on `http://localhost:8181` by default. Leave this terminal open;
the following steps run in a second terminal.

## Step 2 — Load the authorization data

[`data-context.sh`](data-context.sh) reads the `van-authorizations` ConfigMap
from the `opa-authorizations` namespace and uploads its content to the running
OPA instance:

```bash
bash samples/opa/data-context.sh
```

The script reads `authorizations.json` from the ConfigMap, then PUT-s it to
`/v1/data/authorizations` in OPA. This mirrors exactly what the controller
does at runtime. Run this step again whenever the ConfigMap changes.

Expected output when the upload succeeds is an empty JSON object (`{}`).

## Step 3 — Validate an input

[`validate.sh`](validate.sh) prompts for the three fields that make up a policy
input, then sends a POST request to OPA and prints the JSON response:

```bash
bash samples/opa/validate.sh
```

The script asks for:

| Prompt | Accepted values |
|---|---|
| Namespace | any non-empty string, e.g. `team-a` |
| Type | exactly `Listener` or `Connector` (case-sensitive) |
| Routing key | any non-empty string, e.g. `orders` |

If an invalid type is entered the prompt repeats until a valid value is given.

Example session:

```
Namespace: team-a
Type (Listener or Connector): Connector
Routing key: orders

{
    "result": {
        "allowed": true,
        "violations": []
    }
}
```

A denied request returns `allowed: false` with one or more violation messages:

```
{
    "result": {
        "allowed": false,
        "violations": [
            "routing key 'unknown' is not permitted for Connector in namespace 'team-a'"
        ]
    }
}
```
