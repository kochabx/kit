package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	kitjwt "github.com/kochabx/kit/core/auth/jwt"
	kitredis "github.com/kochabx/kit/store/redis"
	goredis "github.com/redis/go-redis/v9"
)

type Store struct {
	client *kitredis.Client
	prefix string
}
type Option func(*Store)

func WithKeyPrefix(prefix string) Option { return func(s *Store) { s.prefix = prefix } }
func New(client *kitredis.Client, options ...Option) (*Store, error) {
	if client == nil || client.UniversalClient() == nil {
		return nil, kitjwt.ErrConfigInvalid
	}
	s := &Store{client: client, prefix: "jwt:"}
	for _, option := range options {
		option(s)
	}
	if s.prefix == "" {
		return nil, kitjwt.ErrConfigInvalid
	}
	return s, nil
}

const createSessionScript = `
local ids=redis.call('ZRANGE',KEYS[2],0,-1)
for _,id in ipairs(ids) do
 local key=ARGV[6]..id;local raw=redis.call('GET',key)
 if not raw then redis.call('ZREM',KEYS[2],id) else local s=cjson.decode(raw);if s.revoked_at_unix~=nil or tonumber(s.expires_at_unix)<=tonumber(ARGV[8]) then redis.call('DEL',key);redis.call('ZREM',KEYS[2],id);if s.device_key and s.device_key~='' then redis.call('DEL',s.device_key) end end end
end
if ARGV[7]~='' then
 local old=redis.call('GET',KEYS[3]);if old then redis.call('DEL',ARGV[6]..old);redis.call('ZREM',KEYS[2],old) end
end
redis.call('SET', KEYS[1], ARGV[1], 'PXAT', ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('PEXPIREAT', KEYS[2], ARGV[2])
if ARGV[7]~='' then redis.call('SET',KEYS[3],ARGV[4],'PXAT',ARGV[2]) end
local max=tonumber(ARGV[5])
if max>0 then
 local excess=redis.call('ZCARD',KEYS[2])-max
 if excess>0 then
  local old=redis.call('ZRANGE',KEYS[2],0,excess-1)
  for _,id in ipairs(old) do local key=ARGV[6]..id;local raw=redis.call('GET',key);if raw then local s=cjson.decode(raw);if s.device_key and s.device_key~='' then redis.call('DEL',s.device_key) end end;redis.call('DEL',key);redis.call('ZREM',KEYS[2],id) end
 end
end
return 1`

const rotateSessionScript = `
local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end
local s=cjson.decode(raw)
if s.revoked_at_unix~=nil or tonumber(s.expires_at_unix)<=tonumber(ARGV[4]) then return -1 end
if s.refresh_jti~=ARGV[1] then s.revoked_at_unix=tonumber(ARGV[4]);s.status='revoked';redis.call('SET',KEYS[1],cjson.encode(s),'KEEPTTL');return -2 end
s.refresh_jti=ARGV[2];s.last_seen_at_unix=tonumber(ARGV[4]);s.expires_at_unix=tonumber(ARGV[3]);redis.call('SET',KEYS[1],cjson.encode(s),'PXAT',ARGV[3]);redis.call('PEXPIREAT',KEYS[2],ARGV[3]);return 1`

const revokeSessionScript = `local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end;local s=cjson.decode(raw);if s.revoked_at_unix==nil then s.revoked_at_unix=tonumber(ARGV[1]);s.status='revoked';redis.call('SET',KEYS[1],cjson.encode(s),'KEEPTTL') end;return 1`
const revokeAllScript = `local ids=redis.call('ZRANGE',KEYS[1],0,-1);for _,id in ipairs(ids) do local key=ARGV[2]..id;local raw=redis.call('GET',key);if raw then local s=cjson.decode(raw);if s.revoked_at_unix==nil then s.revoked_at_unix=tonumber(ARGV[1]);s.status='revoked';redis.call('SET',key,cjson.encode(s),'KEEPTTL') end end end;return #ids`
const revokeDeviceScript = `local id=redis.call('GET',KEYS[1]);if not id then return 0 end;local key=ARGV[2]..id;local raw=redis.call('GET',key);if not raw then redis.call('DEL',KEYS[1]);return 0 end;local s=cjson.decode(raw);if s.revoked_at_unix==nil then s.revoked_at_unix=tonumber(ARGV[1]);s.status='revoked';redis.call('SET',key,cjson.encode(s),'KEEPTTL') end;return 1`

type redisSession struct {
	kitjwt.Session
	CreatedAtUnix  int64  `json:"created_at_unix"`
	LastSeenAtUnix int64  `json:"last_seen_at_unix"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
	RevokedAtUnix  *int64 `json:"revoked_at_unix,omitempty"`
	DeviceKey      string `json:"device_key,omitempty"`
}

func encodeSession(s kitjwt.Session) ([]byte, error) {
	v := redisSession{
		Session:        s,
		CreatedAtUnix:  s.CreatedAt.UnixMilli(),
		LastSeenAtUnix: s.LastSeenAt.UnixMilli(),
		ExpiresAtUnix:  s.ExpiresAt.UnixMilli(),
	}
	if s.RevokedAt != nil {
		x := s.RevokedAt.UnixMilli()
		v.RevokedAtUnix = &x
	}
	return json.Marshal(v)
}

func decodeSession(data []byte) (*kitjwt.Session, error) {
	var v redisSession
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	v.CreatedAt = time.UnixMilli(v.CreatedAtUnix)
	v.LastSeenAt = time.UnixMilli(v.LastSeenAtUnix)
	v.ExpiresAt = time.UnixMilli(v.ExpiresAtUnix)
	if v.RevokedAtUnix != nil {
		x := time.UnixMilli(*v.RevokedAtUnix)
		v.RevokedAt = &x
	}
	return &v.Session, nil
}

func (s *Store) authKeys(subject, sid string) (string, string, string) {
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

func (s *Store) Create(ctx context.Context, session kitjwt.Session, maxSessions int) error {
	invalid := session.ID == "" ||
		session.Subject == "" ||
		session.RefreshJTI == "" ||
		session.CreatedAt.IsZero() ||
		session.LastSeenAt.IsZero() ||
		!session.ExpiresAt.After(session.CreatedAt) ||
		maxSessions <= 0 || maxSessions > 100
	if invalid {
		return kitjwt.ErrInvalidSession
	}
	if session.Status == "" {
		session.Status = kitjwt.SessionActive
	}
	if session.Status != kitjwt.SessionActive {
		return kitjwt.ErrInvalidSession
	}
	data, err := encodeSession(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	key, index, prefix := s.authKeys(session.Subject, session.ID)
	deviceKey := prefix + "device:none:" + session.ID
	deviceArg := ""
	if session.DeviceID != "" {
		deviceKey = s.deviceKey(session.Subject, session.DeviceID)
		deviceArg = deviceKey
		var stored redisSession
		if err := json.Unmarshal(data, &stored); err != nil {
			return err
		}
		stored.DeviceKey = deviceKey
		data, err = json.Marshal(stored)
		if err != nil {
			return err
		}
	}
	return s.client.UniversalClient().Eval(
		ctx,
		createSessionScript,
		[]string{key, index, deviceKey},
		data,
		session.ExpiresAt.UnixMilli(),
		session.CreatedAt.UnixMilli(),
		session.ID,
		maxSessions,
		prefix,
		deviceArg,
		session.CreatedAt.UnixMilli(),
	).Err()
}

func (s *Store) Get(ctx context.Context, subject, sid string) (*kitjwt.Session, error) {
	if subject == "" || sid == "" {
		return nil, kitjwt.ErrInvalidSession
	}
	key, _, _ := s.authKeys(subject, sid)
	data, err := s.client.UniversalClient().Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, kitjwt.ErrSessionNotFound
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	v, err := decodeSession(data)
	if err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return v, nil
}

func (s *Store) Rotate(ctx context.Context, r kitjwt.Rotation) error {
	if r.Subject == "" || r.SessionID == "" || r.OldJTI == "" || r.NewJTI == "" ||
		r.Now.IsZero() || !r.ExpiresAt.After(r.Now) {
		return kitjwt.ErrInvalidSession
	}
	key, index, _ := s.authKeys(r.Subject, r.SessionID)
	n, err := s.client.UniversalClient().Eval(
		ctx,
		rotateSessionScript,
		[]string{key, index},
		r.OldJTI,
		r.NewJTI,
		r.ExpiresAt.UnixMilli(),
		r.Now.UnixMilli(),
	).Int()
	if err != nil {
		return err
	}
	switch n {
	case 1:
		return nil
	case 0:
		return kitjwt.ErrSessionNotFound
	case -1:
		return kitjwt.ErrSessionRevoked
	case -2:
		return kitjwt.ErrRefreshReused
	}
	return kitjwt.ErrInvalidSession
}
func (s *Store) Revoke(ctx context.Context, subject, sid string, now time.Time) error {
	if subject == "" || sid == "" || now.IsZero() {
		return kitjwt.ErrInvalidSession
	}
	key, _, _ := s.authKeys(subject, sid)
	n, err := s.client.UniversalClient().Eval(ctx, revokeSessionScript, []string{key}, now.UnixMilli()).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return kitjwt.ErrSessionNotFound
	}
	return nil
}
func (s *Store) RevokeAll(ctx context.Context, subject string, now time.Time) error {
	if subject == "" || now.IsZero() {
		return kitjwt.ErrInvalidSession
	}
	_, index, prefix := s.authKeys(subject, "")
	return s.client.UniversalClient().Eval(ctx, revokeAllScript, []string{index}, now.UnixMilli(), prefix).Err()
}
func (s *Store) RevokeDevice(ctx context.Context, subject, deviceID string, now time.Time) error {
	if subject == "" || deviceID == "" || now.IsZero() {
		return kitjwt.ErrInvalidSession
	}
	_, _, prefix := s.authKeys(subject, "")
	deviceKey := s.deviceKey(subject, deviceID)
	n, err := s.client.UniversalClient().Eval(ctx, revokeDeviceScript, []string{deviceKey}, now.UnixMilli(), prefix).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return kitjwt.ErrSessionNotFound
	}
	return nil
}
func (s *Store) List(ctx context.Context, subject string, query kitjwt.SessionQuery) (kitjwt.SessionPage, error) {
	if subject == "" ||
		(query.Status != "" && query.Status != kitjwt.SessionActive && query.Status != kitjwt.SessionRevoked) {
		return kitjwt.SessionPage{}, kitjwt.ErrInvalidSession
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	_, index, _ := s.authKeys(subject, "")
	ids, err := s.client.UniversalClient().ZRange(ctx, index, 0, -1).Result()
	if err != nil {
		return kitjwt.SessionPage{}, err
	}
	if len(ids) == 0 {
		return kitjwt.SessionPage{Sessions: []kitjwt.Session{}}, nil
	}
	start := 0
	if query.Cursor != "" {
		start = -1
		for i, id := range ids {
			if id == query.Cursor {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return kitjwt.SessionPage{}, kitjwt.ErrInvalidSession
		}
	}
	if start >= len(ids) {
		return kitjwt.SessionPage{Sessions: []kitjwt.Session{}}, nil
	}
	candidates := ids[start:]
	keys := make([]string, len(candidates))
	for i, id := range candidates {
		keys[i], _, _ = s.authKeys(subject, id)
	}
	values, err := s.client.UniversalClient().MGet(ctx, keys...).Result()
	if err != nil {
		return kitjwt.SessionPage{}, err
	}
	out := make([]kitjwt.Session, 0, min(limit, len(candidates)))
	stale := make([]any, 0)
	next := ""
	for i, value := range values {
		if value == nil {
			stale = append(stale, candidates[i])
			continue
		}
		raw, ok := value.(string)
		if !ok {
			return kitjwt.SessionPage{}, fmt.Errorf("decode session: unexpected Redis value")
		}
		session, err := decodeSession([]byte(raw))
		if err != nil {
			return kitjwt.SessionPage{}, fmt.Errorf("decode session: %w", err)
		}
		if query.Status == "" || session.Status == query.Status {
			out = append(out, *session)
			if len(out) == limit {
				if i < len(candidates)-1 {
					next = candidates[i]
				}
				break
			}
		}
	}
	if len(stale) > 0 {
		_ = s.client.UniversalClient().ZRem(ctx, index, stale...).Err()
	}
	return kitjwt.SessionPage{Sessions: out, NextCursor: next}, nil
}

var _ kitjwt.SessionStore = (*Store)(nil)
