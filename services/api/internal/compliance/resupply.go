package compliance

// validateResupplyTarget reports whether a document may be resupplied (i.e.
// replaced by a freshly uploaded version) on behalf of vendorID. A document
// qualifies only when it belongs to that vendor and has already been
// reviewed — a still-pending document has nothing to resupply yet. Rejected,
// expired, and approved documents are all valid targets (the last covers a
// merchant proactively renewing a document before it expires).
func validateResupplyTarget(target *Document, vendorID string) error {
	if target.VendorID != vendorID {
		return ErrInvalidResupply
	}
	if target.Status == DocStatusPending {
		return ErrInvalidResupply
	}
	return nil
}
