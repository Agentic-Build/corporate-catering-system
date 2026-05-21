package mhttp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateImageUpload_AcceptedTypes(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": "jpg",
		"image/png":  "png",
		"image/webp": "webp",
	}
	for ct, wantExt := range cases {
		ext, err := validateImageUpload(ct, 1024)
		require.NoError(t, err, ct)
		assert.Equal(t, wantExt, ext, ct)
	}
}

func TestValidateImageUpload_RejectsUnsupportedType(t *testing.T) {
	_, err := validateImageUpload("application/pdf", 1024)
	require.Error(t, err)
}

func TestValidateImageUpload_RejectsEmpty(t *testing.T) {
	_, err := validateImageUpload("image/png", 0)
	require.Error(t, err)
}

func TestValidateImageUpload_RejectsTooLarge(t *testing.T) {
	_, err := validateImageUpload("image/png", maxImageBytes+1)
	require.Error(t, err)
}

func TestValidateImageUpload_AcceptsAtSizeLimit(t *testing.T) {
	ext, err := validateImageUpload("image/jpeg", maxImageBytes)
	require.NoError(t, err)
	assert.Equal(t, "jpg", ext)
}

func TestImageObjectKey_VendorScopedWithExt(t *testing.T) {
	key := imageObjectKey("vendor-123", "png")
	assert.True(t, strings.HasPrefix(key, "menu-images/vendor-123/"), key)
	assert.True(t, strings.HasSuffix(key, ".png"), key)
	// Distinct calls produce distinct keys.
	assert.NotEqual(t, key, imageObjectKey("vendor-123", "png"))
}
