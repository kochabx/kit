package jwt

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// UserClaims demonstrates the recommended way to define custom claims
// It embeds jwt.RegisteredClaims and implements ClaimsWithUserID
type UserClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// GetUserID implements ClaimsWithUserID interface
func (c *UserClaims) GetUserID() int64 {
	return c.UserID
}

func TestJWT_Basic(t *testing.T) {
	jwtInstance, err := New(&Config{
		Expire:        3600,
		RefreshExpire: 604800,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create user claims
	userClaims := &UserClaims{
		UserID:   12345,
		Username: "john_doe",
		Role:     "user",
		Email:    "john@example.com",
	}

	// Generate token
	token, err := jwtInstance.Generate(userClaims)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Generated Token:", token)

	// Parse token back
	var parsedClaims UserClaims
	err = jwtInstance.Parse(token, &parsedClaims)
	if err != nil {
		t.Fatal(err)
	}

	// Verify custom fields
	if parsedClaims.UserID != userClaims.UserID {
		t.Errorf("Expected UserID %d, got %d", userClaims.UserID, parsedClaims.UserID)
	}
	if parsedClaims.Username != userClaims.Username {
		t.Errorf("Expected Username %s, got %s", userClaims.Username, parsedClaims.Username)
	}

	t.Logf("Parsed Claims: %+v", parsedClaims)
}

// TestJWT_StronglyTyped demonstrates the strongly-typed approach
func TestJWT_StronglyTyped(t *testing.T) {
	t.Log("=== Demonstrating Strongly-Typed JWT Usage ===")

	// Create JWT instance
	jwtInstance, err := New(&Config{
		Expire:        3600,
		RefreshExpire: 604800,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create strongly-typed user claims
	originalClaims := &UserClaims{
		UserID:   98765,
		Username: "jane_doe",
		Role:     "admin",
		Email:    "jane@example.com",
	}

	t.Logf("Original Claims: %+v", originalClaims)

	// Generate tokens using strongly-typed claims
	accessToken, err := jwtInstance.Generate(originalClaims)
	if err != nil {
		t.Fatal(err)
	}

	refreshToken, err := jwtInstance.GenerateRefreshToken(originalClaims)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Access Token Generated Successfully")
	t.Log("Refresh Token Generated Successfully")

	// Parse tokens back into strongly-typed structs
	var accessClaims UserClaims
	err = jwtInstance.Parse(accessToken, &accessClaims)
	if err != nil {
		t.Fatal("Failed to parse access token:", err)
	}

	var refreshClaims UserClaims
	err = jwtInstance.Parse(refreshToken, &refreshClaims)
	if err != nil {
		t.Fatal("Failed to parse refresh token:", err)
	}

	// Verify type safety - all fields are strongly typed
	t.Logf("Parsed Access Claims - UserID: %d, Username: %s, Role: %s",
		accessClaims.UserID, accessClaims.Username, accessClaims.Role)
	t.Logf("Parsed Refresh Claims - UserID: %d, Email: %s",
		refreshClaims.UserID, refreshClaims.Email)

	// Type-safe comparisons
	if accessClaims.UserID != originalClaims.UserID {
		t.Errorf("UserID mismatch: expected %d, got %d",
			originalClaims.UserID, accessClaims.UserID)
	}

	if accessClaims.Username != originalClaims.Username {
		t.Errorf("Username mismatch: expected %s, got %s",
			originalClaims.Username, accessClaims.Username)
	}

	t.Log("✅ All strongly-typed validations passed!")
}
