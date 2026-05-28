package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
)

// TestBuildCoreServices_AllPointerFieldsNonNil exercises buildCoreServices with
// nil dependencies (constructors don't deref, so this is safe) and asserts via
// reflection that every pointer field on coreServices — and on each domain
// service — is set. cmd/ isn't covered by the regular test suite, so a missing
// service field would otherwise only surface as a startup nil-deref panic in
// production; this test catches the wiring slip at build time.
func TestBuildCoreServices_AllPointerFieldsNonNil(t *testing.T) {
	cs, err := buildCoreServices(context.Background(), nil, nil, config.Config{
		AuthentikBaseURL:             "http://stub",
		AuthentikAPIToken:            "stub",
		AuthentikVendorOperatorGroup: "stub",
	}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, cs)

	assertAllPointerFieldsNonNil(t, "coreServices", reflect.ValueOf(*cs))

	// Each domain service must also have every pointer/interface field wired,
	// except for the few interface fields that are intentionally optional.
	assertServiceFieldsNonNil(t, "Order", *cs.Order)
	assertServiceFieldsNonNil(t, "Vendor", *cs.Vendor)
	assertServiceFieldsNonNil(t, "Menu", *cs.Menu)
	// payroll.Service has Exceptions which the previous mcp-stdio inline copy
	// omitted; assert it's set so the drift can't return. CurrentLines is the
	// deliberate test seam — production leaves it nil and falls through to
	// QueryCurrentLines (see Service.ListCurrentLines).
	assertServiceFieldsNonNilExcept(t, "Payroll", *cs.Payroll, "CurrentLines")
	// compliance.Service.Storage is the one field allowed to be nil when the
	// caller passes nil storage (mcp-stdio path), so skip it here.
	assertServiceFieldsNonNilExcept(t, "Compliance", *cs.Compliance, "Storage")
	// feedback.Service.Reverser was also a previous-MCP omission; assert it.
	assertServiceFieldsNonNil(t, "Feedback", *cs.Feedback)
	assertServiceFieldsNonNil(t, "Settlement", *cs.Settlement)

	// Spot-checks on the specific drift the agent flagged. Plain assert.NotNil
	// uses Go interface-not-nil semantics, but typed-nil pointers wrapped in an
	// interface would slip past it; use a typed pointer comparison for the
	// service-graph spot checks instead.
	require.NotNil(t, cs.Payroll.Exceptions, "payroll.Exceptions must be set (was mcp-stdio drift)")
	require.NotNil(t, cs.Compliance.Vendors, "compliance.Vendors must be set (was mcp-stdio drift)")
	require.NotNil(t, cs.Feedback.Reverser, "feedback.Reverser must be set (was mcp-stdio drift)")
}

// TestBuildCoreServices_StorageWiringRespectsNil confirms the conditional
// Storage assignment: passing nil storage leaves compliance.Service.Storage at
// the interface zero value (so a `if s.Storage != nil` guard works), instead
// of wrapping a typed-nil pointer.
func TestBuildCoreServices_StorageWiringRespectsNil(t *testing.T) {
	cs, err := buildCoreServices(context.Background(), nil, nil, config.Config{
		AuthentikBaseURL: "http://stub", AuthentikAPIToken: "stub", AuthentikVendorOperatorGroup: "stub",
	}, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, cs.Compliance.Storage, "compliance.Storage must be a true interface nil when no S3 client is provided")
}

// assertAllPointerFieldsNonNil walks the exported fields of v and fails the
// test for any pointer/interface field that is nil.
func assertAllPointerFieldsNonNil(t *testing.T, label string, v reflect.Value) {
	t.Helper()
	ty := v.Type()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := ty.Field(i).Name
		if !ty.Field(i).IsExported() {
			continue
		}
		switch f.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
			if f.IsNil() {
				t.Errorf("%s.%s is nil — wiring slip", label, name)
			}
		}
	}
}

// assertServiceFieldsNonNil checks every pointer/interface field on a service
// struct value. v is the dereferenced service struct (e.g. *cs.Order).
func assertServiceFieldsNonNil(t *testing.T, label string, v any) {
	t.Helper()
	assertAllPointerFieldsNonNil(t, label, reflect.ValueOf(v))
}

// assertServiceFieldsNonNilExcept is the same but skips the named field(s),
// for fields that are legitimately optional at construction time.
func assertServiceFieldsNonNilExcept(t *testing.T, label string, v any, except ...string) {
	t.Helper()
	skip := map[string]bool{}
	for _, n := range except {
		skip[n] = true
	}
	val := reflect.ValueOf(v)
	ty := val.Type()
	for i := 0; i < val.NumField(); i++ {
		f := val.Field(i)
		name := ty.Field(i).Name
		if !ty.Field(i).IsExported() || skip[name] {
			continue
		}
		switch f.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
			if f.IsNil() {
				t.Errorf("%s.%s is nil — wiring slip", label, name)
			}
		}
	}
}
