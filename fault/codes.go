// Package fault provides immutable domain errors with HTTP/gRPC status mapping
// and RFC 9457 Problem Details support.
package fault

import (
	stderrors "errors"
	"net/http"
)

// Code identifies a machine-readable domain error category.
type Code string

const (
	CodeInvalidArgument    Code = "INVALID_ARGUMENT"
	CodeUnauthenticated    Code = "UNAUTHENTICATED"
	CodePermissionDenied   Code = "PERMISSION_DENIED"
	CodeNotFound           Code = "NOT_FOUND"
	CodeAlreadyExists      Code = "ALREADY_EXISTS"
	CodeConflict           Code = "CONFLICT"
	CodeFailedPrecondition Code = "FAILED_PRECONDITION"
	CodeTooManyRequests    Code = "TOO_MANY_REQUESTS"
	CodeCanceled           Code = "CANCELED"
	CodeDeadlineExceeded   Code = "DEADLINE_EXCEEDED"
	CodeInternal           Code = "INTERNAL"
	CodeNotImplemented     Code = "NOT_IMPLEMENTED"
	CodeBadGateway         Code = "BAD_GATEWAY"
	CodeUnavailable        Code = "UNAVAILABLE"
	CodeGatewayTimeout     Code = "GATEWAY_TIMEOUT"
)

// Standard gRPC numeric status codes matching google.golang.org/grpc/codes
// without importing the gRPC runtime into the root module.
const (
	grpcOK                 uint32 = 0
	grpcCanceled           uint32 = 1
	grpcInvalidArgument    uint32 = 3
	grpcDeadlineExceeded   uint32 = 4
	grpcNotFound           uint32 = 5
	grpcAlreadyExists      uint32 = 6
	grpcPermissionDenied   uint32 = 7
	grpcResourceExhausted  uint32 = 8
	grpcFailedPrecondition uint32 = 9
	grpcAborted            uint32 = 10
	grpcUnimplemented      uint32 = 12
	grpcInternal           uint32 = 13
	grpcUnavailable        uint32 = 14
	grpcUnauthenticated    uint32 = 16
)

type statusMapping struct {
	httpStatus int
	grpcCode   uint32
}

var codeMappings = map[Code]statusMapping{
	CodeInvalidArgument:    {httpStatus: http.StatusBadRequest, grpcCode: grpcInvalidArgument},
	CodeUnauthenticated:    {httpStatus: http.StatusUnauthorized, grpcCode: grpcUnauthenticated},
	CodePermissionDenied:   {httpStatus: http.StatusForbidden, grpcCode: grpcPermissionDenied},
	CodeNotFound:           {httpStatus: http.StatusNotFound, grpcCode: grpcNotFound},
	CodeAlreadyExists:      {httpStatus: http.StatusConflict, grpcCode: grpcAlreadyExists},
	CodeConflict:           {httpStatus: http.StatusConflict, grpcCode: grpcAborted},
	CodeFailedPrecondition: {httpStatus: http.StatusPreconditionFailed, grpcCode: grpcFailedPrecondition},
	CodeTooManyRequests:    {httpStatus: http.StatusTooManyRequests, grpcCode: grpcResourceExhausted},
	CodeCanceled:           {httpStatus: 499, grpcCode: grpcCanceled},
	CodeDeadlineExceeded:   {httpStatus: http.StatusGatewayTimeout, grpcCode: grpcDeadlineExceeded},
	CodeInternal:           {httpStatus: http.StatusInternalServerError, grpcCode: grpcInternal},
	CodeNotImplemented:     {httpStatus: http.StatusNotImplemented, grpcCode: grpcUnimplemented},
	CodeBadGateway:         {httpStatus: http.StatusBadGateway, grpcCode: grpcUnavailable},
	CodeUnavailable:        {httpStatus: http.StatusServiceUnavailable, grpcCode: grpcUnavailable},
	CodeGatewayTimeout:     {httpStatus: http.StatusGatewayTimeout, grpcCode: grpcDeadlineExceeded},
}

func defaultHTTPStatus(code Code) int {
	if m, ok := codeMappings[code]; ok {
		return m.httpStatus
	}
	return http.StatusInternalServerError
}

func defaultGRPCCode(code Code, httpStatus int) uint32 {
	if m, ok := codeMappings[code]; ok {
		return m.grpcCode
	}
	switch httpStatus {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return grpcInvalidArgument
	case http.StatusUnauthorized:
		return grpcUnauthenticated
	case http.StatusForbidden:
		return grpcPermissionDenied
	case http.StatusNotFound:
		return grpcNotFound
	case http.StatusConflict:
		return grpcAborted
	case http.StatusPreconditionFailed:
		return grpcFailedPrecondition
	case http.StatusTooManyRequests:
		return grpcResourceExhausted
	case http.StatusNotImplemented:
		return grpcUnimplemented
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return grpcUnavailable
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return grpcDeadlineExceeded
	default:
		return grpcInternal
	}
}

// From extracts the first *Error in err's causal chain.
func From(err error) (*Error, bool) {
	return stderrors.AsType[*Error](err)
}

// CodeOf returns the Code of the first *Error in err's chain, or CodeInternal
// when err is not a *Error (and "" when err is nil).
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	if f, ok := From(err); ok {
		return f.Code()
	}
	return CodeInternal
}

// HTTPStatusOf returns the HTTP status code mapped from err, defaulting to 500
// for non-fault errors and 200 for nil.
func HTTPStatusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if f, ok := From(err); ok {
		return f.HTTPStatus()
	}
	return http.StatusInternalServerError
}
