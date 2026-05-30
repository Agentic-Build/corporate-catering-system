package identity

import (
	"errors"
	"net"
	"testing"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

func TestClaimString(t *testing.T) {
	// net.IP implements fmt.Stringer.
	stringer := net.IPv4(10, 0, 0, 1)
	claims := map[string]any{
		"plain":    "  hello  ",
		"stringer": stringer,
		"number":   42,
		"nilval":   nil,
	}
	if got := claimString(claims, "plain"); got != "hello" {
		t.Fatalf("plain: got %q", got)
	}
	if got := claimString(claims, "stringer"); got != stringer.String() {
		t.Fatalf("stringer: got %q want %q", got, stringer.String())
	}
	if got := claimString(claims, "number"); got != "42" {
		t.Fatalf("number: got %q", got)
	}
	if got := claimString(claims, "nilval"); got != "" {
		t.Fatalf("nilval: got %q want empty", got)
	}
	if got := claimString(claims, "absent"); got != "" {
		t.Fatalf("absent: got %q want empty", got)
	}
}

func TestRoleForApp(t *testing.T) {
	cases := map[string]Role{
		"employee": RoleEmployee,
		"merchant": RoleVendorOperator,
		"admin":    RoleWelfareAdmin,
		"unknown":  "",
		"":         "",
	}
	for app, want := range cases {
		if got := roleForApp(app); got != want {
			t.Fatalf("roleForApp(%q) = %q, want %q", app, got, want)
		}
	}
}

func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"/menu":            "/menu",
		"/":                "/",
		"//evil.com":       "/", // protocol-relative is rejected
		"https://evil.com": "/", // absolute is rejected
		"relative":         "/", // missing leading slash
		"":                 "/",
	}
	for in, want := range cases {
		if got := safeReturnTo(in); got != want {
			t.Fatalf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidAppAndRole(t *testing.T) {
	for _, app := range []string{"employee", "merchant", "admin"} {
		if !validApp(app) {
			t.Fatalf("validApp(%q) should be true", app)
		}
	}
	if validApp("nope") {
		t.Fatal("validApp(nope) should be false")
	}
	for _, r := range []Role{RoleEmployee, RoleVendorOperator, RoleWelfareAdmin} {
		if !validRole(r) {
			t.Fatalf("validRole(%q) should be true", r)
		}
	}
	if validRole(Role("ghost")) {
		t.Fatal("validRole(ghost) should be false")
	}
}

// TestUserFromClaims_EmployeeMissingClaims covers the employee branch where the
// required tbite_employee_id / tbite_plant claims are absent. The switch's
// default case is unreachable today because validRole gates role to the three
// handled values before the switch runs.
func TestUserFromClaims_EmployeeMissingClaims(t *testing.T) {
	ui := &oidc.Userinfo{
		Email:           "e@tbite.test",
		ExternalSubject: "sub",
		EmailVerified:   true,
		Raw: map[string]any{
			"tbite_role": string(RoleEmployee),
			// no employee_id / plant
		},
	}
	if _, err := userFromClaims("employee", ui); !errors.Is(err, ErrInvalidClaims) {
		t.Fatalf("expected ErrInvalidClaims, got %v", err)
	}
}

func TestRandState_Unique(t *testing.T) {
	a := randState()
	b := randState()
	if a == "" || b == "" {
		t.Fatal("randState returned empty")
	}
	if a == b {
		t.Fatal("randState should produce distinct values")
	}
}

func TestCallbackError(t *testing.T) {
	base := errors.New("boom")
	ce := &CallbackError{App: "merchant", Err: base}
	if ce.Error() != "boom" {
		t.Fatalf("Error() = %q", ce.Error())
	}
	if !errors.Is(ce, base) {
		t.Fatal("Unwrap should expose base error")
	}
}
