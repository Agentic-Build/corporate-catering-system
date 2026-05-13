package compliance

import "errors"

var (
	ErrDocumentNotFound = errors.New("compliance: document not found")
	ErrAnomalyNotFound  = errors.New("compliance: anomaly not found")
	ErrInvalidStatus    = errors.New("compliance: invalid status transition")
)
