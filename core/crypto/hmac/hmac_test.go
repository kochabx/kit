package hmac

import (
	"testing"
	"time"
)

func TestHmac(t *testing.T) {
	secret := "123456"
	message := "hello"
	result, err := Sign(secret, WithPayload(message))
	if err != nil {
		t.Fatal(err)
	}

	if err := Verify(secret, result.Signature, result.Timestamp, WithPayload(message)); err != nil {
		t.Fatal(err)
	}
}

func TestSignWithoutPayload(t *testing.T) {
	secret := "test-secret"
	result, err := Sign(secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(secret, result.Signature, result.Timestamp); err != nil {
		t.Fatal(err)
	}
}

func TestSignEmptySecret(t *testing.T) {
	_, err := Sign("")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestVerifyExpiredSignature(t *testing.T) {
	secret := "test-secret"
	now := time.Now()

	result, err := Sign(secret, withTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	// 验证时间设置为 6 分钟后
	futureTime := now.Add(6 * time.Minute)
	err = Verify(secret, result.Signature, result.Timestamp,
		withTimeFunc(func() time.Time { return futureTime }))
	if err == nil {
		t.Fatal("expected error for expired signature")
	}
}

func TestVerifyCustomExpiration(t *testing.T) {
	secret := "test-secret"
	now := time.Now()

	result, err := Sign(secret,
		WithExpiration(10*time.Minute),
		withTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	// 验证时间设置为 8 分钟后（在 10 分钟有效期内）
	futureTime := now.Add(8 * time.Minute)
	err = Verify(secret, result.Signature, result.Timestamp,
		WithExpiration(10*time.Minute),
		withTimeFunc(func() time.Time { return futureTime }))
	if err != nil {
		t.Fatal("signature should still be valid")
	}
}

func TestVerifyInvalidSignature(t *testing.T) {
	secret := "test-secret"
	result, err := Sign(secret, WithPayload("data1"))
	if err != nil {
		t.Fatal(err)
	}

	// 使用不同的 payload 验证
	err = Verify(secret, result.Signature, result.Timestamp, WithPayload("data2"))
	if err == nil {
		t.Fatal("expected error for mismatched payload")
	}
}

func TestVerifyInvalidSignatureFormat(t *testing.T) {
	secret := "test-secret"
	err := Verify(secret, "invalid-hex", time.Now().Unix())
	if err == nil {
		t.Fatal("expected error for invalid signature format")
	}
}

func TestVerifyFutureTimestamp(t *testing.T) {
	secret := "test-secret"
	futureTimestamp := time.Now().Add(1 * time.Hour).Unix()
	err := Verify(secret, "somesignature", futureTimestamp)
	if err == nil {
		t.Fatal("expected error for future timestamp")
	}
}
