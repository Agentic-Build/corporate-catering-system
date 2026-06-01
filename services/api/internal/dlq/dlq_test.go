package dlq

import (
	"errors"
	"testing"
	"time"
)

// The dlq package root holds only declarations: the Message struct, the
// Repository interface, and two sentinel errors. There are no executable
// statements (the cover tool reports 0 of 0). These tests document the
// behavior callers rely on.

func TestSentinelErrorsAreDistinct(t *testing.T) {
	if ErrMessageNotFound == nil || ErrAlreadyResolved == nil {
		t.Fatal("sentinel errors must be non-nil")
	}
	if errors.Is(ErrMessageNotFound, ErrAlreadyResolved) {
		t.Fatal("ErrMessageNotFound and ErrAlreadyResolved must be distinct")
	}
	if ErrMessageNotFound.Error() != "dlq: message not found" {
		t.Fatalf("unexpected message: %q", ErrMessageNotFound.Error())
	}
	if ErrAlreadyResolved.Error() != "dlq: already replayed or resolved" {
		t.Fatalf("unexpected message: %q", ErrAlreadyResolved.Error())
	}
}

func TestSentinelErrorsWrap(t *testing.T) {
	wrapped := errors.Join(errors.New("ctx"), ErrMessageNotFound)
	if !errors.Is(wrapped, ErrMessageNotFound) {
		t.Fatal("wrapped error should match ErrMessageNotFound via errors.Is")
	}
	if errors.Is(wrapped, ErrAlreadyResolved) {
		t.Fatal("wrapped error should not match the unrelated sentinel")
	}
}

func TestMessageZeroValue(t *testing.T) {
	var m Message
	if m.ID != "" || m.SourceStream != "" || m.ResolvedNotes != "" {
		t.Fatal("string fields should default to empty")
	}
	if m.Payload != nil || m.Headers != nil {
		t.Fatal("map fields should default to nil")
	}
	if m.ReplayedAt != nil || m.ReplayedBy != nil || m.ResolvedAt != nil || m.ResolvedBy != nil {
		t.Fatal("pointer fields should default to nil")
	}
	if !m.FirstSeenAt.IsZero() {
		t.Fatal("FirstSeenAt should default to the zero time")
	}
}

func TestMessageRoundTripFields(t *testing.T) {
	now := time.Now().UTC()
	by := "admin"
	m := Message{
		ID:            "id-1",
		SourceStream:  "ORDERS",
		Payload:       map[string]any{"k": "v"},
		ReplayedAt:    &now,
		ReplayedBy:    &by,
		ResolvedNotes: "garbage",
	}
	if m.Payload["k"] != "v" {
		t.Fatal("payload value lost")
	}
	if m.ReplayedAt == nil || !m.ReplayedAt.Equal(now) {
		t.Fatal("ReplayedAt not preserved")
	}
	if m.ReplayedBy == nil || *m.ReplayedBy != "admin" {
		t.Fatal("ReplayedBy not preserved")
	}
}
