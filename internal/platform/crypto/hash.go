package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/argon2"
)

func GenerateRandomKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken hashes token with SHA-256 or Argon2id. Returns (hashHex, saltB64, error).
func HashToken(token string, algo string) (string, string, error) {
	if algo == "sha256" {
		hash := sha256.Sum256([]byte(token))
		return hex.EncodeToString(hash[:]), "sha256", nil
	}

	if algo == "argon2" {
		salt, err := GenerateSalt()
		if err != nil {
			return "", "", fmt.Errorf("failed to generate salt: %w", err)
		}
		hash := argon2.IDKey([]byte(token), []byte(salt), 1, 64*1024, 4, 32)
		return hex.EncodeToString(hash), salt, nil
	}

	return "", "", fmt.Errorf("unsupported hash algorithm: %s", algo)
}

func ExtractKeyPrefix(rawToken string) string {
	if len(rawToken) >= 12 {
		return rawToken[:12]
	}
	return ""
}

func HashTokenWithSalt(token string, salt string, algo string) (string, error) {
	if algo == "sha256" || salt == "sha256" {
		hash := sha256.Sum256([]byte(token))
		return hex.EncodeToString(hash[:]), nil
	}

	if algo == "argon2" {
		hash := argon2.IDKey([]byte(token), []byte(salt), 1, 64*1024, 4, 32)
		return hex.EncodeToString(hash), nil
	}

	return "", fmt.Errorf("unsupported hash algorithm: %s", algo)
}
