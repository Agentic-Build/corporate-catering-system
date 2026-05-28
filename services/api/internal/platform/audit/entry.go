package audit

// Entry groups the non-structural arguments to AuditRepo.WriteTx / Write.
type Entry struct {
	ActorID    *string
	ActorRole  *string
	Action     string
	TargetKind string
	TargetID   string
	Payload    map[string]any
	RequestID  string
}
