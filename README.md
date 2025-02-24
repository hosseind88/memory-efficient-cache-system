# Go Memory-Efficient Cache System

A high-performance, in-memory caching system implemented in Go, designed to provide efficient data storage and retrieval while maintaining optimal memory usage through smart eviction policies.

## Features

- **Thread-Safe Operations**: Concurrent access handling using Go's synchronization primitives
- **Memory Management**: Efficient memory utilization with size tracking and constraints
- **Eviction Policies**: Implementation of LRU (Least Recently Used) cache eviction strategy
- **TTL Support**: Automatic expiration of cached items based on Time-To-Live
- **Background Cleanup**: Goroutines for automatic memory management and expired item removal
- **Channel-Based Communication**: Efficient inter-goroutine communication for cache operations
- **Configurable Cache Settings**: Customizable cache size, TTL defaults, and eviction policies

## Key Components

- Thread-safe concurrent operations
- Memory size tracking and management
- Background garbage collection
- Eviction policy implementation
- TTL (Time-To-Live) management
- Channel-based notifications

## Project Status

🚧 Under Development
