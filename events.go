package cache

import "time"

type EventType int

const (
	ItemAdded EventType = iota
	ItemAccessed
	ItemEvicted
	ItemExpired
	ItemDeleted
	ItemWarmed
	ItemWarmingFailed
)

func (e EventType) String() string {
	return [...]string{
		"ItemAdded",
		"ItemAccessed",
		"ItemEvicted",
		"ItemExpired",
		"ItemDeleted",
		"ItemWarmed",
		"ItemWarmingFailed",
	}[e]
}

type EventCallback func(EventType, string, interface{})

type CacheEvent struct {
	Type      EventType
	Key       string
	Value     interface{}
	Timestamp time.Time
}
