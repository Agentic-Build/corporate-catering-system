package compliance

import "testing"

func TestValidateResupplyTarget(t *testing.T) {
	cases := []struct {
		name     string
		target   *Document
		vendorID string
		wantErr  bool
	}{
		{"rejected, same vendor", &Document{VendorID: "v1", Status: DocStatusRejected}, "v1", false},
		{"expired, same vendor", &Document{VendorID: "v1", Status: DocStatusExpired}, "v1", false},
		{"approved, same vendor (proactive renewal)", &Document{VendorID: "v1", Status: DocStatusApproved}, "v1", false},
		{"pending, same vendor — nothing to resupply yet", &Document{VendorID: "v1", Status: DocStatusPending}, "v1", true},
		{"rejected, different vendor", &Document{VendorID: "v2", Status: DocStatusRejected}, "v1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResupplyTarget(tc.target, tc.vendorID)
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}
