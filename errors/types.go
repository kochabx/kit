package errors

// Common HTTP error constructors providing better semantic meaning and consistency

// 4xx Client Errors
func BadRequest(format string, args ...any) *Error {
	return New(400, format, args...)
}

func Unauthorized(format string, args ...any) *Error {
	return New(401, format, args...)
}

func PaymentRequired(format string, args ...any) *Error {
	return New(402, format, args...)
}

func Forbidden(format string, args ...any) *Error {
	return New(403, format, args...)
}

func NotFound(format string, args ...any) *Error {
	return New(404, format, args...)
}

func MethodNotAllowed(format string, args ...any) *Error {
	return New(405, format, args...)
}

func NotAcceptable(format string, args ...any) *Error {
	return New(406, format, args...)
}

func RequestTimeout(format string, args ...any) *Error {
	return New(408, format, args...)
}

func Conflict(format string, args ...any) *Error {
	return New(409, format, args...)
}

func Gone(format string, args ...any) *Error {
	return New(410, format, args...)
}

func UnprocessableEntity(format string, args ...any) *Error {
	return New(422, format, args...)
}

func TooManyRequests(format string, args ...any) *Error {
	return New(429, format, args...)
}

// 5xx Server Errors
func Internal(format string, args ...any) *Error {
	return New(500, format, args...)
}

func NotImplemented(format string, args ...any) *Error {
	return New(501, format, args...)
}

func BadGateway(format string, args ...any) *Error {
	return New(502, format, args...)
}

func ServiceUnavailable(format string, args ...any) *Error {
	return New(503, format, args...)
}

func GatewayTimeout(format string, args ...any) *Error {
	return New(504, format, args...)
}

// Convenience functions with metadata
func BadRequestWithMetadata(metadata map[string]string, format string, args ...any) *Error {
	return NewWithMetadata(400, metadata, format, args...)
}

func UnauthorizedWithMetadata(metadata map[string]string, format string, args ...any) *Error {
	return NewWithMetadata(401, metadata, format, args...)
}

func ForbiddenWithMetadata(metadata map[string]string, format string, args ...any) *Error {
	return NewWithMetadata(403, metadata, format, args...)
}

func NotFoundWithMetadata(metadata map[string]string, format string, args ...any) *Error {
	return NewWithMetadata(404, metadata, format, args...)
}

func InternalWithMetadata(metadata map[string]string, format string, args ...any) *Error {
	return NewWithMetadata(500, metadata, format, args...)
}
