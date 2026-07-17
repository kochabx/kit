package errors

// Common HTTP error constructors. Messages are literal; use Newf when
// formatting is required.

func BadRequest(message string) *Error          { return New(400, message) }
func Unauthorized(message string) *Error        { return New(401, message) }
func PaymentRequired(message string) *Error     { return New(402, message) }
func Forbidden(message string) *Error           { return New(403, message) }
func NotFound(message string) *Error            { return New(404, message) }
func MethodNotAllowed(message string) *Error    { return New(405, message) }
func NotAcceptable(message string) *Error       { return New(406, message) }
func RequestTimeout(message string) *Error      { return New(408, message) }
func Conflict(message string) *Error            { return New(409, message) }
func Gone(message string) *Error                { return New(410, message) }
func UnprocessableEntity(message string) *Error { return New(422, message) }
func TooManyRequests(message string) *Error     { return New(429, message) }
func Internal(message string) *Error            { return New(500, message) }
func NotImplemented(message string) *Error      { return New(501, message) }
func BadGateway(message string) *Error          { return New(502, message) }
func ServiceUnavailable(message string) *Error  { return New(503, message) }
func GatewayTimeout(message string) *Error      { return New(504, message) }
