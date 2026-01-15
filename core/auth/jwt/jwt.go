package jwt

import (
	"errors"
	"reflect"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT represents the JWT handler
type JWT struct {
	config *Config
}

// New creates a new JWT instance
func New(config *Config) (*JWT, error) {
	if err := config.init(); err != nil {
		return nil, err
	}
	return &JWT{config: config}, nil
}

// Generate creates a JWT token from any struct that embeds jwt.RegisteredClaims
func (j *JWT) Generate(claims jwt.Claims) (string, error) {
	return j.generate(claims, j.config.Expire)
}

// GenerateRefreshToken creates a refresh token from any struct that embeds jwt.RegisteredClaims
func (j *JWT) GenerateRefreshToken(claims jwt.Claims) (string, error) {
	return j.generate(claims, j.config.RefreshExpire)
}

// GenerateWithJTI creates a token with a specific JTI
func (j *JWT) GenerateWithJTI(claims jwt.Claims, jti string) (string, error) {
	if err := j.setJTI(claims, jti); err != nil {
		return "", err
	}
	return j.Generate(claims)
}

// GenerateRefreshTokenWithJTI creates a refresh token with a specific JTI
func (j *JWT) GenerateRefreshTokenWithJTI(claims jwt.Claims, jti string) (string, error) {
	if err := j.setJTI(claims, jti); err != nil {
		return "", err
	}
	return j.GenerateRefreshToken(claims)
}

// generate is the internal method that creates tokens
func (j *JWT) generate(claims jwt.Claims, expire int64) (string, error) {
	// Set standard claims if they are not already set
	if err := j.setStandardClaims(claims, expire); err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(j.config.signingMethod(), claims)
	return token.SignedString([]byte(j.config.Secret))
}

// Parse parses a JWT token and populates the provided claims struct
func (j *JWT) Parse(tokenString string, claims jwt.Claims) error {
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(j.config.Secret), nil
	})

	if err != nil {
		return err
	}

	if !token.Valid {
		return jwt.ErrTokenNotValidYet
	}

	return nil
}

// Validate validates a JWT token without parsing claims
func (j *JWT) Validate(tokenString string) error {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte(j.config.Secret), nil
	})

	if err != nil {
		return err
	}

	if !token.Valid {
		return jwt.ErrTokenNotValidYet
	}

	return nil
}

// GetJTI extracts JTI from claims
func (j *JWT) GetJTI(claims jwt.Claims) (string, error) {
	v := reflect.ValueOf(claims)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return "", errors.New("claims must be a struct")
	}

	// Find RegisteredClaims field
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Type() == reflect.TypeOf(jwt.RegisteredClaims{}) {
			registeredClaims := field.Interface().(jwt.RegisteredClaims)
			return registeredClaims.ID, nil
		}
	}

	return "", errors.New("claims struct must embed jwt.RegisteredClaims")
}

// setStandardClaims sets the standard JWT claims if they are not already set
func (j *JWT) setStandardClaims(claims jwt.Claims, expire int64) error {
	// Use reflection to access RegisteredClaims
	v := reflect.ValueOf(claims)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return errors.New("claims must be a struct")
	}

	// Find RegisteredClaims field
	var registeredClaims *jwt.RegisteredClaims
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Type() == reflect.TypeOf(jwt.RegisteredClaims{}) {
			if field.CanAddr() {
				registeredClaims = field.Addr().Interface().(*jwt.RegisteredClaims)
				break
			}
		}
	}

	if registeredClaims == nil {
		return errors.New("claims struct must embed jwt.RegisteredClaims")
	}

	now := time.Now()

	// Set expiration time
	if registeredClaims.ExpiresAt == nil {
		registeredClaims.ExpiresAt = jwt.NewNumericDate(now.Add(time.Duration(expire) * time.Second))
	}

	// Set issued at time
	if registeredClaims.IssuedAt == nil {
		if j.config.IssuedAt != 0 {
			registeredClaims.IssuedAt = jwt.NewNumericDate(now.Add(time.Duration(j.config.IssuedAt) * time.Second))
		} else {
			registeredClaims.IssuedAt = jwt.NewNumericDate(now)
		}
	}

	// Set not before time
	if registeredClaims.NotBefore == nil && j.config.NotBefore != 0 {
		registeredClaims.NotBefore = jwt.NewNumericDate(now.Add(time.Duration(j.config.NotBefore) * time.Second))
	}

	// Set other standard claims if not already set and config values exist
	if registeredClaims.Issuer == "" && j.config.Issuer != "" {
		registeredClaims.Issuer = j.config.Issuer
	}

	if registeredClaims.Subject == "" && j.config.Subject != "" {
		registeredClaims.Subject = j.config.Subject
	}

	if len(registeredClaims.Audience) == 0 && j.config.Audience != "" {
		registeredClaims.Audience = []string{j.config.Audience}
	}

	return nil
}

// setJTI sets the JTI in the RegisteredClaims
func (j *JWT) setJTI(claims jwt.Claims, jti string) error {
	v := reflect.ValueOf(claims)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return errors.New("claims must be a struct")
	}

	// Find RegisteredClaims field
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Type() == reflect.TypeOf(jwt.RegisteredClaims{}) {
			if field.CanAddr() {
				registeredClaims := field.Addr().Interface().(*jwt.RegisteredClaims)
				registeredClaims.ID = jti
				return nil
			}
		}
	}

	return errors.New("claims struct must embed jwt.RegisteredClaims")
}

// ParseClaims is a generic function to parse token into any claims struct
func ParseClaims[T jwt.Claims](j *JWT, tokenString string, claims T) (T, error) {
	err := j.Parse(tokenString, claims)
	return claims, err
}
