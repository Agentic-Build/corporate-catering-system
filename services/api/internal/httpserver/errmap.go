package httpserver

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
)

// Rule maps a sentinel error to an HTTP status. Optional Detail overrides
// err.Error() as the response message when non-empty.
type Rule struct {
	Err    error
	Status int
	Detail string
}

// Map walks rules in order; first errors.Is match wins. Returns nil for nil err.
// If no rule matches, returns nil so the caller can apply its own fallback.
func Map(err error, rules []Rule) error {
	if err == nil {
		return nil
	}
	for _, r := range rules {
		if errors.Is(err, r.Err) {
			msg := r.Detail
			if msg == "" {
				msg = err.Error()
			}
			return huma.NewError(r.Status, msg)
		}
	}
	return nil
}
