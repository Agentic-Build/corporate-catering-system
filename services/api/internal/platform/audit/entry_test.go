package audit

import "testing"

// Entry is a pure data-carrier struct with no methods or executable
// statements. These tests assert its field layout and pointer/optional
// semantics so accidental shape changes are caught at compile/test time.

func strptr(s string) *string { return &s }

func TestEntry_FullPopulation(t *testing.T) {
	actorID := strptr("actor-1")
	actorRole := strptr("merchant")
	e := Entry{
		ActorID:    actorID,
		ActorRole:  actorRole,
		Action:     "order.create",
		TargetKind: "order",
		TargetID:   "ord-123",
		Payload:    map[string]any{"amount_minor": 250, "note": "lunch"},
		RequestID:  "req-abc",
	}

	if e.ActorID == nil || *e.ActorID != "actor-1" {
		t.Fatalf("ActorID = %v, want actor-1", e.ActorID)
	}
	if e.ActorRole == nil || *e.ActorRole != "merchant" {
		t.Fatalf("ActorRole = %v, want merchant", e.ActorRole)
	}
	if e.Action != "order.create" {
		t.Fatalf("Action = %q, want order.create", e.Action)
	}
	if e.TargetKind != "order" {
		t.Fatalf("TargetKind = %q, want order", e.TargetKind)
	}
	if e.TargetID != "ord-123" {
		t.Fatalf("TargetID = %q, want ord-123", e.TargetID)
	}
	if got := e.Payload["amount_minor"]; got != 250 {
		t.Fatalf("Payload[amount_minor] = %v, want 250 (whole NTD)", got)
	}
	if got := e.Payload["note"]; got != "lunch" {
		t.Fatalf("Payload[note] = %v, want lunch", got)
	}
	if e.RequestID != "req-abc" {
		t.Fatalf("RequestID = %q, want req-abc", e.RequestID)
	}
}

func TestEntry_ZeroValue(t *testing.T) {
	var e Entry

	if e.ActorID != nil {
		t.Fatalf("zero ActorID = %v, want nil", e.ActorID)
	}
	if e.ActorRole != nil {
		t.Fatalf("zero ActorRole = %v, want nil", e.ActorRole)
	}
	if e.Action != "" || e.TargetKind != "" || e.TargetID != "" || e.RequestID != "" {
		t.Fatalf("zero string fields not empty: %+v", e)
	}
	if e.Payload != nil {
		t.Fatalf("zero Payload = %v, want nil", e.Payload)
	}
}

func TestEntry_OptionalActorNil(t *testing.T) {
	// System/unauthenticated actions carry nil actor pointers but still
	// populate action/target — assert this is representable.
	e := Entry{
		ActorID:    nil,
		ActorRole:  nil,
		Action:     "system.cleanup",
		TargetKind: "job",
		TargetID:   "job-9",
		RequestID:  "req-sys",
	}

	if e.ActorID != nil || e.ActorRole != nil {
		t.Fatalf("expected nil actor pointers, got %v / %v", e.ActorID, e.ActorRole)
	}
	if e.Action != "system.cleanup" {
		t.Fatalf("Action = %q, want system.cleanup", e.Action)
	}
}
