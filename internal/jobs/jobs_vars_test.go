package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SanitizeVarValue
// ---------------------------------------------------------------------------

func TestSanitizeVarValue(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		v, err := SanitizeVarValue("key", "hello world", 100)
		require.NoError(t, err)
		assert.Equal(t, "hello world", v)
	})

	t.Run("null byte", func(t *testing.T) {
		_, err := SanitizeVarValue("key", "he\x00llo", 100)
		assert.ErrorContains(t, err, "contains null byte")
	})

	t.Run("over length", func(t *testing.T) {
		_, err := SanitizeVarValue("key", "hello", 3)
		assert.ErrorContains(t, err, "exceeds max length")
	})

	t.Run("exact boundary", func(t *testing.T) {
		v, err := SanitizeVarValue("key", "12345", 5)
		require.NoError(t, err)
		assert.Equal(t, "12345", v)
	})

	t.Run("empty string is valid", func(t *testing.T) {
		v, err := SanitizeVarValue("key", "", 100)
		require.NoError(t, err)
		assert.Equal(t, "", v)
	})
}

// ---------------------------------------------------------------------------
// ResolveVariables
// ---------------------------------------------------------------------------

func TestResolveVariables(t *testing.T) {
	t.Run("empty config", func(t *testing.T) {
		vars, err := ResolveVariables(nil, 100)
		require.NoError(t, err)
		assert.Empty(t, vars)

		vars, err = ResolveVariables(VariablesConfig{}, 100)
		require.NoError(t, err)
		assert.Empty(t, vars)
	})

	t.Run("static source", func(t *testing.T) {
		vars, err := ResolveVariables(VariablesConfig{
			"url": map[string]any{"type": "static", "value": "https://example.com"},
		}, 100)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com", vars["url"])
	})

	t.Run("direct string value", func(t *testing.T) {
		vars, err := ResolveVariables(VariablesConfig{
			"name":     "Alice",
			"greeting": "Hello",
		}, 100)
		require.NoError(t, err)
		assert.Equal(t, "Alice", vars["name"])
		assert.Equal(t, "Hello", vars["greeting"])
	})

	t.Run("mixed static and direct", func(t *testing.T) {
		vars, err := ResolveVariables(VariablesConfig{
			"direct": "val",
			"src":    map[string]any{"type": "static", "value": "static-val"},
		}, 100)
		require.NoError(t, err)
		assert.Equal(t, "val", vars["direct"])
		assert.Equal(t, "static-val", vars["src"])
	})

	t.Run("non-string value", func(t *testing.T) {
		_, err := ResolveVariables(VariablesConfig{"x": 42}, 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected string")
	})

	t.Run("unknown source type", func(t *testing.T) {
		_, err := ResolveVariables(VariablesConfig{
			"x": map[string]any{"type": "unsupported", "value": "test"},
		}, 100)
		assert.ErrorContains(t, err, "unknown variable source type")
	})

	t.Run("default max length when zero", func(t *testing.T) {
		long := make([]byte, DefaultMaxVarLength+1)
		for i := range long {
			long[i] = 'a'
		}
		_, err := ResolveVariables(VariablesConfig{"x": string(long)}, 0)
		assert.Error(t, err, "should use DefaultMaxVarLength and reject overlong value")
	})
}

// ---------------------------------------------------------------------------
// LocalLock
// ---------------------------------------------------------------------------

func TestLocalLock(t *testing.T) {
	lock := NewLocalLock()
	require.NotNil(t, lock)

	ctx := t.Context()

	t.Run("first lock succeeds", func(t *testing.T) {
		ok, err := lock.TryLock(ctx, "key-a", 0)
		assert.True(t, ok)
		assert.NoError(t, err)
	})

	t.Run("second lock on same key fails", func(t *testing.T) {
		ok, err := lock.TryLock(ctx, "key-a", 0)
		assert.False(t, ok)
		assert.NoError(t, err)
	})

	t.Run("different key succeeds", func(t *testing.T) {
		ok, err := lock.TryLock(ctx, "key-b", 0)
		assert.True(t, ok)
		assert.NoError(t, err)
	})

	t.Run("unlock then re-lock", func(t *testing.T) {
		err := lock.Unlock(ctx, "key-a")
		assert.NoError(t, err)

		ok, err := lock.TryLock(ctx, "key-a", 0)
		assert.True(t, ok)
		assert.NoError(t, err)
	})

	t.Run("unlock non-existent key", func(t *testing.T) {
		err := lock.Unlock(ctx, "nonexistent")
		assert.NoError(t, err)
	})
}

func TestLocalLockConcurrent(t *testing.T) {
	lock := NewLocalLock()
	ctx := t.Context()

	ok, err := lock.TryLock(ctx, "concurrent-key", 0)
	assert.True(t, ok)
	assert.NoError(t, err)

	ok, err = lock.TryLock(ctx, "concurrent-key", 0)
	assert.False(t, ok)
	assert.NoError(t, err)

	err = lock.Unlock(ctx, "concurrent-key")
	assert.NoError(t, err)

	ok, err = lock.TryLock(ctx, "concurrent-key", 0)
	assert.True(t, ok)
	assert.NoError(t, err)
}

func TestLocalLockDifferentKeysConcurrent(t *testing.T) {
	lock := NewLocalLock()
	ctx := t.Context()

	done := make(chan bool)
	go func() {
		ok, err := lock.TryLock(ctx, "key-goroutine", 0)
		done <- ok && err == nil
	}()

	ok, err := lock.TryLock(ctx, "key-main", 0)
	assert.True(t, ok)
	assert.NoError(t, err)

	assert.True(t, <-done, "goroutine should lock its own key independently")

	_ = lock.Unlock(ctx, "key-main")
	_ = lock.Unlock(ctx, "key-goroutine")
}
