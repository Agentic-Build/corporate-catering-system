package plants

import "time"

// Plant is a factory site / pickup location.
type Plant struct {
	Code      string
	Label     string
	Address   string
	Active    bool
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}
