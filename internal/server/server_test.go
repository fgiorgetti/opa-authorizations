package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/interconnect/opa-controller/internal/cache"
)

const testNamespace = "skupper"

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
	srv := New(newPopulatedCache(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

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
	srv := New(cache.New(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

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
	srv := New(newPopulatedCache(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Navigation links present.
	if !strings.Contains(body, `href="/"`) || !strings.Contains(body, `href="/authorizations"`) {
		t.Error("expected navigation links to / and /authorizations")
	}

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

func doAuthzRequest(srv *http.Server, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/authorizations", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	return w
}

func TestGetAuthorizations_MissingConfigMap(t *testing.T) {
	srv := New(cache.New(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

	w := doAuthzRequest(srv, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "{}" {
		t.Fatalf("expected {}, got %q", body)
	}
}

func TestGetAuthorizations_MissingKey(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "van-authorizations", Namespace: testNamespace},
		Data:       map[string]string{"other": "x"},
	})
	srv := New(cache.New(), client, testNamespace, "127.0.0.1:0")

	w := doAuthzRequest(srv, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "{}" {
		t.Fatalf("expected {}, got %q", body)
	}
}

func TestGetAuthorizations_Existing(t *testing.T) {
	raw := `{"ns1":{"provides":["a"],"requires":["b"]}}`
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "van-authorizations", Namespace: testNamespace},
		Data:       map[string]string{"authorizations.json": raw},
	})
	srv := New(cache.New(), client, testNamespace, "127.0.0.1:0")

	w := doAuthzRequest(srv, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got AuthorizationsData
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(got["ns1"].Provides) != 1 || got["ns1"].Provides[0] != "a" {
		t.Errorf("unexpected provides: %v", got["ns1"].Provides)
	}
	if len(got["ns1"].Requires) != 1 || got["ns1"].Requires[0] != "b" {
		t.Errorf("unexpected requires: %v", got["ns1"].Requires)
	}
}

func TestPutAuthorizations_CreatesConfigMap(t *testing.T) {
	client := fake.NewSimpleClientset()
	srv := New(cache.New(), client, testNamespace, "127.0.0.1:0")

	w := doAuthzRequest(srv, http.MethodPut, `{"ns1":{"provides":["a","b"],"requires":["c"]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"status":"ok"}` {
		t.Fatalf("unexpected body %q", body)
	}

	cm, err := client.CoreV1().ConfigMaps(testNamespace).Get(context.Background(), "van-authorizations", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap not created: %v", err)
	}
	var got AuthorizationsData
	if err := json.Unmarshal([]byte(cm.Data["authorizations.json"]), &got); err != nil {
		t.Fatalf("stored data is not valid JSON: %v", err)
	}
	if len(got["ns1"].Provides) != 2 || len(got["ns1"].Requires) != 1 {
		t.Errorf("unexpected stored data: %+v", got)
	}
	if !strings.Contains(cm.Data["authorizations.json"], "\n  ") {
		t.Errorf("expected indented JSON, got %q", cm.Data["authorizations.json"])
	}
}

func TestPutAuthorizations_UpdatesExistingConfigMap(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "van-authorizations", Namespace: testNamespace},
		Data: map[string]string{
			"authorizations.json": `{"old":{"provides":["x"],"requires":[]}}`,
			"keep":                "me",
		},
	})
	srv := New(cache.New(), client, testNamespace, "127.0.0.1:0")

	w := doAuthzRequest(srv, http.MethodPut, `{"new":{"provides":[],"requires":["y"]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cm, err := client.CoreV1().ConfigMaps(testNamespace).Get(context.Background(), "van-authorizations", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ConfigMap: %v", err)
	}
	var got AuthorizationsData
	if err := json.Unmarshal([]byte(cm.Data["authorizations.json"]), &got); err != nil {
		t.Fatalf("stored data is not valid JSON: %v", err)
	}
	if _, ok := got["old"]; ok {
		t.Errorf("expected old namespace to be replaced")
	}
	if len(got["new"].Requires) != 1 || got["new"].Requires[0] != "y" {
		t.Errorf("unexpected stored data: %+v", got)
	}
	if cm.Data["keep"] != "me" {
		t.Errorf("expected unrelated key to be preserved")
	}
}

func TestPutAuthorizations_NilSlicesStoredAsEmptyArrays(t *testing.T) {
	client := fake.NewSimpleClientset()
	srv := New(cache.New(), client, testNamespace, "127.0.0.1:0")

	w := doAuthzRequest(srv, http.MethodPut, `{"ns1":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	cm, _ := client.CoreV1().ConfigMaps(testNamespace).Get(context.Background(), "van-authorizations", metav1.GetOptions{})
	if strings.Contains(cm.Data["authorizations.json"], "null") {
		t.Errorf("expected no null values, got %q", cm.Data["authorizations.json"])
	}
}

func TestPutAuthorizations_BadRequests(t *testing.T) {
	cases := map[string]string{
		"invalid json":         `{not json`,
		"empty body":           ``,
		"not an object":        `[1,2]`,
		"null":                 `null`,
		"empty namespace":      `{"":{"provides":[],"requires":[]}}`,
		"blank namespace":      `{"  ":{"provides":[],"requires":[]}}`,
		"provides not array":   `{"ns":{"provides":"a","requires":[]}}`,
		"requires not strings": `{"ns":{"provides":[],"requires":[1]}}`,
		"unknown field":        `{"ns":{"provides":[],"requires":[],"extra":1}}`,
		"trailing data":        `{"ns":{"provides":[],"requires":[]}} {}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			srv := New(cache.New(), client, testNamespace, "127.0.0.1:0")

			w := doAuthzRequest(srv, http.MethodPut, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
			cms, _ := client.CoreV1().ConfigMaps(testNamespace).List(context.Background(), metav1.ListOptions{})
			if len(cms.Items) != 0 {
				t.Errorf("expected no ConfigMap to be created")
			}
		})
	}
}

func TestAuthorizationsPage_StatusAndContent(t *testing.T) {
	srv := New(cache.New(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/authorizations", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html Content-Type, got %q", ct)
	}

	body := w.Body.String()

	// Header and navigation
	if !strings.Contains(body, "OPA Controller — Authorizations") {
		t.Error("expected page title / heading OPA Controller — Authorizations")
	}
	if !strings.Contains(body, `href="/"`) || !strings.Contains(body, `href="/authorizations"`) {
		t.Error("expected navigation links to / and /authorizations")
	}

	// Form elements
	for _, expected := range []string{
		"new-namespace",
		"new-provides",
		"new-requires",
		"Save Authorizations",
		"Namespace",
		"Provides",
		"Requires",
		"/api/authorizations",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("expected authorizations page to contain %q", expected)
		}
	}
}

func fetchBundle(t *testing.T) []byte {
	t.Helper()
	srv := New(cache.New(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/api/bundle", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("expected application/gzip, got %q", ct)
	}
	return w.Body.Bytes()
}

func TestAPIBundle_Content(t *testing.T) {
	b, err := bundle.NewReader(bytes.NewReader(fetchBundle(t))).Read()
	if err != nil {
		t.Fatalf("response is not a valid bundle: %v", err)
	}

	if b.Manifest.Roots == nil || len(*b.Manifest.Roots) != 1 || (*b.Manifest.Roots)[0] != "interconnect" {
		t.Errorf("unexpected manifest roots: %v", b.Manifest.Roots)
	}
	if len(b.Data) != 0 {
		t.Errorf("expected no data in bundle, got %v", b.Data)
	}

	var found bool
	for _, m := range b.Modules {
		if strings.HasSuffix(m.Path, "_test.rego") {
			t.Errorf("test module %q must not be bundled", m.Path)
		}
		if strings.HasSuffix(m.Path, "authz.rego") {
			found = true
		}
	}
	if !found {
		t.Error("expected authz.rego in the bundle")
	}
}

func TestAPIBundle_EvaluatesWithConsumerData(t *testing.T) {
	b, err := bundle.NewReader(bytes.NewReader(fetchBundle(t))).Read()
	if err != nil {
		t.Fatalf("response is not a valid bundle: %v", err)
	}

	store := inmem.NewFromObject(map[string]any{
		"authorizations": map[string]any{
			"ns1": map[string]any{
				"provides": []any{"svc.*"},
				"requires": []any{"db"},
			},
		},
	})

	cases := map[string]struct {
		input   map[string]any
		allowed bool
	}{
		"connector permitted": {map[string]any{"namespace": "ns1", "type": "Connector", "routing_key": "svc.a"}, true},
		"listener permitted":  {map[string]any{"namespace": "ns1", "type": "Listener", "routing_key": "db"}, true},
		"key not permitted":   {map[string]any{"namespace": "ns1", "type": "Connector", "routing_key": "db"}, false},
		"unknown namespace":   {map[string]any{"namespace": "other", "type": "Connector", "routing_key": "svc.a"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := []func(*rego.Rego){
				rego.Query("data.interconnect.authz.allowed"),
				rego.Store(store),
			}
			for _, m := range b.Modules {
				opts = append(opts, rego.Module(m.Path, string(m.Raw)))
			}
			pq, err := rego.New(opts...).PrepareForEval(context.Background())
			if err != nil {
				t.Fatalf("prepare failed: %v", err)
			}
			rs, err := pq.Eval(context.Background(), rego.EvalInput(tc.input))
			if err != nil {
				t.Fatalf("eval failed: %v", err)
			}
			if len(rs) != 1 || len(rs[0].Expressions) != 1 {
				t.Fatalf("unexpected result: %v", rs)
			}
			if got, _ := rs[0].Expressions[0].Value.(bool); got != tc.allowed {
				t.Errorf("expected allowed=%v, got %v", tc.allowed, got)
			}
		})
	}
}

func TestAPIBundle_MethodNotAllowed(t *testing.T) {
	srv := New(cache.New(), fake.NewSimpleClientset(), testNamespace, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodPost, "/api/bundle", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
