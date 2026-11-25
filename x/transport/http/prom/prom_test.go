package prom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMiddlewareAndScrapeHandler(t *testing.T) {
	reg := prometheus.NewRegistry()
	mw, err := Middleware(WithNamespace("kit"), WithRegisterer(reg))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	mux.Handle("GET /metrics", Handler(reg))

	h := mw(mux)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("ping status = %d", rec.Code)
	}

	scrapeRec := httptest.NewRecorder()
	h.ServeHTTP(scrapeRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrapeRec.Body.String()
	wantMatchedSample := `kit_http_requests_total{method="GET",path="GET /ping",status="202"} 1`
	if scrapeRec.Code != http.StatusOK || !strings.Contains(body, wantMatchedSample) {
		t.Fatalf("unexpected matched route metric sample (want %q): code=%d body=%s", wantMatchedSample, scrapeRec.Code, body)
	}

	// Unmatched 404 paths must record as "UNMATCHED" rather than unbounded raw URL paths.
	notFoundRec := httptest.NewRecorder()
	h.ServeHTTP(notFoundRec, httptest.NewRequest(http.MethodGet, "/attacker-random-path-999", nil))
	scrape2 := httptest.NewRecorder()
	h.ServeHTTP(scrape2, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body2 := scrape2.Body.String()
	wantUnmatchedSample := `kit_http_requests_total{method="GET",path="UNMATCHED",status="404"} 1`
	if strings.Contains(body2, "/attacker-random-path-999") || !strings.Contains(body2, wantUnmatchedSample) {
		t.Fatalf("expected unmatched sample %q without raw URL path, got: %s", wantUnmatchedSample, body2)
	}
}

func TestMiddlewareRegistrationRollbackOnConflict(t *testing.T) {
	reg := prometheus.NewRegistry()
	conflict := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kit",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution.",
	}, []string{"method", "path"})
	conflict.WithLabelValues("GET", "/seed").Observe(0.1)
	if err := reg.Register(conflict); err != nil {
		t.Fatal(err)
	}

	if _, err := Middleware(WithNamespace("kit"), WithRegisterer(reg)); err == nil {
		t.Fatal("expected registration conflict error")
	}
	// Verify http_requests_total was rolled back from reg on failure.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "kit_http_requests_total" {
			t.Fatalf("http_requests_total remained registered after Middleware failure: %v", mf)
		}
	}

	// Retry after removing conflict must succeed cleanly without leaving http_requests_total orphaned.
	if !reg.Unregister(conflict) {
		t.Fatal("expected Unregister(conflict) to succeed")
	}
	if _, err := Middleware(WithNamespace("kit"), WithRegisterer(reg)); err != nil {
		t.Fatalf("retry Middleware after clearing conflict failed: %v", err)
	}
}
