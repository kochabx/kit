// Package validator wraps go-playground/validator with translated messages and structured errors.
//
// Use Validate for default validation, or New to configure locales, field names, and custom rules.
// Use errors.AsType[*Error] to inspect field failures. Violations omits raw field values.
// Follow Error.Unwrap to explicitly inspect the underlying validation errors.
//
// Instances support concurrent use. Custom callbacks and concurrent access to target data remain the caller's responsibility.
// Context-aware callbacks handle cancellation and timeouts themselves. Invalid runtime validation tags may panic.
package validator
