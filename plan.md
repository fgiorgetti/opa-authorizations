We need to create a Go application. This go application will work as a Kubernetes controller, using the client-go module v0.33.0.
As a Kubernetes controller, it must watch for Listener and Connector resources. These resources are defined by the Skupper (https://skupper.io) controller.

Everytime a change is detected on a Listener or Connector, the controller will get the respective resource and use it as the input for a local set of OPA Policies (.rego files) embedded into the code through //go:embed.

These policies will be validated using a local Validator implementation that uses the embedded policies along with authorization data that comes from a ConfigMap running in the same namespace where our controller runs. This ConfigMap will be named "van-authorizations" and it will contain a unique key named "authorizations.json" and its value will be a JSON string that follows the example below:

```json
{
    "<namespace-name>": {
        "requires": ["routingkey-1", "routingkey-2", "sample-*"],
        "provides": ["key-1", "sample-*"]
    },
    "namespace-selector=xyz": {
        "requires": ["backend"],
        "provides": ["frontend"]
    }
}
```

The controller must create a singleton instance of the Validator, which will load the OPA policies (rego files) and the "authorizations.json" from the ConfigMap will be used as a storage "data" section for the OPA validator. A ConfigMap watcher is also needed as we need to replace the stored data everytime the configmap "authorizations.json" changes.

Along with all that, the controller must also serve HTTP with the index page returning the cached data for the Listeners and Connectors (Column name will be Endpoints and a type column will show whether it is a Listener or a Connector). The following column must show a boolean returned from the policy that says whether or not the respective Listener or Connector is allowed and the third and last column must show the violations reported by the OPA Policies.

With that, we know that the OPA policies must set the following data:
* allowed : boolean
* violations : []string

