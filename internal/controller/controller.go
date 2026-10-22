// Package controller watches Skupper Listener and Connector custom resources,
// evaluates each against OPA policies, and records the results in the cache.
package controller

import (
	"context"
	"log/slog"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	cachepkg "github.com/interconnect/opa-controller/internal/cache"
	"github.com/interconnect/opa-controller/internal/skupper"
	"github.com/interconnect/opa-controller/internal/validator"
)

// Controller watches Listener and Connector resources and updates the evaluation cache.
type Controller struct {
	dynClient dynamic.Interface
	validator *validator.Validator
	cache     *cachepkg.Cache

	mu             sync.RWMutex
	listenerStore  cache.Store
	connectorStore cache.Store
}

// New returns an initialised Controller.
func New(dynClient dynamic.Interface, v *validator.Validator, c *cachepkg.Cache) *Controller {
	return &Controller{
		dynClient: dynClient,
		validator: v,
		cache:     c,
	}
}

// Run starts dynamic informers for Listeners and Connectors, registers event handlers,
// and blocks until ctx is done.
func (c *Controller) Run(ctx context.Context) {
	factory := dynamicinformer.NewDynamicSharedInformerFactory(c.dynClient, 0)

	listenerInformer := factory.ForResource(skupper.ListenerGVR).Informer()
	connectorInformer := factory.ForResource(skupper.ConnectorGVR).Informer()

	c.mu.Lock()
	c.listenerStore = listenerInformer.GetStore()
	c.connectorStore = connectorInformer.GetStore()
	c.mu.Unlock()

	_, _ = listenerInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleAddOrUpdate(obj, "Listener")
		},
		UpdateFunc: func(_, obj interface{}) {
			c.handleAddOrUpdate(obj, "Listener")
		},
		DeleteFunc: func(obj interface{}) {
			c.handleDelete(obj)
		},
	})

	_, _ = connectorInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleAddOrUpdate(obj, "Connector")
		},
		UpdateFunc: func(_, obj interface{}) {
			c.handleAddOrUpdate(obj, "Connector")
		},
		DeleteFunc: func(obj interface{}) {
			c.handleDelete(obj)
		},
	})

	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), listenerInformer.HasSynced, connectorInformer.HasSynced) {
		if ctx.Err() != nil {
			return
		}
		slog.Error("failed to sync informer caches")
		return
	}

	<-ctx.Done()
}

// ReevaluateAll re-runs policy evaluation for every Listener and Connector
// currently held in the informer stores and updates the cache. It is a no-op
// for resource types whose informer has not been started yet.
func (c *Controller) ReevaluateAll() {
	c.mu.RLock()
	listenerStore := c.listenerStore
	connectorStore := c.connectorStore
	c.mu.RUnlock()

	if listenerStore != nil {
		for _, obj := range listenerStore.List() {
			c.handleAddOrUpdate(obj, "Listener")
		}
	}
	if connectorStore != nil {
		for _, obj := range connectorStore.List() {
			c.handleAddOrUpdate(obj, "Connector")
		}
	}
}

func (c *Controller) handleAddOrUpdate(obj interface{}, resType string) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		if tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown); ok2 {
			u, ok = tombstone.Obj.(*unstructured.Unstructured)
		}
		if !ok {
			slog.Error("controller: unexpected object type", "type", obj)
			return
		}
	}

	namespace := u.GetNamespace()
	name := u.GetName()
	routingKey, _, _ := unstructured.NestedString(u.Object, "spec", "routingKey")

	input := map[string]interface{}{
		"namespace":   namespace,
		"routing_key": routingKey,
		"type":        resType,
	}

	allowed, violations, err := c.validator.Evaluate(input)
	if err != nil {
		slog.Error("failed to evaluate policy", "error", err, "namespace", namespace, "name", name, "type", resType)
	}

	c.cache.Set(cachepkg.EndpointResult{
		Name:       name,
		Namespace:  namespace,
		Type:       resType,
		Allowed:    allowed,
		Violations: violations,
	})
	slog.Info("evaluated endpoint", "namespace", namespace, "name", name, "type", resType, "allowed", allowed)
}

func (c *Controller) handleDelete(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		if tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown); ok2 {
			u, ok = tombstone.Obj.(*unstructured.Unstructured)
		}
		if !ok {
			slog.Error("controller: unexpected object type on delete", "type", obj)
			return
		}
	}

	namespace := u.GetNamespace()
	name := u.GetName()
	c.cache.Delete(namespace, name)
	slog.Info("endpoint deleted from cache", "namespace", namespace, "name", name)
}
