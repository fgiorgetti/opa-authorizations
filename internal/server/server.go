package server

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/interconnect/opa-controller/internal/cache"
)

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>OPA Controller — Endpoints</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    table { border-collapse: collapse; width: 100%; }
    th, td { border: 1px solid #ccc; padding: 0.5rem 1rem; text-align: left; }
    th { background: #f4f4f4; }
    .yes { color: green; font-weight: bold; }
    .no  { color: red;   font-weight: bold; }
  </style>
</head>
<body>
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

var pageTmpl = template.Must(
	template.New("index").
		Funcs(template.FuncMap{
			"violations": func(v []string) string { return strings.Join(v, ", ") },
		}).
		Parse(pageHTML),
)

// New returns a configured *http.Server bound to addr.
// The server is not started; callers do: go srv.ListenAndServe().
func New(c *cache.Cache, addr string) *http.Server {
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

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}
