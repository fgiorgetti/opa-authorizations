// Package validator evaluates Skupper resources against the embedded OPA policies.
package validator

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"

	opacontroller "github.com/interconnect/opa-controller"
)

const query = "data.interconnect.authz"

// Validator holds the prepared policy and the current authorization data.
type Validator struct {
	mu        sync.RWMutex
	authzData map[string]interface{}
	store     storage.Store
	prepared  rego.PreparedEvalQuery
}

// New loads the embedded policies and compiles them once.
func New() (*Validator, error) {
	opts := []func(*rego.Rego){rego.Query(query)}

	entries, err := fs.ReadDir(opacontroller.Policies, "policies")
	if err != nil {
		return nil, fmt.Errorf("reading embedded policies: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, "_test.rego") {
			continue
		}
		src, err := fs.ReadFile(opacontroller.Policies, "policies/"+name)
		if err != nil {
			return nil, fmt.Errorf("reading policy %s: %w", name, err)
		}
		opts = append(opts, rego.Module(name, string(src)))
	}

	store := inmem.NewFromObject(map[string]interface{}{
		"authorizations": map[string]interface{}{},
	})
	opts = append(opts, rego.Store(store))

	prepared, err := rego.New(opts...).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("preparing policy: %w", err)
	}

	return &Validator{
		authzData: map[string]interface{}{},
		store:     store,
		prepared:  prepared,
	}, nil
}

// UpdateData replaces the authorization data used by subsequent evaluations.
func (v *Validator) UpdateData(authz map[string]interface{}) {
	if authz == nil {
		authz = map[string]interface{}{}
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	err := storage.WriteOne(context.Background(), v.store, storage.AddOp, storage.MustParsePath("/authorizations"), authz)
	if err != nil {
		slog.Error("failed to update authorization data", "error", err)
		return
	}
	v.authzData = authz
}

// Evaluate runs the policy against input and returns the decision.
func (v *Validator) Evaluate(input map[string]interface{}) (allowed bool, violations []string, err error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	rs, err := v.prepared.Eval(context.Background(), rego.EvalInput(input))
	if err != nil {
		return false, nil, fmt.Errorf("evaluating policy: %w", err)
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return false, nil, fmt.Errorf("policy returned no result")
	}

	result, ok := rs[0].Expressions[0].Value.(map[string]interface{})
	if !ok {
		return false, nil, fmt.Errorf("unexpected policy result type %T", rs[0].Expressions[0].Value)
	}

	allowed, _ = result["allowed"].(bool)

	rawViolations, _ := result["violations"].([]interface{})
	violations = make([]string, 0, len(rawViolations))
	for _, rv := range rawViolations {
		s, ok := rv.(string)
		if !ok {
			return false, nil, fmt.Errorf("unexpected violation type %T", rv)
		}
		violations = append(violations, s)
	}

	return allowed, violations, nil
}
