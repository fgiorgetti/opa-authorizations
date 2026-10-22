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
