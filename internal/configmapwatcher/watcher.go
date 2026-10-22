// Package configmapwatcher watches the van-authorizations ConfigMap and pushes
// updated authorization data into the Validator whenever the ConfigMap changes.
package configmapwatcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/interconnect/opa-controller/internal/validator"
)

const configMapName = "van-authorizations"
const dataKey = "authorizations.json"

// Watcher watches the van-authorizations ConfigMap and calls UpdateData on change.
type Watcher struct {
	client    kubernetes.Interface
	namespace string
	validator *validator.Validator
	onUpdate  func()
}

// New creates a Watcher.
func New(client kubernetes.Interface, namespace string, v *validator.Validator, onUpdate ...func()) *Watcher {
	var cb func()
	if len(onUpdate) > 0 {
		cb = onUpdate[0]
	}
	return &Watcher{
		client:    client,
		namespace: namespace,
		validator: v,
		onUpdate:  cb,
	}
}

// Run starts the informer and blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	// Build the ListerWatcher using typed client methods so it works with both
	// real and fake kubernetes.Interface implementations.
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = fields.OneTermEqualSelector(
				"metadata.name", configMapName,
			).String()
			return w.client.CoreV1().ConfigMaps(w.namespace).List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = fields.OneTermEqualSelector(
				"metadata.name", configMapName,
			).String()
			return w.client.CoreV1().ConfigMaps(w.namespace).Watch(ctx, opts)
		},
	}

	w.runWithListerWatcher(ctx, lw)
}

// runWithListerWatcher is the testable core: it accepts any ListerWatcher.
func (w *Watcher) runWithListerWatcher(ctx context.Context, lw cache.ListerWatcher) {
	_, informer := cache.NewInformer(
		lw,
		&corev1.ConfigMap{},
		time.Duration(0),
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) { w.handle(obj) },
			UpdateFunc: func(_, obj interface{}) { w.handle(obj) },
			DeleteFunc: func(obj interface{}) {
				cm, ok := obj.(*corev1.ConfigMap)
				name := configMapName
				if ok {
					name = cm.Name
				}
				slog.Warn("van-authorizations ConfigMap deleted; retaining previous authorization data",
					"name", name, "namespace", w.namespace)
			},
		},
	)

	informer.Run(ctx.Done())
}

func (w *Watcher) handle(obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		// Tolerate DeletedFinalStateUnknown tombstones that the informer may produce.
		if tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown); ok2 {
			cm, ok = tombstone.Obj.(*corev1.ConfigMap)
		}
		if !ok {
			slog.Error("configmapwatcher: unexpected object type", "type", obj)
			return
		}
	}

	raw, exists := cm.Data[dataKey]
	if !exists {
		slog.Warn("van-authorizations ConfigMap has no authorizations.json key; skipping update",
			"name", cm.Name, "namespace", cm.Namespace)
		return
	}

	var authz map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &authz); err != nil {
		slog.Error("van-authorizations: failed to parse authorizations.json; retaining previous data",
			"error", err)
		return
	}

	w.validator.UpdateData(authz)
	slog.Info("authorization data updated from ConfigMap",
		"name", cm.Name, "namespace", cm.Namespace)
	if w.onUpdate != nil {
		w.onUpdate()
	}
}
