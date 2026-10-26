package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/open-policy-agent/opa/v1/bundle"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	opacontroller "github.com/interconnect/opa-controller"
	"github.com/interconnect/opa-controller/internal/cache"
	"github.com/interconnect/opa-controller/internal/validator"
)

const (
	configMapName = "van-authorizations"
	dataKey       = "authorizations.json"
	maxBodyBytes  = 1 << 20
	policiesDir   = "policies"
	bundleRoot    = "interconnect"
)

// buildBundle packs the embedded policies (test files excluded) into a
// gzipped tarball in the OPA bundle format.
//
// The bundle holds policies only. Its manifest claims the "interconnect" root,
// so consumers keep ownership of data.authorizations, which the policies read
// as {"<namespace>": {"provides": [...], "requires": [...]}}.
func buildBundle() ([]byte, error) {
	entries, err := fs.ReadDir(opacontroller.Policies, policiesDir)
	if err != nil {
		return nil, fmt.Errorf("reading embedded policies: %w", err)
	}

	roots := []string{bundleRoot}
	b := bundle.Bundle{
		Manifest: bundle.Manifest{Roots: &roots},
		Data:     map[string]any{},
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, "_test.rego") {
			continue
		}
		path := policiesDir + "/" + name
		src, err := fs.ReadFile(opacontroller.Policies, path)
		if err != nil {
			return nil, fmt.Errorf("reading policy %s: %w", name, err)
		}
		b.Modules = append(b.Modules, bundle.ModuleFile{
			URL:  path,
			Path: path,
			Raw:  src,
		})
	}

	var buf bytes.Buffer
	if err := bundle.NewWriter(&buf).Write(b); err != nil {
		return nil, fmt.Errorf("writing bundle: %w", err)
	}
	return buf.Bytes(), nil
}

// NamespaceAuthz lists the routing keys a namespace may provide and require.
type NamespaceAuthz struct {
	Provides []string `json:"provides"`
	Requires []string `json:"requires"`
}

// AuthorizationsData maps a namespace name to its authorizations.
type AuthorizationsData map[string]NamespaceAuthz

// parseAuthorizations validates and decodes an authorizations payload.
func parseAuthorizations(r io.Reader) (AuthorizationsData, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var data AuthorizationsData
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("unexpected trailing data")
	}
	if data == nil {
		return nil, errors.New("payload must be a JSON object")
	}
	for ns, a := range data {
		if strings.TrimSpace(ns) == "" {
			return nil, errors.New("namespace name must not be empty")
		}
		if a.Provides == nil {
			a.Provides = []string{}
		}
		if a.Requires == nil {
			a.Requires = []string{}
		}
		data[ns] = a
	}
	return data, nil
}

// authorizeRequest is the body of POST /api/authorize.
type authorizeRequest struct {
	Namespace  string `json:"namespace"`
	Type       string `json:"type"`
	RoutingKey string `json:"routing_key"`
}

// authorizeResponse is the result of POST /api/authorize.
type authorizeResponse struct {
	Allowed    bool     `json:"allowed"`
	Violations []string `json:"violations"`
}

// parseAuthorizeRequest validates and decodes an authorize payload. The type
// is normalised to the values the policy understands: Connector or Listener.
func parseAuthorizeRequest(r io.Reader) (authorizeRequest, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var req authorizeRequest
	if err := dec.Decode(&req); err != nil {
		return authorizeRequest{}, err
	}
	if dec.More() {
		return authorizeRequest{}, errors.New("unexpected trailing data")
	}
	if strings.TrimSpace(req.Namespace) == "" {
		return authorizeRequest{}, errors.New("namespace must not be empty")
	}
	if strings.TrimSpace(req.RoutingKey) == "" {
		return authorizeRequest{}, errors.New("routing_key must not be empty")
	}
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "connector":
		req.Type = "Connector"
	case "listener":
		req.Type = "Listener"
	default:
		return authorizeRequest{}, errors.New("type must be connector or listener")
	}
	return req, nil
}

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>OPA Controller — Endpoints</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    nav { margin-bottom: 1.5rem; }
    nav a { margin-right: 1rem; text-decoration: none; color: #0066cc; }
    nav a.active { font-weight: bold; color: #222; text-decoration: underline; }
    table { border-collapse: collapse; width: 100%; }
    th, td { border: 1px solid #ccc; padding: 0.5rem 1rem; text-align: left; }
    th { background: #f4f4f4; }
    .yes { color: green; font-weight: bold; }
    .no  { color: red;   font-weight: bold; }
  </style>
</head>
<body>
  <nav>
    <a href="/" class="active">Endpoints</a>
    <a href="/authorizations">Authorizations</a>
  </nav>

  <h1>OPA Controller — Endpoints</h1>
  <table>
    <thead>
      <tr>
        <th>Endpoint</th>
        <th>Type</th>
        <th>Allowed</th>
        <th>Violations</th>
      </tr>
    </thead>
    <tbody>
      {{- range .}}
      <tr>
        <td>{{.Namespace}}/{{.Name}}</td>
        <td>{{.Type}}</td>
        <td>{{if .Allowed}}<span class="yes">Yes</span>{{else}}<span class="no">No</span>{{end}}</td>
        <td>{{violations .Violations}}</td>
      </tr>
      {{- end}}
    </tbody>
  </table>
</body>
</html>
`

const authorizationsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>OPA Controller — Authorizations</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    nav { margin-bottom: 1.5rem; }
    nav a { margin-right: 1rem; text-decoration: none; color: #0066cc; }
    nav a.active { font-weight: bold; color: #222; text-decoration: underline; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 1.5rem; }
    th, td { border: 1px solid #ccc; padding: 0.5rem 1rem; text-align: left; }
    th { background: #f4f4f4; }
    .alert { padding: 0.75rem 1rem; margin-bottom: 1rem; border-radius: 4px; display: none; }
    .alert.success { background-color: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
    .alert.error { background-color: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
    .form-inline { display: flex; gap: 0.5rem; align-items: flex-end; margin-bottom: 1.5rem; flex-wrap: wrap; }
    .form-inline div { display: flex; flex-direction: column; }
    label { font-size: 0.85rem; font-weight: bold; margin-bottom: 0.25rem; }
    input[type="text"] { padding: 0.4rem 0.6rem; border: 1px solid #ccc; border-radius: 3px; font-size: 0.9rem; }
    button { padding: 0.4rem 0.8rem; cursor: pointer; border: 1px solid #999; background: #eee; border-radius: 3px; font-size: 0.9rem; }
    button:hover { background: #ddd; }
    button.primary { background: #0066cc; color: #fff; border-color: #0055aa; }
    button.primary:hover { background: #0055aa; }
    button.danger { background: #dc3545; color: #fff; border-color: #bd2130; }
    button.danger:hover { background: #bd2130; }
    .actions { display: flex; gap: 0.5rem; }
    .empty-msg { color: #666; font-style: italic; }
  </style>
</head>
<body>
  <nav>
    <a href="/">Endpoints</a>
    <a href="/authorizations" class="active">Authorizations</a>
  </nav>

  <h1>OPA Controller — Authorizations</h1>

  <div id="alert" class="alert"></div>

  <h2>Add Namespace Authorization</h2>
  <form id="add-form" class="form-inline" onsubmit="handleAdd(event)">
    <div>
      <label for="new-namespace">Namespace</label>
      <input type="text" id="new-namespace" placeholder="e.g. backend" required>
    </div>
    <div>
      <label for="new-provides">Provides (comma-separated)</label>
      <input type="text" id="new-provides" placeholder="e.g. auth.service, order.api">
    </div>
    <div>
      <label for="new-requires">Requires (comma-separated)</label>
      <input type="text" id="new-requires" placeholder="e.g. db.service, user.api">
    </div>
    <div>
      <button type="submit">Add Entry</button>
    </div>
  </form>

  <h2>Configured Authorizations</h2>
  <table>
    <thead>
      <tr>
        <th style="width: 20%;">Namespace</th>
        <th style="width: 35%;">Provides</th>
        <th style="width: 35%;">Requires</th>
        <th style="width: 10%;">Actions</th>
      </tr>
    </thead>
    <tbody id="authz-tbody">
      <tr><td colspan="4" class="empty-msg">Loading authorizations...</td></tr>
    </tbody>
  </table>

  <div style="display: flex; gap: 0.5rem;">
    <button type="button" class="primary" onclick="saveAuthorizations()">Save Authorizations</button>
    <button type="button" onclick="loadAuthorizations()">Reload</button>
  </div>

  <script>
    let authzData = {};
    let editingNamespace = null;

    function showAlert(msg, isError) {
      const el = document.getElementById('alert');
      el.textContent = msg;
      el.className = 'alert ' + (isError ? 'error' : 'success');
      el.style.display = 'block';
      setTimeout(() => {
        el.style.display = 'none';
      }, 5000);
    }

    function parseKeys(str) {
      if (!str) return [];
      return str.split(',').map(s => s.trim()).filter(s => s.length > 0);
    }

    function loadAuthorizations() {
      fetch('/api/authorizations')
        .then(res => {
          if (!res.ok) throw new Error('Failed to load authorizations: ' + res.statusText);
          return res.json();
        })
        .then(data => {
          authzData = data || {};
          editingNamespace = null;
          renderTable();
        })
        .catch(err => {
          showAlert(err.message, true);
        });
    }

    function renderTable() {
      const tbody = document.getElementById('authz-tbody');
      tbody.innerHTML = '';
      const namespaces = Object.keys(authzData).sort();

      if (namespaces.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4" class="empty-msg">No authorizations configured.</td></tr>';
        return;
      }

      namespaces.forEach(ns => {
        const item = authzData[ns] || { provides: [], requires: [] };
        const tr = document.createElement('tr');

        if (editingNamespace === ns) {
          tr.innerHTML = '<td><strong>' + escapeHtml(ns) + '</strong></td>' +
            '<td><input type="text" id="edit-provides-' + escapeHtml(ns) + '" value="' + escapeHtml((item.provides || []).join(', ')) + '" style="width: 90%;"></td>' +
            '<td><input type="text" id="edit-requires-' + escapeHtml(ns) + '" value="' + escapeHtml((item.requires || []).join(', ')) + '" style="width: 90%;"></td>' +
            '<td class="actions">' +
              '<button type="button" onclick="saveEdit(\'' + escapeHtml(ns) + '\')">Done</button>' +
              '<button type="button" onclick="cancelEdit()">Cancel</button>' +
            '</td>';
        } else {
          const providesText = (item.provides && item.provides.length > 0) ? escapeHtml(item.provides.join(', ')) : '<span class="empty-msg">none</span>';
          const requiresText = (item.requires && item.requires.length > 0) ? escapeHtml(item.requires.join(', ')) : '<span class="empty-msg">none</span>';
          tr.innerHTML = '<td><strong>' + escapeHtml(ns) + '</strong></td>' +
            '<td>' + providesText + '</td>' +
            '<td>' + requiresText + '</td>' +
            '<td class="actions">' +
              '<button type="button" onclick="startEdit(\'' + escapeHtml(ns) + '\')">Edit</button>' +
              '<button type="button" class="danger" onclick="deleteNamespace(\'' + escapeHtml(ns) + '\')">Delete</button>' +
            '</td>';
        }
        tbody.appendChild(tr);
      });
    }

    function escapeHtml(str) {
      if (!str) return '';
      return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
    }

    function handleAdd(event) {
      event.preventDefault();
      const nsInput = document.getElementById('new-namespace');
      const provInput = document.getElementById('new-provides');
      const reqInput = document.getElementById('new-requires');

      const ns = nsInput.value.trim();
      if (!ns) return;

      const provides = parseKeys(provInput.value);
      const requires = parseKeys(reqInput.value);

      authzData[ns] = {
        provides: provides,
        requires: requires
      };

      nsInput.value = '';
      provInput.value = '';
      reqInput.value = '';

      renderTable();
      showAlert('Namespace added to list. Click "Save Authorizations" to persist changes.', false);
    }

    function startEdit(ns) {
      editingNamespace = ns;
      renderTable();
    }

    function cancelEdit() {
      editingNamespace = null;
      renderTable();
    }

    function saveEdit(ns) {
      const provInput = document.getElementById('edit-provides-' + ns);
      const reqInput = document.getElementById('edit-requires-' + ns);
      if (provInput && reqInput) {
        authzData[ns] = {
          provides: parseKeys(provInput.value),
          requires: parseKeys(reqInput.value)
        };
      }
      editingNamespace = null;
      renderTable();
    }

    function deleteNamespace(ns) {
      if (confirm('Delete authorization rules for namespace "' + ns + '"?')) {
        delete authzData[ns];
        if (editingNamespace === ns) editingNamespace = null;
        renderTable();
        showAlert('Namespace removed. Click "Save Authorizations" to persist changes.', false);
      }
    }

    function saveAuthorizations() {
      if (editingNamespace) {
        saveEdit(editingNamespace);
      }
      fetch('/api/authorizations', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(authzData)
      })
      .then(res => {
        if (!res.ok) {
          return res.text().then(text => { throw new Error(text || 'Save failed'); });
        }
        return res.json();
      })
      .then(() => {
        showAlert('Authorizations saved successfully.', false);
        loadAuthorizations();
      })
      .catch(err => {
        showAlert('Failed to save: ' + err.message, true);
      });
    }

    window.addEventListener('DOMContentLoaded', loadAuthorizations);
  </script>
</body>
</html>
`

var pageTmpl = template.Must(
	template.New("index").
		Funcs(template.FuncMap{
			"violations": func(v []string) string { return strings.Join(v, ", ") },
		}).
		Parse(pageHTML),
)

// New returns a configured *http.Server bound to addr.
// The server is not started; callers do: go srv.ListenAndServe().
// The client and namespace are used to read and write the van-authorizations ConfigMap.
// The validator answers POST /api/authorize with the live policy decision.
func New(c *cache.Cache, client kubernetes.Interface, v *validator.Validator, namespace, addr string) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		results := c.GetAll()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pageTmpl.Execute(w, results); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("GET /authorizations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := io.WriteString(w, authorizationsHTML); err != nil {
			slog.Error("failed to write authorizations page", "error", err)
		}
	})

	mux.HandleFunc("GET /api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		results := c.GetAll()
		// Ensure we serialise an empty array, not null.
		if results == nil {
			results = []cache.EndpointResult{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			http.Error(w, "json error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("GET /api/bundle", func(w http.ResponseWriter, r *http.Request) {
		out, err := buildBundle()
		if err != nil {
			slog.Error("failed to build policy bundle", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="bundle.tar.gz"`)
		if _, err := w.Write(out); err != nil {
			slog.Error("failed to write bundle response", "error", err)
		}
	})

	mux.HandleFunc("GET /api/authorizations", func(w http.ResponseWriter, r *http.Request) {
		cm, err := client.CoreV1().ConfigMaps(namespace).Get(r.Context(), configMapName, metav1.GetOptions{})
		raw := "{}"
		switch {
		case apierrors.IsNotFound(err):
		case err != nil:
			slog.Error("failed to get van-authorizations ConfigMap", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		default:
			if v, ok := cm.Data[dataKey]; ok && strings.TrimSpace(v) != "" {
				raw = v
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, raw); err != nil {
			slog.Error("failed to write authorizations response", "error", err)
		}
	})

	mux.HandleFunc("PUT /api/authorizations", func(w http.ResponseWriter, r *http.Request) {
		data, err := parseAuthorizations(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			http.Error(w, "invalid authorizations payload", http.StatusBadRequest)
			return
		}
		out, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			slog.Error("failed to marshal authorizations", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		cms := client.CoreV1().ConfigMaps(namespace)
		cm, err := cms.Get(r.Context(), configMapName, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			_, err = cms.Create(r.Context(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespace},
				Data:       map[string]string{dataKey: string(out)},
			}, metav1.CreateOptions{})
		case err == nil:
			if cm.Data == nil {
				cm.Data = map[string]string{}
			}
			cm.Data[dataKey] = string(out)
			_, err = cms.Update(r.Context(), cm, metav1.UpdateOptions{})
		}
		if err != nil {
			slog.Error("failed to save van-authorizations ConfigMap", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok"}`)
	})

	mux.HandleFunc("POST /api/authorize", func(w http.ResponseWriter, r *http.Request) {
		req, err := parseAuthorizeRequest(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			http.Error(w, "invalid authorize request", http.StatusBadRequest)
			return
		}

		allowed, violations, err := v.Evaluate(map[string]interface{}{
			"namespace":   req.Namespace,
			"type":        req.Type,
			"routing_key": req.RoutingKey,
		})
		if err != nil {
			slog.Error("failed to evaluate authorize request", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if violations == nil {
			violations = []string{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authorizeResponse{Allowed: allowed, Violations: violations}); err != nil {
			slog.Error("failed to write authorize response", "error", err)
		}
	})

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}
