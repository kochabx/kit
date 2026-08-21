package http

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/swaggo/swag"

	"github.com/kochabx/kit/log"
)

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
			const instanceName = "openapi"
			swag.Register(instanceName, openapiSpec(opts.openAPI.Spec))
			mux.Handle(prefixPath(opts.openAPI.Path), httpSwagger.Handler(httpSwagger.InstanceName(instanceName)))
		}
	}

	// Forward unmatched requests to the application handler.
	mux.Handle("/", handler)
	return mux
}

// prefixPath converts a path to net/http.ServeMux prefix form.
// It removes a framework-style wildcard suffix and appends a trailing slash.
func prefixPath(path string) string {
	if idx := strings.Index(path, "/*"); idx != -1 {
		path = path[:idx]
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

type openapiSpec string

func (o openapiSpec) ReadDoc() string { return string(o) }
