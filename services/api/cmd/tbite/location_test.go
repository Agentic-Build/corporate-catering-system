package main

import (
	"testing"
	"time"
)

// appLocation must resolve the embedded tz database (the distroless image has
// no tzdata on disk). A regression here means cutoff math silently runs in UTC.
func TestAppLocation_DefaultsToTaipei(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "")
	loc := appLocation()
	if loc.String() != "Asia/Taipei" {
		t.Fatalf("appLocation() = %q, want Asia/Taipei", loc)
	}
	// Asia/Taipei is a fixed +08:00 offset year-round.
	_, offset := time.Date(2026, 5, 27, 12, 0, 0, 0, loc).Zone()
	if offset != 8*60*60 {
		t.Fatalf("Asia/Taipei offset = %ds, want %ds", offset, 8*60*60)
	}
}

func TestAppLocation_RespectsOverride(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "UTC")
	if loc := appLocation(); loc.String() != "UTC" {
		t.Fatalf("appLocation() = %q, want UTC", loc)
	}
}

// A 17:00 Taipei cutoff for next-day supply must reject an order placed that
// evening (the demo bug: 5/27 still orderable after 5/26 17:00 Taipei). This
// pins the timezone semantics that order.Service.Location relies on.
func TestAppLocation_CutoffBoundary(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "")
	loc := appLocation()
	supply := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(supply.Year(), supply.Month(), supply.Day()-1, 17, 0, 0, 0, loc)

	// 2026-05-26 17:30 Taipei == 09:30 UTC — past cutoff.
	past := time.Date(2026, 5, 26, 9, 30, 0, 0, time.UTC)
	if past.Before(cutoff) {
		t.Fatalf("17:30 Taipei should be at/after the 17:00 cutoff")
	}
	// 2026-05-26 16:30 Taipei == 08:30 UTC — before cutoff.
	before := time.Date(2026, 5, 26, 8, 30, 0, 0, time.UTC)
	if !before.Before(cutoff) {
		t.Fatalf("16:30 Taipei should be before the 17:00 cutoff")
	}
}
