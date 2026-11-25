package gin

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kochabx/kit/fault"
	kithttp "github.com/kochabx/kit/x/transport/http"
)

type ctxKey struct{}

func TestAdaptPropagatesResponseWriterAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	authMW := kithttp.Auth(kithttp.AuthenticatorFunc(func(ctx context.Context, r *http.Request) (context.Context, error) {
		if r.Header.Get("X-Token") != "ok" {
			return ctx, fault.Unauthorized("bad token")
		}
		return context.WithValue(ctx, ctxKey{}, "alice"), nil
	}))

	r := gin.New()
	r.Use(Adapt(kithttp.Logger(logger)))
	r.Use(Adapt(authMW))
	r.GET("/me", func(c *gin.Context) {
		user, _ := c.Request.Context().Value(ctxKey{}).(string)
		c.JSON(http.StatusCreated, gin.H{"user": user})
	})
	r.GET("/fail", func(c *gin.Context) {
		Fail(c, fault.Forbidden("nope"))
	})

	// Middleware-wrapped ResponseRecorder inside Logger MUST capture downstream Gin 201 status and body size.
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("X-Token", "ok")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"user":"alice"`) {
		t.Fatalf("unexpected gin response: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type dropped by bridgedWriter: got %q", ct)
	}
	if !strings.Contains(logBuf.String(), `"status":201`) {
		t.Fatalf("logger middleware missed wrapped gin status 201: %s", logBuf.String())
	}

	// Fail works cleanly inside adapted Gin handlers.
	failReq := httptest.NewRequest(http.MethodGet, "/fail", nil)
	failReq.Header.Set("X-Token", "ok")
	failRec := httptest.NewRecorder()
	r.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from Fail, got %d", failRec.Code)
	}

	// Aborting middleware must prevent route handler execution and propagate 401 status.
	unauthReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	unauthRec := httptest.NewRecorder()
	r.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from adapted Auth middleware, got %d", unauthRec.Code)
	}
}

func TestAdaptBridgedWriterFlushCommitsBufferedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(Adapt(func(next http.Handler) http.Handler { return next }))
	r.GET("/stream", func(c *gin.Context) {
		c.Status(http.StatusAccepted)
		c.Writer.Flush()
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if rec.Code != http.StatusAccepted || !rec.Flushed {
		t.Fatalf("Flush after c.Status(202) = code:%d flushed:%v, want 202 and flushed=true", rec.Code, rec.Flushed)
	}
}

func TestAdaptPanicAfterStatusDoesNotBypassRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(Adapt(kithttp.Recovery(slog.New(slog.DiscardHandler))))
	r.GET("/panic-after-status", func(c *gin.Context) {
		c.Status(http.StatusCreated)
		panic("gin boom")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic-after-status", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from Recovery after panic following c.Status(201), got %d (%s)", rec.Code, rec.Body.String())
	}
}
