package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/memory-efficient-cache-system/cache"
)

func main() {
	config := cache.Config{
		MaxSize:     1024 * 1024,
		DefaultTTL:  time.Minute * 5,
		CleanupTick: time.Minute,
	}

	c := cache.NewWithConfig(config)
	defer c.Close()

	// Add a callback for logging cache events
	c.AddCallback(func(eventType cache.EventType, key string, value interface{}) {
		log.Printf("Cache event: %s, Key: %s, Value: %v\n", eventType, key, value)
	})

	// Add a callback for monitoring evictions
	c.AddCallback(func(eventType cache.EventType, key string, value interface{}) {
		if eventType == cache.ItemEvicted {
			// You could send metrics to your monitoring system here
			log.Printf("WARNING: Item evicted - Key: %s\n", key)
		}
	})

	// Add a callback to monitor warming
	c.AddCallback(func(eventType cache.EventType, key string, value interface{}) {
		log.Printf("Cache event: %s, Key: %s\n", eventType, key)
	})

	// Create a warmer function
	warmer := func(ctx context.Context) map[string]cache.WarmingEntry {
		// This could fetch data from a database, API, etc.
		return map[string]cache.WarmingEntry{
			"user:1": {
				Value: map[string]interface{}{"name": "Alice", "age": 30},
				Size:  100,
			},
			"user:2": {
				Value: map[string]interface{}{"name": "Bob", "age": 25},
				Size:  100,
				TTL:   ptr(time.Hour), // Custom TTL
			},
			// ... more entries
		}
	}

	// Warm the cache with custom options
	opts := cache.WarmingOptions{
		Concurrent:      true,
		ContinueOnError: true,
		BatchSize:       50,
	}

	ctx := context.Background()
	if err := c.WarmCache(ctx, warmer, &opts); err != nil {
		log.Printf("Warning: cache warming had errors: %v", err)
	}

	// Add some items
	c.Set("key1", "value1", 100)
	c.Set("key2", map[string]interface{}{
		"nested": "value2",
	}, 200)

	// Save cache to file
	err := c.SaveToFile("cache_backup.json", "json")
	if err != nil {
		log.Fatal(err)
	}

	// Clear the cache
	c.Clear()

	// Load cache from file
	err = c.LoadFromFile("cache_backup.json", "json")
	if err != nil {
		log.Fatal(err)
	}

	// Verify loaded data
	if val, exists := c.Get("key1"); exists {
		fmt.Printf("Retrieved: %v\n", val)
	}
}

// Helper function to get pointer to duration
func ptr(d time.Duration) *time.Duration {
	return &d
}
