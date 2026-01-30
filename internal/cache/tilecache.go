package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// TileCache provides LRU caching for imagery tiles with disk persistence
type TileCache struct {
	baseDir   string
	maxSize   int64 // Maximum cache size in bytes
	currSize  int64 // Current cache size (atomic)
	mu        sync.RWMutex
	index     map[string]*CacheEntry // In-memory index
	evictChan chan struct{}          // Signal for background eviction
}

// CacheEntry represents a cached tile
type CacheEntry struct {
	Key        string
	FilePath   string
	Size       int64
	AccessTime time.Time
	CreateTime time.Time
}

// NewTileCache creates a new tile cache with the specified directory and max size
func NewTileCache(baseDir string, maxSizeMB int) (*TileCache, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &TileCache{
		baseDir:   baseDir,
		maxSize:   int64(maxSizeMB) * 1024 * 1024,
		currSize:  0,
		index:     make(map[string]*CacheEntry),
		evictChan: make(chan struct{}, 1),
	}

	// Load existing cache index
	if err := cache.loadIndex(); err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Start background eviction goroutine
	go cache.evictionWorker()

	return cache, nil
}

// Get retrieves a tile from cache
func (c *TileCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	entry, exists := c.index[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Read from disk
	data, err := os.ReadFile(entry.FilePath)
	if err != nil {
		// File doesn't exist or error reading, remove from index
		c.mu.Lock()
		delete(c.index, key)
		c.mu.Unlock()
		atomic.AddInt64(&c.currSize, -entry.Size)
		return nil, false
	}

	// Update access time
	c.mu.Lock()
	entry.AccessTime = time.Now()
	c.mu.Unlock()

	return data, true
}

// Set stores a tile in cache
func (c *TileCache) Set(key string, data []byte) error {
	size := int64(len(data))

	// Generate file path from key hash (avoid filesystem limits)
	hash := sha256.Sum256([]byte(key))
	hashStr := hex.EncodeToString(hash[:])
	filePath := filepath.Join(c.baseDir, hashStr[:2], hashStr+".jpg")

	// Create subdirectory
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create cache subdirectory: %w", err)
	}

	// Write to disk
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Update index
	now := time.Now()
	entry := &CacheEntry{
		Key:        key,
		FilePath:   filePath,
		Size:       size,
		AccessTime: now,
		CreateTime: now,
	}

	c.mu.Lock()
	// Remove old entry if exists
	if oldEntry, exists := c.index[key]; exists {
		atomic.AddInt64(&c.currSize, -oldEntry.Size)
		os.Remove(oldEntry.FilePath) // Best effort cleanup
	}
	c.index[key] = entry
	c.mu.Unlock()

	atomic.AddInt64(&c.currSize, size)

	// Trigger eviction if needed
	if atomic.LoadInt64(&c.currSize) > c.maxSize {
		select {
		case c.evictChan <- struct{}{}:
		default: // Already signaled
		}
	}

	return nil
}

// evictionWorker runs in background and evicts old tiles when cache is full
func (c *TileCache) evictionWorker() {
	for range c.evictChan {
		c.evict()
	}
}

// evict removes least recently used tiles until cache is under max size
func (c *TileCache) evict() {
	c.mu.Lock()
	defer c.mu.Unlock()

	currSize := atomic.LoadInt64(&c.currSize)
	if currSize <= c.maxSize {
		return
	}

	// Calculate target size (90% of max to avoid thrashing)
	targetSize := c.maxSize * 9 / 10

	// Build sorted list of entries by access time (oldest first)
	type sortEntry struct {
		key        string
		accessTime time.Time
		size       int64
	}

	entries := make([]sortEntry, 0, len(c.index))
	for key, entry := range c.index {
		entries = append(entries, sortEntry{
			key:        key,
			accessTime: entry.AccessTime,
			size:       entry.Size,
		})
	}

	// Sort by access time (oldest first)
	// Using simple bubble sort for small datasets (cache typically has < 1000 entries)
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].accessTime.After(entries[j].accessTime) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove oldest entries until under target size
	for _, e := range entries {
		if currSize <= targetSize {
			break
		}

		entry := c.index[e.key]
		os.Remove(entry.FilePath) // Best effort cleanup
		delete(c.index, e.key)
		atomic.AddInt64(&c.currSize, -entry.Size)
		currSize -= entry.Size
	}
}

// loadIndex scans cache directory and builds in-memory index
func (c *TileCache) loadIndex() error {
	// Walk cache directory
	return filepath.Walk(c.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		if info.IsDir() {
			return nil
		}

		// Only process .jpg files
		if filepath.Ext(path) != ".jpg" {
			return nil
		}

		// Reconstruct key from filename (this is lossy, but sufficient for rebuilding index)
		// In production, you'd store metadata separately
		hashStr := filepath.Base(path)
		hashStr = hashStr[:len(hashStr)-4] // Remove .jpg extension

		entry := &CacheEntry{
			Key:        hashStr, // Use hash as key for now
			FilePath:   path,
			Size:       info.Size(),
			AccessTime: info.ModTime(),
			CreateTime: info.ModTime(),
		}

		c.index[hashStr] = entry
		atomic.AddInt64(&c.currSize, info.Size())

		return nil
	})
}

// Stats returns cache statistics
func (c *TileCache) Stats() (entries int, sizeBytes int64, maxBytes int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.index), atomic.LoadInt64(&c.currSize), c.maxSize
}

// Clear removes all cached tiles
func (c *TileCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove all files
	for _, entry := range c.index {
		os.Remove(entry.FilePath)
	}

	// Clear index
	c.index = make(map[string]*CacheEntry)
	atomic.StoreInt64(&c.currSize, 0)

	return nil
}
