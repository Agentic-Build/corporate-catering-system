package httpserver

import "time"

// FormatTimePtr returns nil for a zero time, else &"<RFC3339 UTC>".
func FormatTimePtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// FormatNullableTimePtr returns nil for nil or zero, else &"<RFC3339 UTC>".
func FormatNullableTimePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
