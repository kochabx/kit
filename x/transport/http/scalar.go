package http

import (
	"bytes"
	"cmp"
	"html/template"
	"net/http"
	"strings"
)

const defaultScalarScriptURL = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.28.30"

// OpenAPIConfig configures built-in OpenAPI 3.x spec serving and Scalar UI
// without external swaggo/Swagger 2.0 dependencies.
type OpenAPIConfig struct {
	UIPath          string // Path prefix serving Scalar HTML UI (default "/docs").
	SpecPath        string // Path serving raw spec bytes (default "/openapi.json" or "/openapi.yaml").
	Spec            []byte // Raw OpenAPI 3.x JSON or YAML bytes.
	Title           string // HTML page title (default "OpenAPI Reference").
	ScalarScriptURL string // Pinned CDN or self-hosted Scalar script URL.
	ScalarSRI       string // Optional Subresource Integrity attribute (e.g. "sha384-...").
}

type scalarData struct {
	Title     string
	ScriptURL string
	SRI       string
	SpecPath  string
}

var scalarTemplate = template.Must(template.New("scalar").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
</head>
<body>
  <div id="app"></div>
  <script src="{{.ScriptURL}}"{{if .SRI}} integrity="{{.SRI}}" crossorigin="anonymous"{{end}}></script>
  <script>
    Scalar.createApiReference("#app", { url: {{.SpecPath}} });
  </script>
</body>
</html>`))

func mountOpenAPI(mux *http.ServeMux, cfg OpenAPIConfig) {
	if len(cfg.Spec) == 0 {
		return
	}
	isYAML := isYAMLSpec(cfg.SpecPath, cfg.Spec)
	specPath := cfg.SpecPath
	if specPath == "" {
		if isYAML {
			specPath = "/openapi.yaml"
		} else {
			specPath = "/openapi.json"
		}
	}
	uiPath := normalizePrefix(cmp.Or(cfg.UIPath, "/docs"))
	contentType := "application/json; charset=utf-8"
	if isYAML {
		contentType = "application/yaml; charset=utf-8"
	}
	spec := bytes.Clone(cfg.Spec)

	mux.HandleFunc("GET "+specPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(spec)
	})

	data := scalarData{
		Title:     cmp.Or(cfg.Title, "OpenAPI Reference"),
		ScriptURL: cmp.Or(cfg.ScalarScriptURL, defaultScalarScriptURL),
		SRI:       cfg.ScalarSRI,
		SpecPath:  specPath,
	}
	var rendered bytes.Buffer
	_ = scalarTemplate.Execute(&rendered, data)
	html := rendered.Bytes()

	mux.HandleFunc("GET "+uiPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(html)
	})
}

func isYAMLSpec(specPath string, spec []byte) bool {
	lower := strings.ToLower(specPath)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return true
	}
	if strings.HasSuffix(lower, ".json") {
		return false
	}
	trimmed := bytes.TrimSpace(spec)
	return len(trimmed) > 0 && trimmed[0] != '{'
}

func normalizePrefix(p string) string {
	p, _, _ = strings.Cut(p, "/*")
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}
