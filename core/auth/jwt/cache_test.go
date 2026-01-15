package jwt

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// TestUserClaims test claims implementation
type TestUserClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GetUserID implements ClaimsWithUserID interface
func (c *TestUserClaims) GetUserID() int64 {
	return c.UserID
}

// setupTestCache 设置测试缓存
func setupTestCache(t *testing.T) *JwtCache {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "12345678",
		DB:       1,
	})

	// 测试连接
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	jwtInstance, err := New(&Config{
		Secret: "test-secret",
		Expire: 3600,
	})
	if err != nil {
		t.Fatal("Failed to create JWT:", err)
	}

	return NewJwtCache(jwtInstance, rdb, WithKeyPrefix("test"))
}

func TestJwtCache_NewJwtCache(t *testing.T) {
	cache := setupTestCache(t)
	if cache == nil {
		return
	}

	if cache.keyPrefix != "test" {
		t.Errorf("Expected keyPrefix 'test', got %s", cache.keyPrefix)
	}
}

func TestJwtCache_Generate(t *testing.T) {
	cache := setupTestCache(t)
	if cache == nil {
		return
	}

	ctx := context.Background()
	claims := &TestUserClaims{
		UserID:   12345,
		Username: "testuser",
	}

	metadata := &TokenGenerationMetadata{
		DeviceID:  "device001",
		IPAddress: "192.168.1.1",
	}

	auth, err := cache.Generate(ctx, claims, metadata)
	if err != nil {
		t.Fatal("Failed to generate token:", err)
	}

	if auth.AccessToken == "" || auth.RefreshToken == "" {
		t.Error("Tokens should not be empty")
	}
}

func TestJwtCache_Parse(t *testing.T) {
	cache := setupTestCache(t)
	if cache == nil {
		return
	}

	ctx := context.Background()
	claims := &TestUserClaims{
		UserID:   12345,
		Username: "testuser",
	}

	metadata := &TokenGenerationMetadata{
		DeviceID:  "device001",
		IPAddress: "192.168.1.1",
	}

	// Generate token
	auth, err := cache.Generate(ctx, claims, metadata)
	if err != nil {
		t.Fatal("Failed to generate token:", err)
	}

	// Parse token
	var parsed TestUserClaims
	err = cache.Parse(ctx, &parsed, auth.AccessToken)
	if err != nil {
		t.Fatal("Failed to parse token:", err)
	}

	if parsed.UserID != claims.UserID {
		t.Errorf("Expected UserID %d, got %d", claims.UserID, parsed.UserID)
	}
}

func TestJwtCache_RevokeToken(t *testing.T) {
	cache := setupTestCache(t)
	if cache == nil {
		return
	}

	ctx := context.Background()
	claims := &TestUserClaims{
		UserID:   12345,
		Username: "testuser",
	}

	metadata := &TokenGenerationMetadata{
		DeviceID:  "device001",
		IPAddress: "192.168.1.1",
	}

	// Generate and revoke token
	auth, err := cache.Generate(ctx, claims, metadata)
	if err != nil {
		t.Fatal("Failed to generate token:", err)
	}

	err = cache.RevokeToken(ctx, auth.AccessToken, claims)
	if err != nil {
		t.Fatal("Failed to revoke token:", err)
	}

	// Try to parse revoked token
	var parsed TestUserClaims
	err = cache.Parse(ctx, &parsed, auth.AccessToken)
	if err == nil {
		t.Error("Expected error when parsing revoked token")
	}
}

func TestJwtCache_DeleteUserTokens(t *testing.T) {
	cache := setupTestCache(t)
	if cache == nil {
		return
	}

	ctx := context.Background()
	userID := int64(12345)
	claims := &TestUserClaims{
		UserID:   userID,
		Username: "testuser",
	}

	metadata := &TokenGenerationMetadata{
		DeviceID:  "device001",
		IPAddress: "192.168.1.1",
	}

	// Generate token
	auth, err := cache.Generate(ctx, claims, metadata)
	if err != nil {
		t.Fatal("Failed to generate token:", err)
	}

	// Delete all user tokens
	err = cache.DeleteUserTokens(ctx, userID)
	if err != nil {
		t.Fatal("Failed to delete user tokens:", err)
	}

	// Verify token is deleted
	var parsed TestUserClaims
	err = cache.Parse(ctx, &parsed, auth.AccessToken)
	if err == nil {
		t.Error("Expected error when parsing deleted token")
	}
}

func TestJwtCache_InvalidToken(t *testing.T) {
	cache := setupTestCache(t)
	if cache == nil {
		return
	}

	ctx := context.Background()
	var claims TestUserClaims

	// Test invalid token
	err := cache.Parse(ctx, &claims, "invalid.token.format")
	if err == nil {
		t.Error("Expected error for invalid token format")
	}
}
