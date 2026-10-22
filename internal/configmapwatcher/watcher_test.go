package configmapwatcher_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/interconnect/opa-controller/internal/configmapwatcher"
	"github.com/interconnect/opa-controller/internal/validator"
)

const testNamespace = "default"

func makeConfigMap(name, namespace, jsonData string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"authorizations.json": jsonData,
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

// TestWatcher_UpdateData_CalledOnAdd verifies that when a van-authorizations
// ConfigMap is present at startup the Validator's data store is updated, and
// a subsequent Evaluate reflects the new data.
func TestWatcher_UpdateData_CalledOnAdd(t *testing.T) {
	authzJSON := `{"ns-a":{"provides":["svc-1"],"requires":["svc-1"]}}`

	fakeClient := fake.NewSimpleClientset(
		makeConfigMap("van-authorizations", testNamespace, authzJSON),
	)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	w := configmapwatcher.New(fakeClient, testNamespace, v)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Wait until the authorization data has been loaded.
	ok := waitFor(t, func() bool {
		allowed, _, err := v.Evaluate(map[string]interface{}{
			"namespace":   "ns-a",
			"routing_key": "svc-1",
			"type":        "Connector",
		})
		return err == nil && allowed
	})

	if !ok {
		t.Fatal("timed out waiting for authorization data to be loaded")
	}

	allowed, violations, err := v.Evaluate(map[string]interface{}{
		"namespace":   "ns-a",
		"routing_key": "svc-1",
		"type":        "Connector",
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !allowed {
		t.Errorf("expected allowed=true after valid ConfigMap add; violations: %v", violations)
	}
}

// TestWatcher_InvalidJSON_DoesNotCallUpdateData verifies that a ConfigMap update
// with invalid JSON does not overwrite the previously loaded authorization data.
func TestWatcher_InvalidJSON_DoesNotCallUpdateData(t *testing.T) {
	validJSON := `{"ns-b":{"provides":["svc-2"],"requires":["svc-2"]}}`

	fakeClient := fake.NewSimpleClientset(
		makeConfigMap("van-authorizations", testNamespace, validJSON),
	)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	w := configmapwatcher.New(fakeClient, testNamespace, v)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Wait for valid data to be loaded.
	ok := waitFor(t, func() bool {
		allowed, _, _ := v.Evaluate(map[string]interface{}{
			"namespace":   "ns-b",
			"routing_key": "svc-2",
			"type":        "Connector",
		})
		return allowed
	})
	if !ok {
		t.Fatal("timed out waiting for valid authorization data to be loaded")
	}

	// Update the ConfigMap with malformed JSON.
	cm, err := fakeClient.CoreV1().ConfigMaps(testNamespace).Get(
		context.Background(), "van-authorizations", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("Get ConfigMap: %v", err)
	}
	cm.Data["authorizations.json"] = `{bad json`
	_, err = fakeClient.CoreV1().ConfigMaps(testNamespace).Update(
		context.Background(), cm, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update ConfigMap: %v", err)
	}

	// Give the informer time to process the update event.
	time.Sleep(200 * time.Millisecond)

	// The previous valid data must still be effective.
	allowed, violations, err := v.Evaluate(map[string]interface{}{
		"namespace":   "ns-b",
		"routing_key": "svc-2",
		"type":        "Connector",
	})
	if err != nil {
		t.Fatalf("Evaluate after bad JSON update: %v", err)
	}
	if !allowed {
		t.Errorf("expected previous data to be retained (allowed=true); violations: %v", violations)
	}
}

// TestWatcher_OnUpdateCallback verifies that the onUpdate callback is executed
// when valid authorization data is added or updated, and not executed on invalid updates.
func TestWatcher_OnUpdateCallback(t *testing.T) {
	initialJSON := `{"ns-c":{"provides":["svc-3"],"requires":["svc-3"]}}`

	fakeClient := fake.NewSimpleClientset(
		makeConfigMap("van-authorizations", testNamespace, initialJSON),
	)

	v, err := validator.New()
	if err != nil {
		t.Fatalf("validator.New: %v", err)
	}

	var callbackCount atomic.Int32
	onUpdate := func() {
		callbackCount.Add(1)
	}

	w := configmapwatcher.New(fakeClient, testNamespace, v, onUpdate)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Wait for initial add callback
	ok := waitFor(t, func() bool {
		return callbackCount.Load() == 1
	})
	if !ok {
		t.Fatalf("timed out waiting for callback on initial add, got count=%d", callbackCount.Load())
	}

	// Update with valid JSON
	updatedJSON := `{"ns-c":{"provides":["svc-4"],"requires":["svc-4"]}}`
	cm, err := fakeClient.CoreV1().ConfigMaps(testNamespace).Get(
		context.Background(), "van-authorizations", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("Get ConfigMap: %v", err)
	}
	cm.Data["authorizations.json"] = updatedJSON
	_, err = fakeClient.CoreV1().ConfigMaps(testNamespace).Update(
		context.Background(), cm, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update ConfigMap: %v", err)
	}

	ok = waitFor(t, func() bool {
		return callbackCount.Load() == 2
	})
	if !ok {
		t.Fatalf("timed out waiting for callback on valid update, got count=%d", callbackCount.Load())
	}

	// Update with invalid JSON - callback should NOT be invoked
	cm, err = fakeClient.CoreV1().ConfigMaps(testNamespace).Get(
		context.Background(), "van-authorizations", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("Get ConfigMap: %v", err)
	}
	cm.Data["authorizations.json"] = `{invalid json`
	_, err = fakeClient.CoreV1().ConfigMaps(testNamespace).Update(
		context.Background(), cm, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update ConfigMap: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if count := callbackCount.Load(); count != 2 {
		t.Errorf("expected callback count to remain 2 after invalid JSON update, got %d", count)
	}
}
