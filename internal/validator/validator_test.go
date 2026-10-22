package validator

import (
	"fmt"
	"sync"
	"testing"
)

func fixture() map[string]interface{} {
	return map[string]interface{}{
		"backend": map[string]interface{}{
			"provides": []interface{}{"payment-*", "orders"},
			"requires": []interface{}{"inventory", "audit-*"},
		},
		"frontend": map[string]interface{}{
			"provides": []interface{}{"ui-service"},
			"requires": []interface{}{"payment-*", "orders"},
		},
	}
}

func newValidator(t *testing.T) *Validator {
	t.Helper()
	v, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	v.UpdateData(fixture())
	return v
}

func input(ns, typ, key string) map[string]interface{} {
	return map[string]interface{}{
		"namespace":   ns,
		"type":        typ,
		"routing_key": key,
	}
}

func TestEvaluate(t *testing.T) {
	v := newValidator(t)

	tests := []struct {
		name         string
		input        map[string]interface{}
		wantAllowed  bool
		wantViolates bool
	}{
		{"exact match connector", input("backend", "Connector", "orders"), true, false},
		{"exact match listener", input("backend", "Listener", "inventory"), true, false},
		{"wildcard match connector", input("backend", "Connector", "payment-eu"), true, false},
		{"wildcard match listener", input("frontend", "Listener", "payment-us"), true, false},
		{"namespace missing", input("unknown", "Connector", "orders"), false, true},
		{"routing key not in list", input("backend", "Connector", "secret"), false, true},
		{"key allowed for other type only", input("backend", "Listener", "orders"), false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, violations, err := v.Evaluate(tt.input)
			if err != nil {
				t.Fatalf("Evaluate() error: %v", err)
			}
			if allowed != tt.wantAllowed {
				t.Errorf("allowed = %v, want %v (violations: %v)", allowed, tt.wantAllowed, violations)
			}
			if tt.wantViolates && len(violations) == 0 {
				t.Error("expected violations, got none")
			}
			if !tt.wantViolates && len(violations) != 0 {
				t.Errorf("expected no violations, got %v", violations)
			}
		})
	}
}

func TestUpdateDataChangesDecision(t *testing.T) {
	v := newValidator(t)

	allowed, _, err := v.Evaluate(input("backend", "Connector", "orders"))
	if err != nil || !allowed {
		t.Fatalf("expected allowed before update, got allowed=%v err=%v", allowed, err)
	}

	v.UpdateData(map[string]interface{}{})

	allowed, violations, err := v.Evaluate(input("backend", "Connector", "orders"))
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if allowed || len(violations) == 0 {
		t.Errorf("expected denial after update, got allowed=%v violations=%v", allowed, violations)
	}
}

func TestConcurrentUpdateAndEvaluate(t *testing.T) {
	v := newValidator(t)

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if j%2 == 0 {
					v.UpdateData(fixture())
				} else {
					v.UpdateData(map[string]interface{}{})
				}
			}
		}(i)
	}

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if _, _, err := v.Evaluate(input("backend", "Connector", "orders")); err != nil {
					errs <- fmt.Errorf("evaluate: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
