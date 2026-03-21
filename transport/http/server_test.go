package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer_Defaults(t *testing.T) {
	s := NewServer(http.NotFoundHandler())
	require.NotNil(t, s)
	assert.NotNil(t, s.Handler())
	assert.Equal(t, defaultAddr, s.srv.Addr)
	assert.Equal(t, defaultName, s.name)
	assert.Equal(t, defaultReadTimeout, s.srv.ReadTimeout)
	assert.Equal(t, defaultWriteTimeout, s.srv.WriteTimeout)
	assert.Equal(t, defaultIdleTimeout, s.srv.IdleTimeout)
}

func TestNewServer_CustomOptions(t *testing.T) {
	s := NewServer(
		http.NotFoundHandler(),
		WithAddr(":9090"),
		WithName("api"),
		WithTimeout(5*time.Second, 15*time.Second, 30*time.Second),
	)
	assert.Equal(t, ":9090", s.srv.Addr)
	assert.Equal(t, "api", s.name)
	assert.Equal(t, 5*time.Second, s.srv.ReadTimeout)
	assert.Equal(t, 15*time.Second, s.srv.WriteTimeout)
	assert.Equal(t, 30*time.Second, s.srv.IdleTimeout)
}

func TestNewServer_InvalidAddrFallback(t *testing.T) {
	s := NewServer(http.NotFoundHandler(), WithAddr("not-valid"))
	assert.Equal(t, defaultAddr, s.srv.Addr)
}

func TestServer_HealthEndpoint(t *testing.T) {
	s := NewServer(
		http.NotFoundHandler(),
		WithHealth(HealthOption{Path: "/health"}),
	)

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
}

func TestServer_HealthEndpoint_DefaultPath(t *testing.T) {
	s := NewServer(
		http.NotFoundHandler(),
		WithHealth(HealthOption{}),
	)

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_MetricsEndpoint(t *testing.T) {
	s := NewServer(
		http.NotFoundHandler(),
		WithMetrics(MetricsOption{Path: "/metrics"}),
	)

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_UserHandlerForwarded(t *testing.T) {
	called := false
	userHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	s := NewServer(userHandler, WithHealth(HealthOption{Path: "/health"}))

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/ping", nil))

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_StdlibMiddleware(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		inner.ServeHTTP(w, r)
	})
	s := NewServer(wrapped)

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServer_Shutdown(t *testing.T) {
	s := NewServer(http.NotFoundHandler(), WithAddr(":19080"))

	errCh := make(chan error, 1)
	go func() { errCh <- s.Run() }()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, s.Shutdown(ctx))
	require.NoError(t, <-errCh)
}

// ---------------------------------------------------------------------------
// Multi-framework tests
// ---------------------------------------------------------------------------

// TestServer_WithStdlibMux verifies the server works with a plain net/http.ServeMux.
func TestServer_WithStdlibMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})
	mux.HandleFunc("/bye", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("bye"))
	})

	s := NewServer(mux)

	t.Run("hello route", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hello", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "hello", w.Body.String())
	})

	t.Run("bye route", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bye", nil))
		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Equal(t, "bye", w.Body.String())
	})

	t.Run("unknown route returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nope", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestServer_WithStdlibMux_AndHealth verifies that built-in endpoints are served
// alongside routes registered on a stdlib mux.
func TestServer_WithStdlibMux_AndHealth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})

	s := NewServer(mux, WithHealth(HealthOption{Path: "/health"}))

	t.Run("health endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
	})

	t.Run("user route still reachable", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "pong", w.Body.String())
	})
}

// TestServer_WithGin verifies the server works with a Gin engine.
func TestServer_WithGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/greet", func(c *gin.Context) {
		c.String(http.StatusOK, "hi gin")
	})
	engine.POST("/echo", func(c *gin.Context) {
		c.String(http.StatusCreated, "created")
	})

	s := NewServer(engine)

	t.Run("GET /greet", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/greet", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "hi gin", w.Body.String())
	})

	t.Run("POST /echo", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/echo", nil))
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("unregistered route returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/unknown", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestServer_WithGin_AndHealth verifies that built-in health/metrics endpoints
// are served by the outer mux while Gin handles all other routes.
func TestServer_WithGin_AndHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/users", func(c *gin.Context) {
		c.String(http.StatusOK, "users")
	})

	s := NewServer(engine,
		WithHealth(HealthOption{Path: "/health"}),
		WithMetrics(MetricsOption{Path: "/metrics"}),
	)

	t.Run("health endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
	})

	t.Run("metrics endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("gin route still reachable", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "users", w.Body.String())
	})
}

// TestServer_WithGin_PathParams verifies Gin path parameters work correctly
// when the engine is wrapped by the kit server.
func TestServer_WithGin_PathParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/item/:id", func(c *gin.Context) {
		c.String(http.StatusOK, c.Param("id"))
	})

	s := NewServer(engine)

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/item/42", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "42", w.Body.String())
}

// TestServer_FrameworkAgnostic_HandlerFuncAsHandler confirms a bare
// http.HandlerFunc satisfies the interface and is served correctly.
func TestServer_FrameworkAgnostic_HandlerFuncAsHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusNoContent)
	})

	s := NewServer(handler)

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/anything", nil))
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "yes", w.Header().Get("X-Custom"))
}
