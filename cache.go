package cache

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type Item struct {
	Value      interface{}
	Expiration int64
	Created    time.Time
	LastAccess time.Time
	Size       int64
	next       *Item
	prev       *Item
	key        string
}

type Stats struct {
	Hits        uint64
	Misses      uint64
	Evictions   uint64
	Expirations uint64
	ItemsCount  uint64
}

type Cache struct {
	items       map[string]*Item
	mu          sync.RWMutex
	maxSize     int64
	currSize    int64
	head        *Item
	tail        *Item
	defaultTTL  time.Duration
	cleanupTick time.Duration
	stopCleanup chan bool
	stats       Stats
	statsMu     sync.RWMutex
	callbacks   []EventCallback
	eventsMu    sync.RWMutex
}

type Config struct {
	MaxSize     int64
	DefaultTTL  time.Duration
	CleanupTick time.Duration
}

type SerializedItem struct {
	Value      interface{} `json:"value"`
	Expiration int64       `json:"expiration"`
	Created    time.Time   `json:"created"`
	LastAccess time.Time   `json:"last_access"`
	Size       int64       `json:"size"`
	Key        string      `json:"key"`
}

type SerializedCache struct {
	Items   []SerializedItem `json:"items"`
	MaxSize int64            `json:"max_size"`
	Stats   Stats            `json:"stats"`
}

type Option func(*operationOptions)

type operationOptions struct {
	expiration time.Duration
}

func WithExpiration(d time.Duration) Option {
	return func(opts *operationOptions) {
		opts.expiration = d
	}
}

func NewWithConfig(config Config) *Cache {
	cache := &Cache{
		items:       make(map[string]*Item),
		maxSize:     config.MaxSize,
		defaultTTL:  config.DefaultTTL,
		cleanupTick: config.CleanupTick,
		stopCleanup: make(chan bool),
	}

	go cache.startCleanup()
	return cache
}

func New(maxSize int64) *Cache {
	return &Cache{
		items:    make(map[string]*Item),
		maxSize:  maxSize,
		currSize: 0,
	}
}

func (c *Cache) moveToFront(item *Item) {
	if item == c.head {
		return
	}

	if item.prev != nil {
		item.prev.next = item.next
	}
	if item.next != nil {
		item.next.prev = item.prev
	}
	if item == c.tail {
		c.tail = item.prev
	}

	item.next = c.head
	item.prev = nil
	if c.head != nil {
		c.head.prev = item
	}
	c.head = item
	if c.tail == nil {
		c.tail = item
	}
}

func (c *Cache) evict(requiredSize int64) {
	for c.currSize+requiredSize > c.maxSize && c.tail != nil {
		evictedKey := c.tail.key
		evictedValue := c.tail.Value

		lru := c.tail
		c.tail = c.tail.prev
		if c.tail != nil {
			c.tail.next = nil
		}

		delete(c.items, lru.key)
		c.currSize -= lru.Size

		c.notifyCallbacks(ItemEvicted, evictedKey, evictedValue)
	}

	c.incrementStat(&c.stats.Evictions)
}

func (c *Cache) SetWithTTL(key string, value interface{}, size int64, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if size > c.maxSize {
		return fmt.Errorf("item size exceeds cache maximum size")
	}

	expiration := time.Now().Add(ttl).UnixNano()
	return c.set(key, value, size, expiration)
}

func (c *Cache) Set(key string, value interface{}, size int64, opts ...Option) error {
	options := &operationOptions{
		expiration: c.defaultTTL,
	}
	for _, opt := range opts {
		opt(options)
	}

	return c.SetWithTTL(key, value, size, options.expiration)
}

func (c *Cache) set(key string, value interface{}, size int64, expiration int64) error {
	if existing, exists := c.items[key]; exists {
		c.currSize -= existing.Size
		if existing.prev != nil {
			existing.prev.next = existing.next
		}
		if existing.next != nil {
			existing.next.prev = existing.prev
		}
		if existing == c.head {
			c.head = existing.next
		}
		if existing == c.tail {
			c.tail = existing.prev
		}
	}

	c.evict(size)

	item := &Item{
		Value:      value,
		Expiration: expiration,
		Created:    time.Now(),
		LastAccess: time.Now(),
		Size:       size,
		key:        key,
	}

	if c.head != nil {
		c.head.prev = item
		item.next = c.head
	}
	c.head = item
	if c.tail == nil {
		c.tail = item
	}

	c.items[key] = item
	c.currSize += size

	c.statsMu.Lock()
	if _, exists := c.items[key]; !exists {
		c.stats.ItemsCount++
	}
	c.statsMu.Unlock()

	c.notifyCallbacks(ItemAdded, key, value)

	return nil
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, exists := c.items[key]
	if !exists {
		c.incrementStat(&c.stats.Misses)
		return nil, false
	}

	if time.Now().UnixNano() > item.Expiration {
		c.delete(key)
		c.incrementStat(&c.stats.Expirations)
		c.incrementStat(&c.stats.Misses)
		c.notifyCallbacks(ItemExpired, key, item.Value)
		return nil, false
	}

	item.LastAccess = time.Now()
	c.moveToFront(item)
	c.incrementStat(&c.stats.Hits)
	c.notifyCallbacks(ItemAccessed, key, item.Value)
	return item.Value, true
}

func (c *Cache) delete(key string) {
	if item, exists := c.items[key]; exists {
		c.currSize -= item.Size
		if item.prev != nil {
			item.prev.next = item.next
		}
		if item.next != nil {
			item.next.prev = item.prev
		}
		if item == c.head {
			c.head = item.next
		}
		if item == c.tail {
			c.tail = item.prev
		}
		delete(c.items, key)

		c.statsMu.Lock()
		c.stats.ItemsCount--
		c.statsMu.Unlock()

		c.notifyCallbacks(ItemDeleted, key, item.Value)
	}
}

func (c *Cache) startCleanup() {
	ticker := time.NewTicker(c.cleanupTick)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCleanup:
			return
		}
	}
}

func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UnixNano()
	expiredCount := 0
	for key, item := range c.items {
		if now > item.Expiration {
			c.delete(key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		c.incrementStat(&c.stats.Expirations)
	}
}

func (c *Cache) Close() {
	c.stopCleanup <- true
}

func (c *Cache) GetStats() Stats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	return c.stats
}

func (c *Cache) incrementStat(stat *uint64) {
	c.statsMu.Lock()
	*stat++
	c.statsMu.Unlock()
}

func (c *Cache) ResetStats() {
	c.statsMu.Lock()
	c.stats = Stats{
		ItemsCount: uint64(len(c.items)),
	}
	c.statsMu.Unlock()
}

func (c *Cache) SaveToFile(filename string, format string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	serialized := SerializedCache{
		Items:   make([]SerializedItem, 0, len(c.items)),
		MaxSize: c.maxSize,
		Stats:   c.GetStats(),
	}

	for key, item := range c.items {
		if time.Now().UnixNano() > item.Expiration {
			continue
		}

		serialized.Items = append(serialized.Items, SerializedItem{
			Value:      item.Value,
			Expiration: item.Expiration,
			Created:    item.Created,
			LastAccess: item.LastAccess,
			Size:       item.Size,
			Key:        key,
		})
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	switch format {
	case "json":
		encoder := json.NewEncoder(file)
		return encoder.Encode(serialized)
	case "gob":
		encoder := gob.NewEncoder(file)
		return encoder.Encode(serialized)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func (c *Cache) LoadFromFile(filename string, format string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	var serialized SerializedCache

	switch format {
	case "json":
		decoder := json.NewDecoder(file)
		err = decoder.Decode(&serialized)
	case "gob":
		decoder := gob.NewDecoder(file)
		err = decoder.Decode(&serialized)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to decode cache: %v", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*Item)
	c.head = nil
	c.tail = nil
	c.currSize = 0

	c.maxSize = serialized.MaxSize

	now := time.Now().UnixNano()
	for _, sItem := range serialized.Items {
		if now > sItem.Expiration {
			continue
		}

		item := &Item{
			Value:      sItem.Value,
			Expiration: sItem.Expiration,
			Created:    sItem.Created,
			LastAccess: sItem.LastAccess,
			Size:       sItem.Size,
			key:        sItem.Key,
		}

		if c.head != nil {
			c.head.prev = item
			item.next = c.head
		}
		c.head = item
		if c.tail == nil {
			c.tail = item
		}

		c.items[sItem.Key] = item
		c.currSize += sItem.Size
	}

	c.statsMu.Lock()
	c.stats = serialized.Stats
	c.statsMu.Unlock()

	return nil
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*Item)
	c.head = nil
	c.tail = nil
	c.currSize = 0
	c.ResetStats()
}

func (c *Cache) AddCallback(callback EventCallback) {
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	c.callbacks = append(c.callbacks, callback)
}

func (c *Cache) RemoveCallbacks() {
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	c.callbacks = nil
}

func (c *Cache) notifyCallbacks(eventType EventType, key string, value interface{}) {
	c.eventsMu.RLock()
	defer c.eventsMu.RUnlock()

	for _, callback := range c.callbacks {
		go callback(eventType, key, value)
	}
}

func (c *Cache) WarmCache(ctx context.Context, warmer WarmerFunc, opts *WarmingOptions) error {
	if opts == nil {
		opts = &DefaultWarmingOptions
	}

	entries := warmer(ctx)
	if len(entries) == 0 {
		return nil
	}

	if !opts.Concurrent {
		return c.warmSequentially(ctx, entries)
	}
	return c.warmConcurrently(ctx, entries, opts.BatchSize)
}

func (c *Cache) warmSequentially(ctx context.Context, entries map[string]WarmingEntry) error {
	for key, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			expiration := c.defaultTTL
			if entry.TTL != nil {
				expiration = *entry.TTL
			}

			if err := c.Set(key, entry.Value, entry.Size, WithExpiration(expiration)); err != nil {
				c.notifyCallbacks(ItemWarmingFailed, key, entry.Value)
				return fmt.Errorf("failed to warm key %s: %w", key, err)
			}
			c.notifyCallbacks(ItemWarmed, key, entry.Value)
		}
	}
	return nil
}

func (c *Cache) warmConcurrently(ctx context.Context, entries map[string]WarmingEntry, batchSize int) error {
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, len(entries))
		sem     = make(chan struct{}, batchSize)
	)

	for key, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			wg.Add(1)
			go func(k string, e WarmingEntry) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				expiration := c.defaultTTL
				if e.TTL != nil {
					expiration = *e.TTL
				}

				if err := c.Set(k, e.Value, e.Size, WithExpiration(expiration)); err != nil {
					c.notifyCallbacks(ItemWarmingFailed, k, e.Value)
					errChan <- fmt.Errorf("failed to warm key %s: %w", k, err)
					return
				}
				c.notifyCallbacks(ItemWarmed, k, e.Value)
			}(key, entry)
		}
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("warming errors: %v", errors)
	}
	return nil
}
