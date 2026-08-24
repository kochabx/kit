package redis_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kitjwt "github.com/kochabx/kit/core/auth/jwt"
	jwtredis "github.com/kochabx/kit/core/auth/jwt/store/redis"
	kitredis "github.com/kochabx/kit/store/redis"
	"github.com/stretchr/testify/require"
)

func redisStore(t *testing.T) *jwtredis.Store {
	t.Helper()
	addr := os.Getenv("KIT_JWT_TEST_REDIS")
	if addr == "" {
		t.Skip("KIT_JWT_TEST_REDIS is not set")
	}
	config := kitredis.Single(addr)
	config.Password = os.Getenv("KIT_JWT_TEST_REDIS_PASSWORD")
	client, err := kitredis.New(config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	prefix := "jwt-test:" + time.Now().Format("150405.000000") + ":"
	store, err := jwtredis.New(client, jwtredis.WithKeyPrefix(prefix))
	require.NoError(t, err)
	return store
}

func TestRotateIsAtomicAndReuseRevokes(t *testing.T) {
	store := redisStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	session := kitjwt.Session{ID: "s1", Subject: "u1", RefreshJTI: "old", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, store.Create(ctx, session, 3))
	var success, rejected atomic.Int32
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.Rotate(ctx, kitjwt.Rotation{Subject: "u1", SessionID: "s1", OldJTI: "old", NewJTI: "new-" + string(rune(i)), Now: now.Add(time.Second), ExpiresAt: now.Add(time.Hour)})
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, kitjwt.ErrRefreshReused) || errors.Is(err, kitjwt.ErrSessionRevoked) {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	require.EqualValues(t, 31, rejected.Load())
	stored, err := store.Get(ctx, "u1", "s1")
	require.NoError(t, err)
	require.NotNil(t, stored.RevokedAt)
	require.Equal(t, kitjwt.SessionRevoked, stored.Status)
}

func TestSessionLimitAndRevokeAll(t *testing.T) {
	store := redisStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := range 3 {
		s := kitjwt.Session{ID: string(rune('a' + i)), Subject: "u", RefreshJTI: string(rune('x' + i)), CreatedAt: now.Add(time.Duration(i) * time.Second), LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
		require.NoError(t, store.Create(ctx, s, 2))
	}
	page, err := store.List(ctx, "u", kitjwt.SessionQuery{})
	require.NoError(t, err)
	require.Len(t, page.Sessions, 2)
	require.NoError(t, store.RevokeAll(ctx, "u", now))
	page, err = store.List(ctx, "u", kitjwt.SessionQuery{})
	require.NoError(t, err)
	for _, s := range page.Sessions {
		require.NotNil(t, s.RevokedAt)
	}
	newSession := kitjwt.Session{ID: "new", Subject: "u", RefreshJTI: "new-jti", CreatedAt: now.Add(10 * time.Second), LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, store.Create(ctx, newSession, 2))
	page, err = store.List(ctx, "u", kitjwt.SessionQuery{Status: kitjwt.SessionActive})
	require.NoError(t, err)
	require.Len(t, page.Sessions, 1)
}

func TestRevokeDevice(t *testing.T) {
	store := redisStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, device := range []string{"phone", "tablet"} {
		s := kitjwt.Session{ID: string(rune('a' + i)), Subject: "u-device", DeviceID: device, RefreshJTI: string(rune('x' + i)), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
		require.NoError(t, store.Create(ctx, s, 3))
	}
	require.NoError(t, store.RevokeDevice(ctx, "u-device", "phone", now))
	phone, err := store.Get(ctx, "u-device", "a")
	require.NoError(t, err)
	require.NotNil(t, phone.RevokedAt)
	tablet, err := store.Get(ctx, "u-device", "b")
	require.NoError(t, err)
	require.Nil(t, tablet.RevokedAt)
	require.ErrorIs(t, store.RevokeDevice(ctx, "u-device", "watch", now), kitjwt.ErrSessionNotFound)
}

func TestDeviceReplacementAndPagination(t *testing.T) {
	store := redisStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	first := kitjwt.Session{ID: "first", Subject: "paged", DeviceID: "phone", RefreshJTI: "j1", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
	second := first
	second.ID = "second"
	second.RefreshJTI = "j2"
	second.CreatedAt = now.Add(time.Second)
	require.NoError(t, store.Create(ctx, first, 10))
	require.NoError(t, store.Create(ctx, second, 10))
	_, err := store.Get(ctx, "paged", "first")
	require.ErrorIs(t, err, kitjwt.ErrSessionNotFound)
	for i := range 2 {
		s := kitjwt.Session{ID: fmt.Sprintf("extra-%d", i), Subject: "paged", RefreshJTI: fmt.Sprintf("e-%d", i), CreatedAt: now.Add(time.Duration(i+2) * time.Second), LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
		require.NoError(t, store.Create(ctx, s, 10))
	}
	page, err := store.List(ctx, "paged", kitjwt.SessionQuery{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Sessions, 2)
	require.NotEmpty(t, page.NextCursor)
	next, err := store.List(ctx, "paged", kitjwt.SessionQuery{Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Sessions, 1)
}
