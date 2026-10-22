package cache

import (
	"sync"
	"testing"
)

func TestSet_and_GetAll(t *testing.T) {
	c := New()

	c.Set(EndpointResult{Name: "l1", Namespace: "ns-a", Type: "Listener", Allowed: true})
	c.Set(EndpointResult{Name: "c1", Namespace: "ns-a", Type: "Connector", Allowed: false, Violations: []string{"not allowed"}})

	results := c.GetAll()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// GetAll is sorted by key: "ns-a/c1" < "ns-a/l1"
	if results[0].Name != "c1" {
		t.Errorf("expected first entry to be c1, got %s", results[0].Name)
	}
	if results[1].Name != "l1" {
		t.Errorf("expected second entry to be l1, got %s", results[1].Name)
	}
}

func TestSet_overwrite(t *testing.T) {
	c := New()

	c.Set(EndpointResult{Name: "l1", Namespace: "ns-a", Type: "Listener", Allowed: false})
	c.Set(EndpointResult{Name: "l1", Namespace: "ns-a", Type: "Listener", Allowed: true})

	results := c.GetAll()
	if len(results) != 1 {
		t.Fatalf("expected 1 result after overwrite, got %d", len(results))
	}
	if !results[0].Allowed {
		t.Error("expected overwritten entry to have Allowed=true")
	}
}

func TestDelete(t *testing.T) {
	c := New()

	c.Set(EndpointResult{Name: "l1", Namespace: "ns-a", Type: "Listener", Allowed: true})
	c.Set(EndpointResult{Name: "c1", Namespace: "ns-a", Type: "Connector", Allowed: true})

	c.Delete("ns-a", "l1")

	results := c.GetAll()
	if len(results) != 1 {
		t.Fatalf("expected 1 result after delete, got %d", len(results))
	}
	if results[0].Name != "c1" {
		t.Errorf("expected remaining entry to be c1, got %s", results[0].Name)
	}
}

func TestDelete_nonexistent(t *testing.T) {
	c := New()
	// deleting a key that does not exist must not panic
	c.Delete("ns-x", "missing")
}

func TestGetAll_empty(t *testing.T) {
	c := New()
	results := c.GetAll()
	if results == nil {
		t.Error("GetAll on empty cache should return non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New()
	var wg sync.WaitGroup

	// 50 writers
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Set(EndpointResult{
				Name:      "resource",
				Namespace: "ns",
				Type:      "Listener",
				Allowed:   i%2 == 0,
			})
		}(i)
	}

	// 50 readers
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.GetAll()
		}()
	}

	// 10 deleters
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Delete("ns", "resource")
		}()
	}

	wg.Wait()
}
