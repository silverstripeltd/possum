package possum

type Change struct {
	ID             *string         // AWS unique identifier
	Name           string          // human readable identifier
	Action         ScheduledAction // start or stop action
	Type           string
	minSize        int64 // some resources have a number of resources
	currentMinSize int64 // some resources have a number of resources
}

type Changes []Change

func (c Changes) Append(o Changes) Changes {
	return append(c, o...)
}
