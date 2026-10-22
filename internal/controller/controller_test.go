package controller_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"

	"github.com/interconnect/opa-controller/internal/cache"
	"github.com/interconnect/opa-controller/internal/controller"
	"github.com/interconnect/opa-controller/internal/skupper"
	"github.com/interconnect/opa-controller/internal/validator"
)

func makeUnstructured(gvr schema.GroupVersionResource, kind, namespace, name, routingKey string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"routingKey": routingKey,
			},
		},
	}
}

// waitFor polls until pred returns true or the deadline passes.
func waitFor(t *testing.T, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func TestController_Listener_AddAndDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := skupper.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	fakeClient := fake.NewSimpleDynamicClient(scheme)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	// Authorize "allowed-key" for Listeners in "test-ns"
	v.UpdateData(map[string]interface{}{
		"test-ns": map[string]interface{}{
			"requires": []interface{}{"allowed-key"},
		},
	})

	c := cache.New()
	ctrl := controller.New(fakeClient, v, c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ctrl.Run(ctx)

	// Create a Listener resource
	listener := makeUnstructured(skupper.ListenerGVR, "Listener", "test-ns", "my-listener", "allowed-key")
	_, err = fakeClient.Resource(skupper.ListenerGVR).Namespace("test-ns").Create(ctx, listener, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	// Verify cache is populated
	ok := waitFor(t, func() bool {
		entries := c.GetAll()
		for _, e := range entries {
			if e.Namespace == "test-ns" && e.Name == "my-listener" {
				return e.Type == "Listener" && e.Allowed && len(e.Violations) == 0
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("timed out waiting for listener to be added to cache; cache entries: %+v", c.GetAll())
	}

	// Delete the Listener resource
	err = fakeClient.Resource(skupper.ListenerGVR).Namespace("test-ns").Delete(ctx, "my-listener", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to delete listener: %v", err)
	}

	// Verify cache entry is removed
	ok = waitFor(t, func() bool {
		entries := c.GetAll()
		for _, e := range entries {
			if e.Namespace == "test-ns" && e.Name == "my-listener" {
				return false
			}
		}
		return true
	})
	if !ok {
		t.Fatalf("timed out waiting for listener to be deleted from cache; cache entries: %+v", c.GetAll())
	}
}

func TestController_Connector_AddAndUpdateAndDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := skupper.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	fakeClient := fake.NewSimpleDynamicClient(scheme)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	v.UpdateData(map[string]interface{}{
		"prod-ns": map[string]interface{}{
			"provides": []interface{}{"service-a"},
		},
	})

	c := cache.New()
	ctrl := controller.New(fakeClient, v, c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ctrl.Run(ctx)

	// Create a Connector with unpermitted routing key
	connector := makeUnstructured(skupper.ConnectorGVR, "Connector", "prod-ns", "my-connector", "unauthorized-key")
	_, err = fakeClient.Resource(skupper.ConnectorGVR).Namespace("prod-ns").Create(ctx, connector, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	// Verify cache has entry with Allowed=false
	ok := waitFor(t, func() bool {
		entries := c.GetAll()
		for _, e := range entries {
			if e.Namespace == "prod-ns" && e.Name == "my-connector" {
				return e.Type == "Connector" && !e.Allowed && len(e.Violations) > 0
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("timed out waiting for unauthorized connector in cache; cache entries: %+v", c.GetAll())
	}

	// Update the Connector with permitted routing key
	connector.Object["spec"] = map[string]interface{}{
		"routingKey": "service-a",
	}
	_, err = fakeClient.Resource(skupper.ConnectorGVR).Namespace("prod-ns").Update(ctx, connector, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update connector: %v", err)
	}

	// Verify cache is updated to Allowed=true
	ok = waitFor(t, func() bool {
		entries := c.GetAll()
		for _, e := range entries {
			if e.Namespace == "prod-ns" && e.Name == "my-connector" {
				return e.Allowed && len(e.Violations) == 0
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("timed out waiting for updated connector in cache; cache entries: %+v", c.GetAll())
	}

	// Delete the Connector
	err = fakeClient.Resource(skupper.ConnectorGVR).Namespace("prod-ns").Delete(ctx, "my-connector", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to delete connector: %v", err)
	}

	// Verify cache is cleared
	ok = waitFor(t, func() bool {
		entries := c.GetAll()
		return len(entries) == 0
	})
	if !ok {
		t.Fatalf("timed out waiting for connector to be deleted; cache entries: %+v", c.GetAll())
	}
}

func findEntry(c *cache.Cache, namespace, name string) (cache.EndpointResult, bool) {
	for _, e := range c.GetAll() {
		if e.Namespace == namespace && e.Name == name {
			return e, true
		}
	}
	return cache.EndpointResult{}, false
}

func TestController_ReevaluateAll_BeforeRun(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := skupper.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	c := cache.New()
	ctrl := controller.New(fake.NewSimpleDynamicClient(scheme), v, c)

	// Must not panic when informer stores are not yet initialised
	ctrl.ReevaluateAll()

	if n := len(c.GetAll()); n != 0 {
		t.Fatalf("expected empty cache, got %d entries", n)
	}
}

func TestController_ReevaluateAll_UpdatesCacheOnValidatorChange(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := skupper.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	fakeClient := fake.NewSimpleDynamicClient(scheme)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	// Initially nothing is authorized
	v.UpdateData(map[string]interface{}{})

	c := cache.New()
	ctrl := controller.New(fakeClient, v, c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ctrl.Run(ctx)

	listener := makeUnstructured(skupper.ListenerGVR, "Listener", "test-ns", "my-listener", "key-l")
	if _, err = fakeClient.Resource(skupper.ListenerGVR).Namespace("test-ns").Create(ctx, listener, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	connector := makeUnstructured(skupper.ConnectorGVR, "Connector", "test-ns", "my-connector", "key-c")
	if _, err = fakeClient.Resource(skupper.ConnectorGVR).Namespace("test-ns").Create(ctx, connector, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	// Both are denied initially
	ok := waitFor(t, func() bool {
		l, lok := findEntry(c, "test-ns", "my-listener")
		cn, cok := findEntry(c, "test-ns", "my-connector")
		return lok && cok && !l.Allowed && !cn.Allowed
	})
	if !ok {
		t.Fatalf("timed out waiting for denied entries; cache entries: %+v", c.GetAll())
	}

	// Authorize both keys; cache must not change until ReevaluateAll is called
	v.UpdateData(map[string]interface{}{
		"test-ns": map[string]interface{}{
			"requires": []interface{}{"key-l"},
			"provides": []interface{}{"key-c"},
		},
	})
	if l, _ := findEntry(c, "test-ns", "my-listener"); l.Allowed {
		t.Fatalf("listener cache entry changed before ReevaluateAll")
	}

	ctrl.ReevaluateAll()

	l, lok := findEntry(c, "test-ns", "my-listener")
	cn, cok := findEntry(c, "test-ns", "my-connector")
	if !lok || !cok {
		t.Fatalf("expected both entries in cache after ReevaluateAll; cache entries: %+v", c.GetAll())
	}
	if l.Type != "Listener" || !l.Allowed || len(l.Violations) != 0 {
		t.Errorf("listener not re-evaluated as allowed: %+v", l)
	}
	if cn.Type != "Connector" || !cn.Allowed || len(cn.Violations) != 0 {
		t.Errorf("connector not re-evaluated as allowed: %+v", cn)
	}

	// Revoke authorization and re-evaluate
	v.UpdateData(map[string]interface{}{})
	ctrl.ReevaluateAll()

	l, _ = findEntry(c, "test-ns", "my-listener")
	cn, _ = findEntry(c, "test-ns", "my-connector")
	if l.Allowed || len(l.Violations) == 0 {
		t.Errorf("listener should be denied with violations after revoke: %+v", l)
	}
	if cn.Allowed || len(cn.Violations) == 0 {
		t.Errorf("connector should be denied with violations after revoke: %+v", cn)
	}
}

func TestController_ReevaluateAll_SkipsDeletedResources(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := skupper.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	fakeClient := fake.NewSimpleDynamicClient(scheme)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}
	v.UpdateData(map[string]interface{}{
		"test-ns": map[string]interface{}{
			"requires": []interface{}{"key-l"},
		},
	})

	c := cache.New()
	ctrl := controller.New(fakeClient, v, c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ctrl.Run(ctx)

	listener := makeUnstructured(skupper.ListenerGVR, "Listener", "test-ns", "my-listener", "key-l")
	if _, err = fakeClient.Resource(skupper.ListenerGVR).Namespace("test-ns").Create(ctx, listener, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	ok := waitFor(t, func() bool {
		e, found := findEntry(c, "test-ns", "my-listener")
		return found && e.Allowed
	})
	if !ok {
		t.Fatalf("timed out waiting for listener in cache; cache entries: %+v", c.GetAll())
	}

	if err = fakeClient.Resource(skupper.ListenerGVR).Namespace("test-ns").Delete(ctx, "my-listener", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("failed to delete listener: %v", err)
	}
	ok = waitFor(t, func() bool {
		_, found := findEntry(c, "test-ns", "my-listener")
		return !found
	})
	if !ok {
		t.Fatalf("timed out waiting for listener removal; cache entries: %+v", c.GetAll())
	}

	ctrl.ReevaluateAll()

	if _, found := findEntry(c, "test-ns", "my-listener"); found {
		t.Fatalf("deleted listener was resurrected by ReevaluateAll; cache entries: %+v", c.GetAll())
	}
}
