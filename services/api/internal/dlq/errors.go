package dlq

import "errors"

// ErrMessageNotFound is returned when the row does not exist.
var ErrMessageNotFound = errors.New("dlq: message not found")

// ErrAlreadyResolved is returned when MarkReplayed/MarkResolved targets a row
// that has already been replayed or resolved.
var ErrAlreadyResolved = errors.New("dlq: already replayed or resolved")
