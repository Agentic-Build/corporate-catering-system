package plants

import "errors"

var (
	ErrPlantNotFound = errors.New("plant: not found")
	ErrDuplicateCode = errors.New("plant: code already exists")
	ErrInvalid       = errors.New("plant: invalid")
)
