package order

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMaybeConcurrencyErr(t *testing.T) {
	if got := MaybeConcurrencyErr(nil); got != nil {
		t.Fatalf("nil in → %v, want nil", got)
	}

	// A plain (non-pg) error passes through unchanged.
	plain := errors.New("boom")
	if got := MaybeConcurrencyErr(plain); !errors.Is(got, plain) {
		t.Fatalf("plain error not passed through: %v", got)
	}
	if errors.Is(MaybeConcurrencyErr(plain), ErrConcurrentModification) {
		t.Fatal("plain error wrongly classified as concurrent")
	}

	// Retryable conflict codes wrap to ErrConcurrentModification.
	for _, code := range []string{"40P01", "40001"} {
		pgErr := &pgconn.PgError{Code: code, Message: "conflict-" + code}
		// Wrap it so errors.As has to unwrap, mirroring pgx's real layering.
		got := MaybeConcurrencyErr(fmt.Errorf("tx: %w", pgErr))
		if !errors.Is(got, ErrConcurrentModification) {
			t.Errorf("code %s → %v, want ErrConcurrentModification", code, got)
		}
	}

	// A pg error with a non-retryable code is left untouched.
	other := &pgconn.PgError{Code: "23505", Message: "dup key"}
	got := MaybeConcurrencyErr(other)
	if errors.Is(got, ErrConcurrentModification) {
		t.Fatalf("non-retryable pg code wrongly classified: %v", got)
	}
	var pg *pgconn.PgError
	if !errors.As(got, &pg) || pg.Code != "23505" {
		t.Fatalf("non-retryable pg error not passed through: %v", got)
	}
}

func TestAssemblePrepSheet_ShortIDFallback(t *testing.T) {
	// An id of <= 8 chars not present in names is returned verbatim (the
	// `return id` fallback, distinct from the id[:8] truncation path).
	sheet := assemblePrepSheet(time.Now(), "v1", []*Order{
		{ID: "o1", Plant: "P", Items: []Item{{MenuItemID: "short", Qty: 1}}},
	}, nil)
	if got := sheet.Plants[0].Items[0].Name; got != "short" {
		t.Fatalf("short-id fallback name = %q, want %q", got, "short")
	}
}

func TestCanTransition_UnknownFromState(t *testing.T) {
	// A status that is not a key in allowedTransitions returns false (the
	// `!ok` branch) rather than panicking on a nil sub-map lookup.
	if CanTransition(Status("bogus"), StatusPlaced) {
		t.Fatal("unknown from-state must not allow any transition")
	}
	if CanTransition(Status(""), StatusCancelled) {
		t.Fatal("empty from-state must not allow any transition")
	}
}

func TestSanitizeConsumerToken(t *testing.T) {
	cases := map[string]string{
		"host.local":  "host-local",
		"pod_abc-123": "pod_abc-123",
		"AZaz09":      "AZaz09",
		"a b/c.d":     "a-b-c-d",
		"":            "",
		"已知主機":        "----", // each non-ASCII rune maps to '-'
		"ok":          "ok",
	}
	for in, want := range cases {
		if got := sanitizeConsumerToken(in); got != want {
			t.Errorf("sanitizeConsumerToken(%q) = %q, want %q", in, got, want)
		}
	}
}
