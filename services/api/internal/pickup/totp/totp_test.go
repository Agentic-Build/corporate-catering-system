package totp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/pickup/totp"
)

func TestNewSecret_Length(t *testing.T) {
	s, err := totp.NewSecret()
	require.NoError(t, err)
	assert.Len(t, s, totp.SecretBytes)
}

func TestGenerate_Deterministic(t *testing.T) {
	sec := make([]byte, 32)
	now := time.Unix(1700000000, 0)
	assert.Equal(t, totp.Generate(sec, now), totp.Generate(sec, now))
	assert.Equal(t, totp.Digits, len(totp.Generate(sec, now)))
}

func TestVerify_AcceptsCurrentAndPrevWindow(t *testing.T) {
	sec, _ := totp.NewSecret()
	now := time.Now()
	prev := now.Add(-30 * time.Second)
	next := now.Add(30 * time.Second)
	codeNow := totp.Generate(sec, now)
	codePrev := totp.Generate(sec, prev)
	codeNext := totp.Generate(sec, next)

	assert.True(t, totp.Verify(sec, codeNow, now))
	assert.True(t, totp.Verify(sec, codePrev, now))
	assert.True(t, totp.Verify(sec, codeNext, now))

	// 2 windows back must fail
	twoAgo := now.Add(-60 * time.Second)
	codeTwoAgo := totp.Generate(sec, twoAgo)
	assert.False(t, totp.Verify(sec, codeTwoAgo, now))
}

func TestVerify_WrongSecretFails(t *testing.T) {
	sec1, _ := totp.NewSecret()
	sec2, _ := totp.NewSecret()
	now := time.Now()
	assert.False(t, totp.Verify(sec1, totp.Generate(sec2, now), now))
}
