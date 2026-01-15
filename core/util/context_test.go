package util

import (
	"context"
	"testing"
)

type contextKey string

func TestCtxValue(t *testing.T) {
	k := contextKey("key")
	ctx := context.WithValue(context.Background(), k, 2)
	value, err := CtxValue[int](ctx, k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Log(value)
}

func TestCtxValueSlice(t *testing.T) {
	k := contextKey("key")
	ctx := context.WithValue(context.Background(), k, []string{"a", "b"})
	value, err := CtxValue[[]string](ctx, k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Log(value)
}
