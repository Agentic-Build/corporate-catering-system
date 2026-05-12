package quota

import "errors"

var (
	ErrSupplyNotFound = errors.New("quota: supply not found")
	ErrOutOfStock     = errors.New("quota: out of stock")
)
