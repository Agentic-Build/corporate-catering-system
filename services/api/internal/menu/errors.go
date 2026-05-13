package menu

import "errors"

var (
	ErrCategoryNotFound = errors.New("menu: category not found")
	ErrItemNotFound     = errors.New("menu: item not found")
	ErrImageNotFound    = errors.New("menu: image not found")
	ErrForbidden        = errors.New("menu: forbidden (ownership mismatch)")
)
