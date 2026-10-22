package skupper

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// GroupName is the API group for Skupper resources.
	GroupName = "skupper.io"

	// GroupVersion is the version for Skupper resources.
	GroupVersion = "v2alpha1"

	// ListenerResource is the plural resource name for Listener.
	ListenerResource = "listeners"

	// ConnectorResource is the plural resource name for Connector.
	ConnectorResource = "connectors"
)

// SchemeGroupVersion is the GroupVersion for Skupper resources.
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}

// GroupVersionResource definitions for Skupper resources.
var (
	ListenerGVR  = SchemeGroupVersion.WithResource(ListenerResource)
	ConnectorGVR = SchemeGroupVersion.WithResource(ConnectorResource)
)

var (
	// SchemeBuilder collects functions that add things to a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme applies all the stored functions to the scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// addKnownTypes adds the set of types defined in this package to the supplied scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Listener{},
		&ListenerList{},
		&Connector{},
		&ConnectorList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
