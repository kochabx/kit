package jwt

import (
	"context"
	"testing"
)

// UserClaims 自定义 Claims 示例
type UserClaims struct {
	RegisteredClaims
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email"`
}

func TestBasicAuthenticator(t *testing.T) {
	ctx := context.Background()

	// 创建基础认证器
	auth, err := NewBasicAuthenticator(
		WithSecret("test-secret"),
		WithAccessTokenTTL(3600),
		WithRefreshTokenTTL(604800),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 创建 claims
	claims := &UserClaims{
		RegisteredClaims: RegisteredClaims{},
		UserID:           12345,
		Username:         "john_doe",
		Role:             "user",
		Email:            "john@example.com",
	}
	claims.Subject = "user123"

	// 生成 token 对
	tokenPair, err := auth.Generate(ctx, claims)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Access Token:", tokenPair.AccessToken)
	t.Log("Refresh Token:", tokenPair.RefreshToken)

	// 验证 access token
	verifiedClaims := &UserClaims{}
	err = auth.Verify(ctx, tokenPair.AccessToken, verifiedClaims)
	if err != nil {
		t.Fatal(err)
	}

	// 验证字段
	if verifiedClaims.UserID != claims.UserID {
		t.Errorf("Expected UserID %d, got %d", claims.UserID, verifiedClaims.UserID)
	}
	if verifiedClaims.Username != claims.Username {
		t.Errorf("Expected Username %s, got %s", claims.Username, verifiedClaims.Username)
	}

	t.Logf("Verified Claims: %+v", verifiedClaims)

	// 刷新 token
	refreshedClaims := &UserClaims{}
	err = auth.Verify(ctx, tokenPair.RefreshToken, refreshedClaims)
	if err != nil {
		t.Fatal(err)
	}

	newPair, err := auth.Refresh(ctx, tokenPair.RefreshToken, refreshedClaims)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("New Access Token:", newPair.AccessToken)
	t.Log("New Refresh Token:", newPair.RefreshToken)
}
