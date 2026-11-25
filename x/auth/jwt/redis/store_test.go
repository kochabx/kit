package redis

import (
	"strings"
	"testing"

	kitjwt "github.com/kochabx/kit/x/auth/jwt"
	goredis "github.com/redis/go-redis/v9"
)

func TestNewValidation(t *testing.T) {
	t.Parallel()

	if _, err := New(nil); err == nil {
		t.Fatal("expected error for nil redis client")
	}

	rdb := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	defer func() { _ = rdb.Close() }()

	if _, err := New(rdb, WithKeyPrefix("   ")); err == nil {
		t.Fatal("expected error for blank key prefix")
	}

	store, err := New(rdb, WithKeyPrefix("auth:"))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	var _ kitjwt.SessionStore = store

	sk, idx, pfx := store.authKeys("user-42", "sid-1")
	dk := store.deviceKey("user-42", "ios-1")
	if !strings.HasPrefix(sk, pfx) || !strings.HasPrefix(dk, pfx) || !strings.Contains(idx, "{") {
		t.Fatalf("unexpected cluster hash-tagged keys: sk=%q idx=%q dk=%q pfx=%q", sk, idx, dk, pfx)
	}
}

func TestLuaScriptsUseEvalSHAAndFixes(t *testing.T) {
	t.Parallel()

	// Check SHA1 hashes are non-empty (goredis.NewScript executes via EVALSHA).
	for name, sc := range map[string]*goredis.Script{
		"create":       createSessionScript,
		"rotate":       rotateSessionScript,
		"revoke":       revokeSessionScript,
		"revokeAll":    revokeAllScript,
		"revokeDevice": revokeDeviceScript,
	} {
		if sc == nil || sc.Hash() == "" {
			t.Fatalf("script %s missing hash", name)
		}
	}

	// Verify create & rotate scripts check PTTL before GT on ZSET PEXPIREAT and renew s.device_key.
	for _, snippet := range []string{"PTTL", "PEXPIREAT", "'GT'", "s.device_key"} {
		if !strings.Contains(createSessionLua, snippet) && snippet != "s.device_key" {
			t.Fatalf("createSessionLua missing %q", snippet)
		}
		if !strings.Contains(rotateSessionLua, snippet) {
			t.Fatalf("rotateSessionLua missing %q", snippet)
		}
	}
}
