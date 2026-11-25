// Package gin provides Gin framework adapters for kit/x/transport/http
// that faithfully propagate middleware-wrapped http.ResponseWriter state.
package gin

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	kithttp "github.com/kochabx/kit/x/transport/http"
)

type bridgedWriter struct {
	gin.ResponseWriter
	rw      http.ResponseWriter
	status  int
	size    int
	written bool
}

func (w *bridgedWriter) Header() http.Header { return w.rw.Header() }
func (w *bridgedWriter) Write(p []byte) (int, error) {
	w.WriteHeaderNow()
	n, err := w.rw.Write(p)
	w.size += n
	return n, err
}
func (w *bridgedWriter) WriteString(s string) (int, error) {
	w.WriteHeaderNow()
	n, err := io.WriteString(w.rw, s)
	w.size += n
	return n, err
}

func (w *bridgedWriter) WriteHeader(code int) {
	if code >= http.StatusContinue && code <= 199 && code != http.StatusSwitchingProtocols {
		w.rw.WriteHeader(code)
		return
	}
	if !w.written {
		w.status = code
	}
}

func (w *bridgedWriter) WriteHeaderNow() {
	if !w.written {
		code := w.Status()
		if code == 0 {
			code = http.StatusOK
		}
		w.status = code
		w.written = true
		w.rw.WriteHeader(code)
	}
}

func (w *bridgedWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

func (w *bridgedWriter) Size() int                   { return w.size }
func (w *bridgedWriter) Written() bool               { return w.written }
func (w *bridgedWriter) Unwrap() http.ResponseWriter { return w.rw }
func (w *bridgedWriter) Flush() {
	w.WriteHeaderNow()
	_ = http.NewResponseController(w.rw).Flush()
}

// Adapt converts a standard net/http middleware into a gin.HandlerFunc while
// propagating request context and middleware-wrapped http.ResponseWriter mutations.
func Adapt(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		if mw == nil {
			c.Next()
			return
		}
		origWriter := c.Writer
		origReq := c.Request
		nextCalled := false

		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			c.Request = r
			bw := &bridgedWriter{ResponseWriter: origWriter, rw: w}
			c.Writer = bw
			defer func() {
				if bw.status != 0 && !bw.written {
					bw.WriteHeaderNow()
				}
				c.Writer = origWriter
			}()
			c.Next()
		})).ServeHTTP(origWriter, origReq)

		if !nextCalled {
			c.Abort()
		}
	}
}

// WriteProblem writes an RFC 9457 fault Problem response via Gin and aborts the chain.
func WriteProblem(c *gin.Context, err error) {
	kithttp.WriteProblem(c.Writer, c.Request, err)
	c.Abort()
}
