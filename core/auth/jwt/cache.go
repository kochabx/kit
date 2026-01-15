package jwt

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ClaimsWithUserID 定义了一个接口，用于获取用户ID
type ClaimsWithUserID interface {
	jwt.Claims
	GetUserID() int64
}

// Auth 代表用户的认证信息
type Auth struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// TokenMetadata 存储token的元数据信息
type TokenMetadata struct {
	JTI       string `json:"jti"`
	TokenType string `json:"token_type"` // "access" or "refresh"
	CreatedAt int64  `json:"created_at"` // Unix timestamp
	ExpiresAt int64  `json:"expires_at"` // Unix timestamp
	DeviceID  string `json:"device_id,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// JwtCache JWT缓存
type JwtCache struct {
	jwt   *JWT
	redis *redis.Client
	// Redis key前缀
	keyPrefix string
	// 使用Hash结构存储用户的所有token
	userTokensPrefix string
	// 使用String结构存储JTI到用户ID的映射，用于快速反查
	jtiMappingPrefix string
	// 是否启用多点登录
	multiLogin bool
	// 最大设备数限制（0表示无限制）
	maxDevices int
}

// JwtCacheOption 配置选项
type JwtCacheOption func(*JwtCache)

// WithKeyPrefix 设置Redis key前缀
func WithKeyPrefix(prefix string) JwtCacheOption {
	return func(j *JwtCache) {
		j.keyPrefix = prefix
		j.userTokensPrefix = prefix + ":user_tokens"
		j.jtiMappingPrefix = prefix + ":jti_mapping"
	}
}

// WithMultiLogin 启用多点登录
func WithMultiLogin(enabled bool) JwtCacheOption {
	return func(j *JwtCache) {
		j.multiLogin = enabled
	}
}

// WithMaxDevices 设置最大设备数限制
func WithMaxDevices(max int) JwtCacheOption {
	return func(j *JwtCache) {
		j.maxDevices = max
	}
}

// NewJwtCache 创建JWT缓存实例
func NewJwtCache(jwt *JWT, redisClient *redis.Client, opts ...JwtCacheOption) *JwtCache {
	cache := &JwtCache{
		jwt:              jwt,
		redis:            redisClient,
		keyPrefix:        "jwt",
		userTokensPrefix: "jwt:user_tokens",
		jtiMappingPrefix: "jwt:jti_mapping",
		multiLogin:       false,
		maxDevices:       0, // 默认无限制
	}

	for _, opt := range opts {
		opt(cache)
	}

	return cache
}

// extractJTI 从claims中提取JTI
func (j *JwtCache) extractJTI(claims jwt.Claims) (string, error) {
	// Use the JWT instance's GetJTI method which handles reflection
	return j.jwt.GetJTI(claims)
}

// buildUserTokensKey 构建用户token的Hash key
func (j *JwtCache) buildUserTokensKey(userID int64) string {
	return fmt.Sprintf("%s:%d", j.userTokensPrefix, userID)
}

// buildJTIMappingKey 构建JTI映射的key
func (j *JwtCache) buildJTIMappingKey(jti string) string {
	return fmt.Sprintf("%s:%s", j.jtiMappingPrefix, jti)
}

// Generate 生成token对并缓存
func (j *JwtCache) Generate(ctx context.Context, claims ClaimsWithUserID, metadata *TokenGenerationMetadata) (*Auth, error) {
	userID := claims.GetUserID()

	// 生成JTI
	accessJTI := uuid.New().String()
	refreshJTI := uuid.New().String()

	// 生成token
	accessToken, err := j.jwt.GenerateWithJTI(claims, accessJTI)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := j.jwt.GenerateRefreshTokenWithJTI(claims, refreshJTI)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	// 解析token获取过期时间
	if err := j.jwt.Parse(accessToken, claims); err != nil {
		return nil, fmt.Errorf("parse access token for TTL: %w", err)
	}
	accessExpTime, _ := claims.GetExpirationTime()

	tempClaims := claims
	if err := j.jwt.Parse(refreshToken, tempClaims); err != nil {
		return nil, fmt.Errorf("parse refresh token for TTL: %w", err)
	}
	refreshExpTime, _ := tempClaims.GetExpirationTime()

	now := time.Now()

	// 创建token元数据
	accessMeta := &TokenMetadata{
		JTI:       accessJTI,
		TokenType: "access",
		CreatedAt: now.Unix(),
		ExpiresAt: accessExpTime.Time.Unix(),
		DeviceID:  metadata.DeviceID,
		IPAddress: metadata.IPAddress,
		UserAgent: metadata.UserAgent,
	}

	refreshMeta := &TokenMetadata{
		JTI:       refreshJTI,
		TokenType: "refresh",
		CreatedAt: now.Unix(),
		ExpiresAt: refreshExpTime.Time.Unix(),
		DeviceID:  metadata.DeviceID,
		IPAddress: metadata.IPAddress,
		UserAgent: metadata.UserAgent,
	}

	// 缓存token
	if err := j.cacheTokens(ctx, userID, accessMeta, refreshMeta); err != nil {
		return nil, fmt.Errorf("cache tokens: %w", err)
	}

	return &Auth{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// TokenGenerationMetadata token生成时的元数据
type TokenGenerationMetadata struct {
	DeviceID  string
	IPAddress string
	UserAgent string
}

// cacheTokens 缓存token
func (j *JwtCache) cacheTokens(ctx context.Context, userID int64, accessMeta, refreshMeta *TokenMetadata) error {
	pipe := j.redis.Pipeline()

	userTokensKey := j.buildUserTokensKey(userID)

	// 序列化元数据
	accessMetaJSON, _ := json.Marshal(accessMeta)
	refreshMetaJSON, _ := json.Marshal(refreshMeta)

	if j.multiLogin {
		// 检查最大设备数限制
		if j.maxDevices > 0 {
			if err := j.enforceMaxDevicesLimit(ctx, userID, accessMeta.DeviceID); err != nil {
				return fmt.Errorf("enforce max devices limit: %w", err)
			}
		}

		// 多点登录：使用Hash存储，field为sessionID（deviceID+timestamp），value为token元数据
		sessionID := fmt.Sprintf("%s_%d", accessMeta.DeviceID, accessMeta.CreatedAt)

		// 使用Hash的嵌套结构存储access和refresh token
		sessionData := map[string]any{
			"access_token":  string(accessMetaJSON),
			"refresh_token": string(refreshMetaJSON),
			"created_at":    accessMeta.CreatedAt,
			"device_id":     accessMeta.DeviceID,
			"ip_address":    accessMeta.IPAddress,
		}

		sessionDataJSON, _ := json.Marshal(sessionData)
		pipe.HSet(ctx, userTokensKey, sessionID, sessionDataJSON)
		pipe.Expire(ctx, userTokensKey, time.Until(time.Unix(refreshMeta.ExpiresAt, 0)))

		// 建立JTI到用户ID和会话ID的映射，用于快速查找
		pipe.Set(ctx, j.buildJTIMappingKey(accessMeta.JTI), fmt.Sprintf("%d:%s", userID, sessionID), time.Until(time.Unix(accessMeta.ExpiresAt, 0)))
		pipe.Set(ctx, j.buildJTIMappingKey(refreshMeta.JTI), fmt.Sprintf("%d:%s", userID, sessionID), time.Until(time.Unix(refreshMeta.ExpiresAt, 0)))

	} else {
		// 单点登录：直接存储在Hash中
		pipe.HSet(ctx, userTokensKey, "access_token", string(accessMetaJSON))
		pipe.HSet(ctx, userTokensKey, "refresh_token", string(refreshMetaJSON))
		pipe.Expire(ctx, userTokensKey, time.Until(time.Unix(refreshMeta.ExpiresAt, 0)))

		// JTI映射
		pipe.Set(ctx, j.buildJTIMappingKey(accessMeta.JTI), strconv.FormatInt(userID, 10), time.Until(time.Unix(accessMeta.ExpiresAt, 0)))
		pipe.Set(ctx, j.buildJTIMappingKey(refreshMeta.JTI), strconv.FormatInt(userID, 10), time.Until(time.Unix(refreshMeta.ExpiresAt, 0)))
	}

	_, err := pipe.Exec(ctx)
	return err
}

// Parse token解析和验证
func (j *JwtCache) Parse(ctx context.Context, claimsPtr ClaimsWithUserID, tokens ...string) error {
	if len(tokens) == 0 {
		return fmt.Errorf("no tokens provided")
	}

	// 解析第一个token
	if err := j.jwt.Parse(tokens[0], claimsPtr); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}

	// 提取JTI
	jti, err := j.extractJTI(claimsPtr)
	if err != nil {
		return err
	}

	// 通过JTI映射快速查找用户信息
	_, err = j.redis.Get(ctx, j.buildJTIMappingKey(jti)).Result()
	if err == redis.Nil {
		return jwt.ErrTokenExpired
	} else if err != nil {
		return fmt.Errorf("get JTI mapping: %w", err)
	}

	// 验证其他token（如果有的话）
	for i, token := range tokens[1:] {
		tempClaims := &jwt.MapClaims{}
		if err := j.jwt.Parse(token, tempClaims); err != nil {
			return fmt.Errorf("parse token %d: %w", i+1, err)
		}

		tempJTI, exists := (*tempClaims)["jti"]
		if !exists {
			return fmt.Errorf("JTI not found in token %d", i+1)
		}

		jtiStr, ok := tempJTI.(string)
		if !ok {
			return fmt.Errorf("invalid JTI type in token %d", i+1)
		}

		// 验证JTI映射是否存在
		_, err := j.redis.Get(ctx, j.buildJTIMappingKey(jtiStr)).Result()
		if err == redis.Nil {
			return jwt.ErrTokenExpired
		} else if err != nil {
			return fmt.Errorf("get JTI mapping for token %d: %w", i+1, err)
		}
	}

	return nil
}

// GetUserTokenMetadata 获取用户所有token的元数据
func (j *JwtCache) GetUserTokenMetadata(ctx context.Context, userID int64) ([]TokenMetadata, error) {
	userTokensKey := j.buildUserTokensKey(userID)

	if j.multiLogin {
		// 获取所有会话
		sessions, err := j.redis.HGetAll(ctx, userTokensKey).Result()
		if err != nil {
			return nil, fmt.Errorf("get user sessions: %w", err)
		}

		var metadata []TokenMetadata
		for _, sessionData := range sessions {
			var session map[string]any
			if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
				continue
			}

			// 解析access token元数据
			if accessTokenJSON, exists := session["access_token"]; exists {
				var accessMeta TokenMetadata
				if err := json.Unmarshal([]byte(accessTokenJSON.(string)), &accessMeta); err == nil {
					metadata = append(metadata, accessMeta)
				}
			}

			// 解析refresh token元数据
			if refreshTokenJSON, exists := session["refresh_token"]; exists {
				var refreshMeta TokenMetadata
				if err := json.Unmarshal([]byte(refreshTokenJSON.(string)), &refreshMeta); err == nil {
					metadata = append(metadata, refreshMeta)
				}
			}
		}

		return metadata, nil

	} else {
		// 单点登录模式
		tokenData, err := j.redis.HMGet(ctx, userTokensKey, "access_token", "refresh_token").Result()
		if err != nil {
			return nil, fmt.Errorf("get tokens: %w", err)
		}

		var metadata []TokenMetadata

		for _, data := range tokenData {
			if data != nil {
				var meta TokenMetadata
				if err := json.Unmarshal([]byte(data.(string)), &meta); err == nil {
					metadata = append(metadata, meta)
				}
			}
		}

		return metadata, nil
	}
}

// DeleteUserTokens 删除用户所有token
func (j *JwtCache) DeleteUserTokens(ctx context.Context, userID int64) error {
	// 首先获取所有JTI用于清理映射
	metadata, err := j.GetUserTokenMetadata(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user token metadata: %w", err)
	}

	pipe := j.redis.Pipeline()

	// 删除用户token Hash
	pipe.Del(ctx, j.buildUserTokensKey(userID))

	// 删除JTI映射
	for _, meta := range metadata {
		pipe.Del(ctx, j.buildJTIMappingKey(meta.JTI))
	}

	_, err = pipe.Exec(ctx)
	return err
}

// RevokeToken token撤销
func (j *JwtCache) RevokeToken(ctx context.Context, tokenString string, claimsPtr ClaimsWithUserID) error {
	if err := j.jwt.Parse(tokenString, claimsPtr); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}

	userID := claimsPtr.GetUserID()
	jti, err := j.extractJTI(claimsPtr)
	if err != nil {
		return err
	}

	// 通过JTI映射找到会话信息
	mapping, err := j.redis.Get(ctx, j.buildJTIMappingKey(jti)).Result()
	if err == redis.Nil {
		return nil // token已经不存在
	} else if err != nil {
		return fmt.Errorf("get JTI mapping: %w", err)
	}

	pipe := j.redis.Pipeline()

	if j.multiLogin {
		// 解析映射信息：userID:sessionID
		parts := strings.SplitN(mapping, ":", 2)
		if len(parts) == 2 {
			sessionID := parts[1]
			// 删除整个会话
			pipe.HDel(ctx, j.buildUserTokensKey(userID), sessionID)
		}
	} else {
		// 单点登录：需要确定是access还是refresh token
		metadata, _ := j.GetUserTokenMetadata(ctx, userID)
		for _, meta := range metadata {
			if meta.JTI == jti {
				if meta.TokenType == "access" {
					pipe.HDel(ctx, j.buildUserTokensKey(userID), "access_token")
				} else {
					pipe.HDel(ctx, j.buildUserTokensKey(userID), "refresh_token")
				}
				break
			}
		}
	}

	// 删除JTI映射
	pipe.Del(ctx, j.buildJTIMappingKey(jti))

	_, err = pipe.Exec(ctx)
	return err
}

// GetActiveSessionCount 获取活跃会话数量
func (j *JwtCache) GetActiveSessionCount(ctx context.Context, userID int64) (int64, error) {
	userTokensKey := j.buildUserTokensKey(userID)

	if j.multiLogin {
		return j.redis.HLen(ctx, userTokensKey).Result()
	}

	// 单点登录模式，检查是否存在token
	exists, err := j.redis.Exists(ctx, userTokensKey).Result()
	return exists, err
}

// CleanExpiredTokens 清理过期token（可以通过定时任务调用）
func (j *JwtCache) CleanExpiredTokens(ctx context.Context) error {
	// 这里可以实现基于Redis KEY过期的清理逻辑
	// 由于我们设置了合适的TTL，Redis会自动清理过期的key
	// 这个方法可以用于额外的清理逻辑，比如清理孤立的映射等
	return nil
}

// enforceMaxDevicesLimit 强制执行最大设备数限制
func (j *JwtCache) enforceMaxDevicesLimit(ctx context.Context, userID int64, newDeviceID string) error {
	if j.maxDevices <= 0 {
		return nil // 无限制
	}

	userTokensKey := j.buildUserTokensKey(userID)
	sessions, err := j.redis.HGetAll(ctx, userTokensKey).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("get user sessions: %w", err)
	}

	// 统计不同设备的数量
	deviceSessions := make(map[string][]string) // deviceID -> sessionIDs
	for sessionID, sessionData := range sessions {
		var session map[string]any
		if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
			continue
		}

		if deviceID, exists := session["device_id"]; exists {
			deviceIDStr := deviceID.(string)
			deviceSessions[deviceIDStr] = append(deviceSessions[deviceIDStr], sessionID)
		}
	}

	// 如果新设备已存在，则不需要检查限制
	if _, exists := deviceSessions[newDeviceID]; exists {
		return nil
	}

	// 检查是否超过最大设备数
	if len(deviceSessions) >= j.maxDevices {
		// 找到最旧的设备会话进行删除
		oldestDevice := ""
		oldestTime := time.Now().Unix()

		for deviceID, sessionIDs := range deviceSessions {
			for _, sessionID := range sessionIDs {
				sessionData := sessions[sessionID]
				var session map[string]any
				if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
					continue
				}

				if createdAt, exists := session["created_at"]; exists {
					if timestamp, ok := createdAt.(float64); ok && int64(timestamp) < oldestTime {
						oldestTime = int64(timestamp)
						oldestDevice = deviceID
					}
				}
			}
		}

		// 删除最旧设备的所有会话
		if oldestDevice != "" {
			if err := j.removeDeviceSessions(ctx, userID, oldestDevice, deviceSessions[oldestDevice]); err != nil {
				return fmt.Errorf("remove oldest device sessions: %w", err)
			}
		}
	}

	return nil
}

// removeDeviceSessions 删除指定设备的所有会话
func (j *JwtCache) removeDeviceSessions(ctx context.Context, userID int64, deviceID string, sessionIDs []string) error {
	userTokensKey := j.buildUserTokensKey(userID)
	pipe := j.redis.Pipeline()

	// 获取会话数据以提取JTI进行清理
	for _, sessionID := range sessionIDs {
		sessionData, err := j.redis.HGet(ctx, userTokensKey, sessionID).Result()
		if err != nil {
			continue
		}

		var session map[string]any
		if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
			continue
		}

		// 清理access token的JTI映射
		if accessTokenJSON, exists := session["access_token"]; exists {
			var accessMeta TokenMetadata
			if err := json.Unmarshal([]byte(accessTokenJSON.(string)), &accessMeta); err == nil {
				pipe.Del(ctx, j.buildJTIMappingKey(accessMeta.JTI))
			}
		}

		// 清理refresh token的JTI映射
		if refreshTokenJSON, exists := session["refresh_token"]; exists {
			var refreshMeta TokenMetadata
			if err := json.Unmarshal([]byte(refreshTokenJSON.(string)), &refreshMeta); err == nil {
				pipe.Del(ctx, j.buildJTIMappingKey(refreshMeta.JTI))
			}
		}

		// 删除会话
		pipe.HDel(ctx, userTokensKey, sessionID)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// GetActiveDevicesCount 获取活跃设备数量
func (j *JwtCache) GetActiveDevicesCount(ctx context.Context, userID int64) (int, error) {
	if !j.multiLogin {
		// 单点登录模式，最多1个设备
		exists, err := j.redis.Exists(ctx, j.buildUserTokensKey(userID)).Result()
		if err != nil {
			return 0, err
		}
		return int(exists), nil
	}

	userTokensKey := j.buildUserTokensKey(userID)
	sessions, err := j.redis.HGetAll(ctx, userTokensKey).Result()
	if err != nil {
		return 0, fmt.Errorf("get user sessions: %w", err)
	}

	deviceSet := make(map[string]bool)
	for _, sessionData := range sessions {
		var session map[string]any
		if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
			continue
		}

		if deviceID, exists := session["device_id"]; exists {
			deviceSet[deviceID.(string)] = true
		}
	}

	return len(deviceSet), nil
}

// GetMaxDevicesLimit 获取最大设备数限制
func (j *JwtCache) GetMaxDevicesLimit() int {
	return j.maxDevices
}
