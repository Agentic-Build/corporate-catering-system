package identity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

func TestRoleConstants(t *testing.T) {
	assert.Equal(t, identity.Role("employee"), identity.RoleEmployee)
	assert.Equal(t, identity.Role("vendor_operator"), identity.RoleVendorOperator)
	assert.Equal(t, identity.Role("welfare_admin"), identity.RoleWelfareAdmin)
}

func TestStatusConstants(t *testing.T) {
	assert.Equal(t, identity.Status("active"), identity.StatusActive)
	assert.Equal(t, identity.Status("suspended"), identity.StatusSuspended)
	assert.Equal(t, identity.Status("terminated"), identity.StatusTerminated)
}

func TestProviderConstants(t *testing.T) {
	assert.Equal(t, identity.Provider("google"), identity.ProviderGoogle)
	assert.Equal(t, identity.Provider("github"), identity.ProviderGitHub)
}
