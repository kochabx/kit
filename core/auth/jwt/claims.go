package jwt

import (
	"reflect"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// Claims is the contract accepted by Auth. Custom claims should anonymously
// embed SessionClaims and may add any application-specific fields.
type Claims interface {
	gojwt.Claims
	JWTClaims() *SessionClaims
}

// SessionClaims contains standard JWT claims and the opaque session identifier
// required to resolve server-side session state.
type SessionClaims struct {
	gojwt.RegisteredClaims
	SessionID string `json:"sid"`
}

// JWTClaims exposes the claims owned by the JWT session protocol.
func (claims *SessionClaims) JWTClaims() *SessionClaims { return claims }

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
