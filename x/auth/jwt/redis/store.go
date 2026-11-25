package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	kitjwt "github.com/kochabx/kit/x/auth/jwt"
)

// Lua scripts executed via EVALSHA (redis.NewScript) with cluster hash-tag locality on {subject_hash}.
const (
	createSessionLua = `
local ids = redis.call('ZRANGE', KEYS[2], 0, -1)
for _, id in ipairs(ids) do
  local key = ARGV[6] .. id
  local raw = redis.call('GET', key)
  if not raw then
    redis.call('ZREM', KEYS[2], id)
  else
    local s = cjson.decode(raw)
    if s.revoked_at_ms ~= nil or tonumber(s.expires_at_ms) <= tonumber(ARGV[8]) then
      redis.call('DEL', key)
      redis.call('ZREM', KEYS[2], id)
      if s.device_key and s.device_key ~= '' then
        redis.call('DEL', s.device_key)
      end
    end
  end
end
if ARGV[7] ~= '' then
  local old = redis.call('GET', KEYS[3])
  if old then
    redis.call('DEL', ARGV[6] .. old)
    redis.call('ZREM', KEYS[2], old)
  end
end
redis.call('SET', KEYS[1], ARGV[1], 'PXAT', ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
if redis.call('PTTL', KEYS[2]) < 0 then
  redis.call('PEXPIREAT', KEYS[2], ARGV[2])
else
  redis.call('PEXPIREAT', KEYS[2], ARGV[2], 'GT')
end
if ARGV[7] ~= '' then
  redis.call('SET', KEYS[3], ARGV[4], 'PXAT', ARGV[2])
end
local max = tonumber(ARGV[5])
if max > 0 then
  local excess = redis.call('ZCARD', KEYS[2]) - max
  if excess > 0 then
    local old = redis.call('ZRANGE', KEYS[2], 0, excess - 1)
    for _, id in ipairs(old) do
      local key = ARGV[6] .. id
      local raw = redis.call('GET', key)
      if raw then
        local s = cjson.decode(raw)
        if s.device_key and s.device_key ~= '' then
          redis.call('DEL', s.device_key)
        end
      end
      redis.call('DEL', key)
      redis.call('ZREM', KEYS[2], id)
    end
  end
end
return 1
`

	rotateSessionLua = `
local raw = redis.call('GET', KEYS[1])
if not raw then
  return 0
end
local s = cjson.decode(raw)
if s.revoked_at_ms ~= nil or tonumber(s.expires_at_ms) <= tonumber(ARGV[4]) then
  return -1
end
if s.refresh_jti ~= ARGV[1] then
  s.revoked_at_ms = tonumber(ARGV[4])
  s.status = 'revoked'
  redis.call('SET', KEYS[1], cjson.encode(s), 'KEEPTTL')
  return -2
end
s.refresh_jti = ARGV[2]
s.last_seen_at_ms = tonumber(ARGV[4])
s.expires_at_ms = tonumber(ARGV[3])
redis.call('SET', KEYS[1], cjson.encode(s), 'PXAT', ARGV[3])
if redis.call('PTTL', KEYS[2]) < 0 then
  redis.call('PEXPIREAT', KEYS[2], ARGV[3])
else
  redis.call('PEXPIREAT', KEYS[2], ARGV[3], 'GT')
end
if s.device_key and s.device_key ~= '' then
  redis.call('PEXPIREAT', s.device_key, ARGV[3])
end
return 1
`
)

var (
	createSessionScript = goredis.NewScript(createSessionLua)
	rotateSessionScript = goredis.NewScript(rotateSessionLua)

	revokeSessionScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then
  return 0
end
local s = cjson.decode(raw)
if s.revoked_at_ms == nil then
  s.revoked_at_ms = tonumber(ARGV[1])
  s.status = 'revoked'
  redis.call('SET', KEYS[1], cjson.encode(s), 'KEEPTTL')
end
return 1
`)

	revokeAllScript = goredis.NewScript(`
local ids = redis.call('ZRANGE', KEYS[1], 0, -1)
for _, id in ipairs(ids) do
  local key = ARGV[2] .. id
  local raw = redis.call('GET', key)
  if raw then
    local s = cjson.decode(raw)
    if s.revoked_at_ms == nil then
      s.revoked_at_ms = tonumber(ARGV[1])
      s.status = 'revoked'
      redis.call('SET', key, cjson.encode(s), 'KEEPTTL')
    end
  end
end
return #ids
`)

	revokeDeviceScript = goredis.NewScript(`
local id = redis.call('GET', KEYS[1])
if not id then
  return 0
end
local key = ARGV[2] .. id
local raw = redis.call('GET', key)
if not raw then
  redis.call('DEL', KEYS[1])
  return 0
end
local s = cjson.decode(raw)
if s.revoked_at_ms == nil then
  s.revoked_at_ms = tonumber(ARGV[1])
  s.status = 'revoked'
  redis.call('SET', key, cjson.encode(s), 'KEEPTTL')
end
return 1
`)
)

// Option configures a Redis JWT SessionStore.
type Option func(*Store)

// WithKeyPrefix sets the Redis key namespace prefix (default "jwt:").
func WithKeyPrefix(prefix string) Option {
	return func(s *Store) {
		s.prefix = prefix
	}
}

// Store implements kitjwt.SessionStore backed by redis.UniversalClient and EVALSHA scripts.
type Store struct {
	rdb    goredis.UniversalClient
	prefix string
}

var _ kitjwt.SessionStore = (*Store)(nil)

// New creates a Redis JWT SessionStore backed directly by any go-redis UniversalClient.
func New(rdb goredis.UniversalClient, opts ...Option) (*Store, error) {
	if rdb == nil {
		return nil, errors.New("jwt/redis: redis client is required")
	}
	s := &Store{
		rdb:    rdb,
		prefix: "jwt:",
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if strings.TrimSpace(s.prefix) == "" {
		return nil, errors.New("jwt/redis: key prefix cannot be empty")
	}
	return s, nil
}

func (s *Store) authKeys(subject, sid string) (sessionKey, indexKey, sessionPrefix string) {
	sum := sha256.Sum256([]byte(subject))
	tag := hex.EncodeToString(sum[:16])
	base := s.prefix + "{" + tag + "}:"
	return base + "session:" + sid, base + "sessions", base + "session:"
}

func (s *Store) deviceKey(subject, deviceID string) string {
	_, _, prefix := s.authKeys(subject, "")
	sum := sha256.Sum256([]byte(deviceID))
	return prefix + "device:" + hex.EncodeToString(sum[:16])
}

// Create stores a new active session and evicts excess older sessions per subject.
func (s *Store) Create(ctx context.Context, session kitjwt.Session, maxPerSubject int) error {
	invalid := session.ID == "" ||
		session.Subject == "" ||
		session.RefreshJTI == "" ||
		session.IssuedAtMs <= 0 ||
		session.LastSeenAtMs <= 0 ||
		session.ExpiresAtMs <= session.IssuedAtMs ||
		maxPerSubject < 0
	if invalid {
		return errors.New("jwt/redis: invalid session")
	}
	if session.Status == "" {
		session.Status = kitjwt.SessionActive
	}
	if session.Status != kitjwt.SessionActive {
		return errors.New("jwt/redis: session must be active on create")
	}

	key, index, prefix := s.authKeys(session.Subject, session.ID)
	deviceKey := prefix + "device:none:" + session.ID
	deviceArg := ""
	if session.DeviceID != "" {
		deviceKey = s.deviceKey(session.Subject, session.DeviceID)
		deviceArg = deviceKey
		session.DeviceKey = deviceKey
	}

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("jwt/redis: marshal session: %w", err)
	}

	return createSessionScript.Run(
		ctx,
		s.rdb,
		[]string{key, index, deviceKey},
		data,
		session.ExpiresAtMs,
		session.IssuedAtMs,
		session.ID,
		maxPerSubject,
		prefix,
		deviceArg,
		session.IssuedAtMs,
	).Err()
}

// Get fetches a session by subject and sessionID.
func (s *Store) Get(ctx context.Context, subject, sessionID string) (kitjwt.Session, error) {
	if subject == "" || sessionID == "" {
		return kitjwt.Session{}, errors.New("jwt/redis: subject and sessionID are required")
	}
	key, _, _ := s.authKeys(subject, sessionID)
	data, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return kitjwt.Session{}, kitjwt.ErrSessionNotFound
		}
		return kitjwt.Session{}, fmt.Errorf("jwt/redis: get session: %w", err)
	}
	var session kitjwt.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return kitjwt.Session{}, fmt.Errorf("jwt/redis: unmarshal session: %w", err)
	}
	return session, nil
}

// Rotate atomically swaps OldRefreshJTI for NewRefreshJTI, renews device_key, and advances ZSET expiry via GT.
func (s *Store) Rotate(ctx context.Context, r kitjwt.Rotation) (kitjwt.Session, error) {
	if r.Subject == "" || r.SessionID == "" || r.OldRefreshJTI == "" || r.NewRefreshJTI == "" ||
		r.Now.IsZero() || !r.ExpiresAt.After(r.Now) {
		return kitjwt.Session{}, errors.New("jwt/redis: invalid rotation")
	}
	key, index, _ := s.authKeys(r.Subject, r.SessionID)
	n, err := rotateSessionScript.Run(
		ctx,
		s.rdb,
		[]string{key, index},
		r.OldRefreshJTI,
		r.NewRefreshJTI,
		r.ExpiresAt.UnixMilli(),
		r.Now.UnixMilli(),
	).Int()
	if err != nil {
		return kitjwt.Session{}, fmt.Errorf("jwt/redis: rotate session: %w", err)
	}
	switch n {
	case 1:
		return s.Get(ctx, r.Subject, r.SessionID)
	case 0:
		return kitjwt.Session{}, kitjwt.ErrSessionNotFound
	case -1:
		return kitjwt.Session{}, kitjwt.ErrSessionRevoked
	case -2:
		return kitjwt.Session{}, kitjwt.ErrRefreshReused
	default:
		return kitjwt.Session{}, fmt.Errorf("jwt/redis: unexpected rotate result %d", n)
	}
}

// Revoke marks a single session revoked without removing audit state until expiry.
func (s *Store) Revoke(ctx context.Context, subject, sessionID string, at time.Time) error {
	if subject == "" || sessionID == "" || at.IsZero() {
		return errors.New("jwt/redis: invalid revoke arguments")
	}
	key, _, _ := s.authKeys(subject, sessionID)
	n, err := revokeSessionScript.Run(ctx, s.rdb, []string{key}, at.UnixMilli()).Int()
	if err != nil {
		return fmt.Errorf("jwt/redis: revoke session: %w", err)
	}
	if n == 0 {
		return kitjwt.ErrSessionNotFound
	}
	return nil
}

// RevokeDevice revokes the active session bound to deviceID.
func (s *Store) RevokeDevice(ctx context.Context, subject, deviceID string, at time.Time) error {
	if subject == "" || deviceID == "" || at.IsZero() {
		return errors.New("jwt/redis: invalid revoke device arguments")
	}
	_, _, prefix := s.authKeys(subject, "")
	dk := s.deviceKey(subject, deviceID)
	n, err := revokeDeviceScript.Run(ctx, s.rdb, []string{dk}, at.UnixMilli(), prefix).Int()
	if err != nil {
		return fmt.Errorf("jwt/redis: revoke device: %w", err)
	}
	if n == 0 {
		return kitjwt.ErrSessionNotFound
	}
	return nil
}

// RevokeAll marks every session belonging to subject revoked.
func (s *Store) RevokeAll(ctx context.Context, subject string, at time.Time) error {
	if subject == "" || at.IsZero() {
		return errors.New("jwt/redis: invalid revoke all arguments")
	}
	_, index, prefix := s.authKeys(subject, "")
	return revokeAllScript.Run(ctx, s.rdb, []string{index}, at.UnixMilli(), prefix).Err()
}

// IsRevoked reports whether the given session is revoked or no longer exists in Redis.
func (s *Store) IsRevoked(ctx context.Context, subject, _, sessionID string) (bool, error) {
	session, err := s.Get(ctx, subject, sessionID)
	if err != nil {
		if errors.Is(err, kitjwt.ErrSessionNotFound) {
			return true, nil
		}
		return false, err
	}
	return session.IsRevokedAt(time.Now().UTC()), nil
}

// ListSessions streams all stored sessions for subject via Go iter.Seq2.
func (s *Store) ListSessions(ctx context.Context, subject string) iter.Seq2[kitjwt.Session, error] {
	return func(yield func(kitjwt.Session, error) bool) {
		if subject == "" {
			yield(kitjwt.Session{}, errors.New("jwt/redis: subject is required"))
			return
		}
		_, index, _ := s.authKeys(subject, "")
		ids, err := s.rdb.ZRange(ctx, index, 0, -1).Result()
		if err != nil {
			yield(kitjwt.Session{}, fmt.Errorf("jwt/redis: list session ids: %w", err))
			return
		}
		if len(ids) == 0 {
			return
		}
		keys := make([]string, len(ids))
		for i, id := range ids {
			keys[i], _, _ = s.authKeys(subject, id)
		}
		values, err := s.rdb.MGet(ctx, keys...).Result()
		if err != nil {
			yield(kitjwt.Session{}, fmt.Errorf("jwt/redis: load sessions: %w", err))
			return
		}
		var stale []any
		for i, val := range values {
			if val == nil {
				stale = append(stale, ids[i])
				continue
			}
			raw, ok := val.(string)
			if !ok {
				yield(kitjwt.Session{}, errors.New("jwt/redis: unexpected non-string redis value"))
				return
			}
			var session kitjwt.Session
			if err := json.Unmarshal([]byte(raw), &session); err != nil {
				yield(kitjwt.Session{}, fmt.Errorf("jwt/redis: unmarshal session: %w", err))
				return
			}
			if !yield(session, nil) {
				return
			}
		}
		if len(stale) > 0 {
			_ = s.rdb.ZRem(ctx, index, stale...).Err()
		}
	}
}
