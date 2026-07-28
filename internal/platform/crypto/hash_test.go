package crypto

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateRandomKey tests the GenerateRandomKey function.
func TestGenerateRandomKey(t *testing.T) {
	t.Run("returns different values on successive calls", func(t *testing.T) {
		key1, err := GenerateRandomKey()
		require.NoError(t, err)
		key2, err := GenerateRandomKey()
		require.NoError(t, err)
		require.NotEqual(t, key1, key2, "GenerateRandomKey should return different values")
	})

	t.Run("output is valid hex and 64 characters", func(t *testing.T) {
		key, err := GenerateRandomKey()
		require.NoError(t, err)
		require.Equal(t, 64, len(key), "GenerateRandomKey output should be 64 hex characters")
		_, err = hex.DecodeString(key)
		require.NoError(t, err, "GenerateRandomKey output should be valid hex")
	})
}

// TestGenerateSalt tests the GenerateSalt function.
func TestGenerateSalt(t *testing.T) {
	t.Run("returns different values on successive calls", func(t *testing.T) {
		salt1, err := GenerateSalt()
		require.NoError(t, err)
		salt2, err := GenerateSalt()
		require.NoError(t, err)
		require.NotEqual(t, salt1, salt2, "GenerateSalt should return different values")
	})

	t.Run("output is valid base64 URL-safe encoding without padding", func(t *testing.T) {
		salt, err := GenerateSalt()
		require.NoError(t, err)
		// Decode using RawURLEncoding (no padding)
		decoded, err := base64.RawURLEncoding.DecodeString(salt)
		require.NoError(t, err, "GenerateSalt output should be valid base64 URL-safe encoding")
		// Should be 16 bytes
		require.Equal(t, 16, len(decoded), "Decoded salt should be 16 bytes")
	})
}

// TestHashToken tests the HashToken function.
func TestHashToken(t *testing.T) {
	token := "test-token"

	t.Run("with argon2 returns hash, salt, and no error", func(t *testing.T) {
		hashHex, saltB64, err := HashToken(token, "argon2")
		require.NoError(t, err)
		require.NotEmpty(t, hashHex, "hashHex should not be empty")
		require.NotEmpty(t, saltB64, "saltB64 should not be empty")
		// Verify salt is valid base64
		_, err = base64.RawURLEncoding.DecodeString(saltB64)
		require.NoError(t, err, "saltB64 should be valid base64 URL-safe encoding")
		// Verify hash is valid hex
		_, err = hex.DecodeString(hashHex)
		require.NoError(t, err, "hashHex should be valid hex encoding")
	})

	t.Run("with unsupported algorithm returns error", func(t *testing.T) {
		_, _, err := HashToken(token, "unsupported")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported hash algorithm")
	})

	t.Run("produces different hashes for the same token (due to different salts)", func(t *testing.T) {
		hash1, _, err := HashToken(token, "argon2")
		require.NoError(t, err)
		hash2, _, err := HashToken(token, "argon2")
		require.NoError(t, err)
		require.NotEqual(t, hash1, hash2, "HashToken should produce different hashes for the same token due to random salt")
	})
}

// TestHashTokenWithSalt tests the HashTokenWithSalt function.
func TestHashTokenWithSalt(t *testing.T) {
	token := "test-token"

	t.Run("with argon2 returns hash and no error", func(t *testing.T) {
		// First, generate a salt using GenerateSalt
		salt, err := GenerateSalt()
		require.NoError(t, err)
		// Then hash the token with that salt
		hash, err := HashTokenWithSalt(token, salt, "argon2")
		require.NoError(t, err)
		require.NotEmpty(t, hash, "hash should not be empty")
		// Verify hash is valid hex
		_, err = hex.DecodeString(hash)
		require.NoError(t, err, "hash should be valid hex encoding")
	})

	t.Run("with unsupported algorithm returns error", func(t *testing.T) {
		_, err := HashTokenWithSalt(token, "somesalt", "unsupported")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported hash algorithm")
	})

	t.Run("with wrong token returns different hash", func(t *testing.T) {
		salt, err := GenerateSalt()
		require.NoError(t, err)
		hash1, err := HashTokenWithSalt(token, salt, "argon2")
		require.NoError(t, err)
		wrongToken := "wrong-token"
		hash2, err := HashTokenWithSalt(wrongToken, salt, "argon2")
		require.NoError(t, err)
		require.NotEqual(t, hash1, hash2, "HashTokenWithSalt should produce different hashes for different tokens with the same salt")
	})

	t.Run("is deterministic: same token and salt produce same hash", func(t *testing.T) {
		salt, err := GenerateSalt()
		require.NoError(t, err)
		hash1, err := HashTokenWithSalt(token, salt, "argon2")
		require.NoError(t, err)
		hash2, err := HashTokenWithSalt(token, salt, "argon2")
		require.NoError(t, err)
		require.Equal(t, hash1, hash2, "HashTokenWithSalt should be deterministic for the same token and salt")
	})
}

// TestConstantTimeCompareFlow tests the end-to-return flow of hashing a token, storing salt:hash, and verifying with constant time comparison.
func TestConstantTimeCompareFlow(t *testing.T) {
	token := "my-token"
	// Step 1: Hash the token to get hash and salt
	hashHex, saltB64, err := HashToken(token, "argon2")
	require.NoError(t, err)
	// Step 2: Store the salt and hash (in practice, you'd store them together, e.g., saltB64 + ":" + hashHex)
	// Step 3: To verify, recompute the hash using the stored salt
	recomputedHash, err := HashTokenWithSalt(token, saltB64, "argon2")
	require.NoError(t, err)
	// Step 4: Compare the two hashes using constant time comparison
	// We'll use the subtle.ConstantTimeCompare from the crypto/subtle package
	hashBytes1, _ := hex.DecodeString(hashHex)
	hashBytes2, _ := hex.DecodeString(recomputedHash)
	result := subtle.ConstantTimeCompare(hashBytes1, hashBytes2)
	require.Equal(t, 1, result, "Hashes should be equal (constant time comparison returns 1 for match)")
}

func TestExtractKeyPrefix(t *testing.T) {
	require.Equal(t, "abc123def456", ExtractKeyPrefix("abc123def4567890"))
	require.Equal(t, "aabbccddeeff", ExtractKeyPrefix("aabbccddeeff112233"))
	require.Equal(t, "", ExtractKeyPrefix("short"))
	require.Equal(t, "", ExtractKeyPrefix(""))
}
