package skupper

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add to scheme: %v", err)
	}

	listenerGK := schema.GroupKind{Group: "skupper.io", Kind: "Listener"}
	if !scheme.Recognizes(SchemeGroupVersion.WithKind("Listener")) {
		t.Errorf("scheme does not recognize Listener: %v", listenerGK)
	}

	connectorGK := schema.GroupKind{Group: "skupper.io", Kind: "Connector"}
	if !scheme.Recognizes(SchemeGroupVersion.WithKind("Connector")) {
		t.Errorf("scheme does not recognize Connector: %v", connectorGK)
	}
}

func TestDeepCopy(t *testing.T) {
	listener := &Listener{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "skupper.io/v2alpha1",
			Kind:       "Listener",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-listener",
			Namespace: "test-ns",
		},
		Spec: ListenerSpec{
			RoutingKey: "my-key",
			Host:       "localhost",
			Port:       8080,
		},
	}

	copiedObj := listener.DeepCopyObject()
	copiedListener, ok := copiedObj.(*Listener)
	if !ok {
		t.Fatalf("expected *Listener, got %T", copiedObj)
	}
	if copiedListener.Spec.RoutingKey != "my-key" {
		t.Errorf("expected routingKey 'my-key', got '%s'", copiedListener.Spec.RoutingKey)
	}

	connector := &Connector{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "skupper.io/v2alpha1",
			Kind:       "Connector",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-connector",
			Namespace: "test-ns",
		},
		Spec: ConnectorSpec{
			RoutingKey: "my-key",
			Host:       "localhost",
			Port:       8080,
		},
	}

	copiedConnectorObj := connector.DeepCopyObject()
	copiedConnector, ok := copiedConnectorObj.(*Connector)
	if !ok {
		t.Fatalf("expected *Connector, got %T", copiedConnectorObj)
	}
	if copiedConnector.Spec.RoutingKey != "my-key" {
		t.Errorf("expected routingKey 'my-key', got '%s'", copiedConnector.Spec.RoutingKey)
	}
}
