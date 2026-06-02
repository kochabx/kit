package ctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctxKey string

func TestValue_OK(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey("user"), "alice")
	v, err := Value[string](ctx, ctxKey("user"))
	require.NoError(t, err)
	assert.Equal(t, "alice", v)
}

func TestValue_NilContext(t *testing.T) {
	//nolint:staticcheck // intentional nil context for test
	v, err := Value[string](context.Background(), ctxKey("user"))
	assert.Error(t, err)
	assert.Equal(t, "", v)
}

func TestValue_NotFound(t *testing.T) {
	v, err := Value[string](context.Background(), ctxKey("missing"))
	assert.Error(t, err)
	assert.Equal(t, "", v)
}

func TestValue_TypeMismatch(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey("age"), 18)
	v, err := Value[string](ctx, ctxKey("age"))
	assert.Error(t, err)
	assert.Equal(t, "", v)
}

func TestValue_Pointer(t *testing.T) {
	type user struct{ Name string }
	u := &user{Name: "bob"}
	ctx := context.WithValue(context.Background(), ctxKey("u"), u)
	got, err := Value[*user](ctx, ctxKey("u"))
	require.NoError(t, err)
	assert.Same(t, u, got)
}
