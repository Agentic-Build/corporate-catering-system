package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

// SecretBytes is the length of a TOTP secret in bytes (256-bit).
const SecretBytes = 32

// StepSeconds is the time-step duration. Tokens rotate every 30s.
const StepSeconds = 30

// Digits is the displayed TOTP length.
const Digits = 6

// NewSecret returns a fresh 32-byte random secret.
func NewSecret() ([]byte, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("totp secret: %w", err)
	}
	return b, nil
}

// Generate returns a TOTP code for the given secret at the given time.
func Generate(secret []byte, t time.Time) string {
	counter := uint64(t.Unix() / StepSeconds)
	return hotp(secret, counter)
}

// Verify checks if `code` matches the secret within ±1 time-step window.
// Constant-time compare prevents timing side channels.
func Verify(secret []byte, code string, now time.Time) bool {
	counter := uint64(now.Unix() / StepSeconds)
	for _, delta := range []int64{0, -1, 1} {
		expected := hotp(secret, uint64(int64(counter)+delta))
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
}

func hotp(secret []byte, counter uint64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha256.New, secret)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, code%mod)
}
