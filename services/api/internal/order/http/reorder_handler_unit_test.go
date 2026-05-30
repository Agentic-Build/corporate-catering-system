package ohttp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// allUnavailableDetail.Error() satisfies the error interface so the detail can
// be returned as an error value; huma renders it via ErrorDetail() rather than
// Error(), so this white-box test drives Error() directly.
func TestAllUnavailableDetail_Error(t *testing.T) {
	d := &allUnavailableDetail{Items: []unavailableItemDTO{{MenuItemID: "m1", Name: "Dish", Reason: "archived"}}}
	assert.Equal(t, "all_items_unavailable", d.Error())
}
