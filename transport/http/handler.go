package http

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/swaggo/http-swagger"

	"github.com/kochabx/kit/log"
)

var scalarPage = template.Must(template.New("scalar").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OpenAPI Reference</title>
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  <script>
    Scalar.createApiReference("#app", { url: {{.}} });
  </script>
</body>
</html>`))

// withBuiltinEndpoints mounts configured built-in endpoints in front of handler.
// It returns handler unchanged when no built-in endpoint is configured.
func withBuiltinEndpoints(handler http.Handler, opts *options) http.Handler {
	if opts.metrics == nil && opts.swagger == nil && opts.openAPI == nil && opts.health == nil {
		return handler
	}

	mux := http.NewServeMux()

	if opts.health != nil {
		mux.HandleFunc(opts.health.Path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
	}

	if opts.metrics != nil {
		mux.Handle(opts.metrics.Path, promhttp.HandlerFor(opts.metrics.Registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}))
	}

	if opts.swagger != nil {
		mux.Handle(prefixPath(opts.swagger.Path), httpSwagger.WrapHandler)
	}

	if opts.openAPI != nil {
		if len(opts.openAPI.Spec) == 0 {
			log.Error().Msg("openapi: Spec is empty, skipping registration")
		} else {
			mountOpenAPI(mux, opts.openAPI.Path, opts.openAPI.SpecPath, opts.openAPI.Spec)
		}
	}

	// Forward unmatched requests to the application handler.
	mux.Handle("/", handler)
	return mux
}

func mountOpenAPI(mux *http.ServeMux, uiPath, specPath string, spec []byte) {
	mux.HandleFunc(specPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(spec)
	})

	mux.Handle(prefixPath(uiPath), newScalarHandler(specPath))
}

func newScalarHandler(specPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = scalarPage.Execute(w, specPath)
	})
}

// prefixPath converts a path to net/http.ServeMux prefix form.
// It removes a framework-style wildcard suffix and appends a trailing slash.
func prefixPath(path string) string {
	if basePath, _, hasWildcard := strings.Cut(path, "/*"); hasWildcard {
		path = basePath
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}
