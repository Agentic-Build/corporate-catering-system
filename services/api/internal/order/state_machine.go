package order

var allowedTransitions = map[Status]map[Status]bool{
	StatusDraft:     {StatusPlaced: true, StatusCancelled: true},
	StatusPlaced:    {StatusCutoff: true, StatusReady: true, StatusCancelled: true},
	StatusCutoff:    {StatusReady: true, StatusCancelled: true},
	StatusReady:     {StatusPickedUp: true, StatusNoShow: true},
	StatusPickedUp:  {StatusRefunded: true},
	StatusNoShow:    {StatusRefunded: true},
	StatusCancelled: {},
	StatusRefunded:  {},
}

func CanTransition(from, to Status) bool {
	next, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return next[to]
}
