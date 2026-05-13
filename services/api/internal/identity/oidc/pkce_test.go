package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestS256_KnownVector(t *testing.T) {
	// RFC 7636 Appendix B
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expectedHash := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(expectedHash[:])
	assert.Equal(t, expected, s256(verifier))
}

func TestRandURLSafe_Length(t *testing.T) {
	a := randURLSafe(32)
	b := randURLSafe(32)
	assert.NotEqual(t, a, b)
	// base64 of 32 bytes is ~43 chars unpadded
	assert.GreaterOrEqual(t, len(a), 40)
}
