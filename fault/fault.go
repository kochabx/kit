package fault

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const separator = ", "

// Error is an immutable structured domain error carrying a symbolic code,
// caller-safe message, optional metadata, HTTP/gRPC mappings, and causal error.
type Error struct {
	code       Code
	httpStatus int
	message    string
	metadata   map[string]string
	cause      error
}

// New constructs an immutable Error for code with a safe caller-facing message.
func New(code Code, message string) *Error {
	c := cmp.Or(code, CodeInternal)
	return &Error{
		code:       c,
		httpStatus: defaultHTTPStatus(c),
		message:    message,
	}
}

// Newf constructs an immutable Error with a formatted caller-facing message.
func Newf(code Code, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

func InvalidArgument(message string) *Error { return New(CodeInvalidArgument, message) }
func BadRequest(message string) *Error      { return New(CodeInvalidArgument, message) }
func Unauthorized(message string) *Error    { return New(CodeUnauthenticated, message) }
func Forbidden(message string) *Error       { return New(CodePermissionDenied, message) }
func NotFound(message string) *Error        { return New(CodeNotFound, message) }
func Conflict(message string) *Error        { return New(CodeConflict, message) }

func UnprocessableEntity(message string) *Error {
	return New(CodeInvalidArgument, message).WithStatus(http.StatusUnprocessableEntity)
}

func TooManyRequests(message string) *Error    { return New(CodeTooManyRequests, message) }
func Internal(message string) *Error           { return New(CodeInternal, message) }
func NotImplemented(message string) *Error     { return New(CodeNotImplemented, message) }
func BadGateway(message string) *Error         { return New(CodeBadGateway, message) }
func ServiceUnavailable(message string) *Error { return New(CodeUnavailable, message) }
func GatewayTimeout(message string) *Error     { return New(CodeGatewayTimeout, message) }
func Canceled(message string) *Error           { return New(CodeCanceled, message) }
func DeadlineExceeded(message string) *Error   { return New(CodeDeadlineExceeded, message) }

// Error returns a deterministic structured description for diagnostics and logs.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	var b strings.Builder
	b.Grow(len(e.code) + len(e.message) + 40)
	b.WriteString("code=")
	b.WriteString(string(e.code))
	b.WriteString(separator)
	b.WriteString("status=")
	b.WriteString(strconv.Itoa(e.HTTPStatus()))
	b.WriteString(separator)
	b.WriteString("message=")
	b.WriteString(e.message)

	if len(e.metadata) > 0 {
		b.WriteString(separator)
		b.WriteString("metadata={")
		for i, k := range slices.Sorted(maps.Keys(e.metadata)) {
			if i > 0 {
				b.WriteString(separator)
			}
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(e.metadata[k])
		}
		b.WriteByte('}')
	}

	if e.cause != nil {
		b.WriteString(separator)
		b.WriteString("cause=")
		b.WriteString(e.cause.Error())
	}

	return b.String()
}

// Unwrap returns the underlying cause for standard errors.Is / errors.AsType.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Code returns the symbolic domain error code.
func (e *Error) Code() Code {
	if e == nil {
		return ""
	}
	return e.code
}

// Message returns the caller-safe message without internal causes.
func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

// HTTPStatus returns the mapped HTTP status code.
func (e *Error) HTTPStatus() int {
	if e == nil {
		return http.StatusOK
	}
	if e.httpStatus > 0 {
		return e.httpStatus
	}
	return defaultHTTPStatus(e.code)
}

// GRPCCode returns the standard numeric gRPC status code (0–16).
func (e *Error) GRPCCode() uint32 {
	if e == nil {
		return grpcOK
	}
	return defaultGRPCCode(e.code, e.HTTPStatus())
}

// Metadata returns a defensive copy of attached metadata, or nil if empty.
func (e *Error) Metadata() map[string]string {
	if e == nil || len(e.metadata) == 0 {
		return nil
	}
	return maps.Clone(e.metadata)
}

// WithCause returns an immutable copy wrapping cause.
func (e *Error) WithCause(cause error) *Error {
	if e == nil {
		return nil
	}
	clone := e.clone()
	clone.cause = cause
	return clone
}

// WithStatus returns an immutable copy overriding the HTTP status code.
func (e *Error) WithStatus(status int) *Error {
	if e == nil || status <= 0 {
		return e
	}
	clone := e.clone()
	clone.httpStatus = status
	return clone
}

// With returns an immutable copy adding or overriding a single metadata key.
func (e *Error) With(key, value string) *Error {
	if e == nil || key == "" {
		return e
	}
	clone := e.clone()
	if clone.metadata == nil {
		clone.metadata = make(map[string]string, 1)
	}
	clone.metadata[key] = value
	return clone
}

// WithMetadata returns an immutable copy merging m into its metadata.
func (e *Error) WithMetadata(m map[string]string) *Error {
	if e == nil || len(m) == 0 {
		return e
	}
	clone := e.clone()
	if clone.metadata == nil {
		clone.metadata = make(map[string]string, len(m))
	}
	maps.Copy(clone.metadata, m)
	return clone
}

func (e *Error) clone() *Error {
	cp := *e
	cp.metadata = maps.Clone(e.metadata)
	return &cp
}
