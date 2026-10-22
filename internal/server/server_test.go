package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/interconnect/opa-controller/internal/cache"
)

// newPopulatedCache returns a Cache pre-loaded with two entries.
func newPopulatedCache() *cache.Cache {
	c := cache.New()
	c.Set(cache.EndpointResult{
		Name:       "my-listener",
		Namespace:  "default",
		Type:       "Listener",
		Allowed:    true,
		Violations: nil,
	})
	c.Set(cache.EndpointResult{
		Name:       "my-connector",
		Namespace:  "prod",
		Type:       "Connector",
		Allowed:    false,
		Violations: []string{"namespace not permitted", "routing key denied"},
	})
	return c
}

func TestAPIEndpoints_Populated(t *testing.T) {
	srv := New(newPopulatedCache(), "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}

	var results []cache.EndpointResult
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// GetAll returns entries sorted by key (<namespace>/<name>).
	// "default/my-listener" < "prod/my-connector"
	if results[0].Name != "my-listener" {
		t.Errorf("expected first result to be my-listener, got %q", results[0].Name)
	}
	if results[0].Namespace != "default" {
		t.Errorf("expected namespace default, got %q", results[0].Namespace)
	}
	if !results[0].Allowed {
		t.Errorf("expected my-listener to be allowed")
	}

	if results[1].Name != "my-connector" {
		t.Errorf("expected second result to be my-connector, got %q", results[1].Name)
	}
	if results[1].Allowed {
		t.Errorf("expected my-connector to be denied")
	}
	if len(results[1].Violations) != 2 {
		t.Errorf("expected 2 violations, got %d", len(results[1].Violations))
	}
}

func TestAPIEndpoints_EmptyCache(t *testing.T) {
	srv := New(cache.New(), "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := strings.TrimSpace(w.Body.String())
	// Must serialise as an empty array, not null.
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("expected JSON array, got %q", body)
	}

	var results []cache.EndpointResult
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty array, got %d elements", len(results))
	}
}

func TestRootHandler_StatusAndContent(t *testing.T) {
	srv := New(newPopulatedCache(), "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Table column headers present.
	for _, header := range []string{"Endpoint", "Type", "Allowed", "Violations"} {
		if !strings.Contains(body, header) {
			t.Errorf("expected page to contain %q", header)
		}
	}

	// Both entries rendered.
	if !strings.Contains(body, "default/my-listener") {
		t.Error("expected page to contain default/my-listener")
	}
	if !strings.Contains(body, "prod/my-connector") {
		t.Error("expected page to contain prod/my-connector")
	}

	// Allowed indicator.
	if !strings.Contains(body, "Yes") {
		t.Error("expected page to contain Yes for allowed entry")
	}
	if !strings.Contains(body, "No") {
		t.Error("expected page to contain No for denied entry")
	}

	// Violations comma-joined.
	if !strings.Contains(body, "namespace not permitted, routing key denied") {
		t.Error("expected page to contain comma-joined violations")
	}
}
