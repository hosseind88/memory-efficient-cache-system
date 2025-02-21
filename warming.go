package cache

import (
	"context"
	"time"
)

type WarmerFunc func(context.Context) map[string]WarmingEntry

type WarmingEntry struct {
	Value interface{}
	Size  int64
	TTL   *time.Duration
}

type WarmingOptions struct {
	Concurrent      bool
	ContinueOnError bool
	BatchSize       int
}

var DefaultWarmingOptions = WarmingOptions{
	Concurrent:      true,
	ContinueOnError: true,
	BatchSize:       100,
}
