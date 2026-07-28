package reqmeta

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIKeyBudgetGetter(t *testing.T) {
	ctx := context.Background()

	// Not set
	b, ok := GetAPIKeyBudget(ctx)
	assert.False(t, ok)
	assert.Equal(t, 0.0, b)

	// Set
	ctx = context.WithValue(ctx, APIKeyBudgetContextKey, 100.5)

	b, ok = GetAPIKeyBudget(ctx)
	assert.True(t, ok)
	assert.Equal(t, 100.5, b)
}

func TestAPIKeyAuthDoneGetter(t *testing.T) {
	ctx := context.Background()

	// Not set
	assert.False(t, IsAPIKeyAuthDone(ctx))

	// Set
	ctx = context.WithValue(ctx, APIKeyAuthDoneContextKey, true)

	assert.True(t, IsAPIKeyAuthDone(ctx))
}
