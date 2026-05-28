package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
)

// Asserts every wired field is non-nil — cmd/ has no other test coverage, so
// this is what catches a wiring slip before it deploys as a startup panic.
func TestBuildCoreServices_AllPointerFieldsNonNil(t *testing.T) {
	cs, err := buildCoreServices(context.Background(), nil, nil, config.Config{
		AuthentikBaseURL:             "http://stub",
		AuthentikAPIToken:            "stub",
		AuthentikVendorOperatorGroup: "stub",
	}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, cs)

	assertAllPointerFieldsNonNil(t, "coreServices", reflect.ValueOf(*cs))

	assertServiceFieldsNonNil(t, "Order", *cs.Order)
	assertServiceFieldsNonNil(t, "Vendor", *cs.Vendor)
	assertServiceFieldsNonNil(t, "Menu", *cs.Menu)
	// Payroll.CurrentLines: deliberate test seam, prod fallthrough leaves it nil.
	assertServiceFieldsNonNilExcept(t, "Payroll", *cs.Payroll, "CurrentLines")
	// Compliance.Storage: nil when no S3 client (mcp-stdio path).
	assertServiceFieldsNonNilExcept(t, "Compliance", *cs.Compliance, "Storage")
	assertServiceFieldsNonNil(t, "Feedback", *cs.Feedback)
	assertServiceFieldsNonNil(t, "Settlement", *cs.Settlement)

	// Pin the three fields the previous mcp-stdio inline copy used to omit.
	require.NotNil(t, cs.Payroll.Exceptions)
	require.NotNil(t, cs.Compliance.Vendors)
	require.NotNil(t, cs.Feedback.Reverser)
}

// Passing nil storage must leave compliance.Storage as interface-nil — not a
// typed-nil-wrapped-in-interface, which would slip past `if s.Storage != nil`.
func TestBuildCoreServices_StorageWiringRespectsNil(t *testing.T) {
	cs, err := buildCoreServices(context.Background(), nil, nil, config.Config{
		AuthentikBaseURL: "http://stub", AuthentikAPIToken: "stub", AuthentikVendorOperatorGroup: "stub",
	}, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, cs.Compliance.Storage)
}

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

func assertServiceFieldsNonNil(t *testing.T, label string, v any) {
	t.Helper()
	assertAllPointerFieldsNonNil(t, label, reflect.ValueOf(v))
}

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
