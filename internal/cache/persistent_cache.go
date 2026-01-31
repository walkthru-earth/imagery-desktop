package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PersistentTileCache provides disk-based caching with OGC ZXY structure
// Cache persists across app restarts and uses standard tile directory layout
type PersistentTileCache struct {
	baseDir   string
	maxSize   int64 // Maximum cache size in bytes
	currSize  int64 // Current cache size (atomic)
	ttl       time.Duration
	mu        sync.RWMutex
	metadata  map[string]*TileMetadata // Persistent metadata index
	evictChan chan struct{}
}

// TileMetadata stores information about a cached tile
type TileMetadata struct {
	Key        string    `json:"key"`
	Provider   string    `json:"provider"` // "google", "esri", etc.
	Z          int       `json:"z"`
	X          int       `json:"x"`
	Y          int       `json:"y"`
	Date       string    `json:"date,omitempty"`   // For historical imagery
	Size       int64     `json:"size"`
	AccessTime time.Time `json:"accessTime"`
	CreateTime time.Time `json:"createTime"`
}

// NewPersistentTileCache creates a new persistent tile cache
// Cache structure: baseDir/{provider}/{z}/{x}/{y}.jpg
// Metadata index: baseDir/cache_index.json
func NewPersistentTileCache(baseDir string, maxSizeMB int, ttlDays int) (*PersistentTileCache, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &PersistentTileCache{
		baseDir:   baseDir,
		maxSize:   int64(maxSizeMB) * 1024 * 1024,
		ttl:       time.Duration(ttlDays) * 24 * time.Hour,
		metadata:  make(map[string]*TileMetadata),
		evictChan: make(chan struct{}, 1),
	}

	// Load metadata index from disk
	if err := cache.loadMetadata(); err != nil {
		// If metadata can't be loaded, rebuild it from disk
		if err := cache.rebuildMetadata(); err != nil {
			return nil, fmt.Errorf("failed to initialize cache: %w", err)
		}
	}

	// Start background maintenance
	go cache.maintenanceWorker()

	return cache, nil
}

// Get retrieves a tile from cache
// Key format: "{provider}:{z}:{x}:{y}:{date}"
func (c *PersistentTileCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	meta, exists := c.metadata[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check if tile has expired
	if c.ttl > 0 && time.Since(meta.CreateTime) > c.ttl {
		c.evictTile(key, meta)
		return nil, false
	}

	// Build file path: {provider}/{z}/{x}/{y}.jpg
	filePath := c.buildFilePath(meta)

	// Read from disk
	data, err := os.ReadFile(filePath)
	if err != nil {
		// File missing - remove from metadata
		c.evictTile(key, meta)
		return nil, false
	}

	// Update access time
	c.mu.Lock()
	meta.AccessTime = time.Now()
	c.mu.Unlock()

	// Persist metadata update (async)
	go c.saveMetadata()

	return data, true
}

// Set stores a tile in cache using OGC ZXY structure
func (c *PersistentTileCache) Set(provider string, z, x, y int, date string, data []byte) error {
	key := c.buildKey(provider, z, x, y, date)
	size := int64(len(data))

	// Create metadata
	now := time.Now()
	meta := &TileMetadata{
		Key:        key,
		Provider:   provider,
		Z:          z,
		X:          x,
		Y:          y,
		Date:       date,
		Size:       size,
		AccessTime: now,
		CreateTime: now,
	}

	// Build file path: {provider}/{z}/{x}/{y}.jpg or {provider}/{z}/{x}/{y}_{date}.jpg
	filePath := c.buildFilePath(meta)

	// Create directory structure
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Write to disk
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Update metadata
	c.mu.Lock()
	oldMeta, exists := c.metadata[key]
	if exists {
		atomic.AddInt64(&c.currSize, -oldMeta.Size)
		// Remove old file if path changed
		oldPath := c.buildFilePath(oldMeta)
		if oldPath != filePath {
			os.Remove(oldPath)
		}
	}
	c.metadata[key] = meta
	c.mu.Unlock()

	atomic.AddInt64(&c.currSize, size)

	// Trigger eviction if needed
	if atomic.LoadInt64(&c.currSize) > c.maxSize {
		select {
		case c.evictChan <- struct{}{}:
		default:
		}
	}

	// Save metadata (async)
	go c.saveMetadata()

	return nil
}

// buildKey creates a cache key from tile coordinates
func (c *PersistentTileCache) buildKey(provider string, z, x, y int, date string) string {
	if date == "" {
		return fmt.Sprintf("%s:%d:%d:%d", provider, z, x, y)
	}
	return fmt.Sprintf("%s:%d:%d:%d:%s", provider, z, x, y, date)
}

// buildFilePath creates the OGC ZXY file path for a tile
// Structure: {baseDir}/{provider}/{z}/{x}/{y}.jpg
// Historical: {baseDir}/{provider}/{z}/{x}/{y}_{date}.jpg
func (c *PersistentTileCache) buildFilePath(meta *TileMetadata) string {
	filename := fmt.Sprintf("%d.jpg", meta.Y)
	if meta.Date != "" {
		// Include date in filename for historical imagery
		// Sanitize date for filesystem (replace slashes/colons)
		sanitizedDate := strings.ReplaceAll(meta.Date, "/", "-")
		sanitizedDate = strings.ReplaceAll(sanitizedDate, ":", "-")
		filename = fmt.Sprintf("%d_%s.jpg", meta.Y, sanitizedDate)
	}

	return filepath.Join(c.baseDir, meta.Provider, fmt.Sprintf("%d", meta.Z),
		fmt.Sprintf("%d", meta.X), filename)
}

// evictTile removes a tile from cache
func (c *PersistentTileCache) evictTile(key string, meta *TileMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	filePath := c.buildFilePath(meta)
	os.Remove(filePath)
	delete(c.metadata, key)
	atomic.AddInt64(&c.currSize, -meta.Size)
}

// maintenanceWorker runs periodic cache maintenance
func (c *PersistentTileCache) maintenanceWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.evictChan:
			c.evictOldTiles()
		case <-ticker.C:
			c.evictExpiredTiles()
		}
	}
}

// evictOldTiles removes least recently used tiles when cache is full
func (c *PersistentTileCache) evictOldTiles() {
	c.mu.Lock()
	defer c.mu.Unlock()

	currSize := atomic.LoadInt64(&c.currSize)
	if currSize <= c.maxSize {
		return
	}

	// Target size: 80% of max to avoid thrashing
	targetSize := c.maxSize * 8 / 10

	// Collect all metadata sorted by access time
	type sortEntry struct {
		key        string
		accessTime time.Time
		size       int64
		meta       *TileMetadata
	}

	entries := make([]sortEntry, 0, len(c.metadata))
	for key, meta := range c.metadata {
		entries = append(entries, sortEntry{
			key:        key,
			accessTime: meta.AccessTime,
			size:       meta.Size,
			meta:       meta,
		})
	}

	// Sort by access time (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].accessTime.After(entries[j].accessTime) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Evict oldest until under target size
	for _, e := range entries {
		if currSize <= targetSize {
			break
		}

		filePath := c.buildFilePath(e.meta)
		os.Remove(filePath)
		delete(c.metadata, e.key)
		atomic.AddInt64(&c.currSize, -e.size)
		currSize -= e.size
	}

	// Save updated metadata
	c.saveMetadata()
}

// evictExpiredTiles removes tiles that exceed TTL
func (c *PersistentTileCache) evictExpiredTiles() {
	if c.ttl <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	toEvict := []string{}

	for key, meta := range c.metadata {
		if now.Sub(meta.CreateTime) > c.ttl {
			toEvict = append(toEvict, key)
		}
	}

	for _, key := range toEvict {
		meta := c.metadata[key]
		filePath := c.buildFilePath(meta)
		os.Remove(filePath)
		delete(c.metadata, key)
		atomic.AddInt64(&c.currSize, -meta.Size)
	}

	if len(toEvict) > 0 {
		c.saveMetadata()
	}
}

// loadMetadata loads the metadata index from disk
func (c *PersistentTileCache) loadMetadata() error {
	metaPath := filepath.Join(c.baseDir, "cache_index.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("metadata file not found")
		}
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata map[string]*TileMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	c.metadata = metadata

	// Calculate current size
	var totalSize int64
	for _, meta := range metadata {
		totalSize += meta.Size
	}
	atomic.StoreInt64(&c.currSize, totalSize)

	return nil
}

// saveMetadata saves the metadata index to disk
func (c *PersistentTileCache) saveMetadata() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metaPath := filepath.Join(c.baseDir, "cache_index.json")

	data, err := json.MarshalIndent(c.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write to temp file first, then rename (atomic operation)
	tempPath := metaPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	if err := os.Rename(tempPath, metaPath); err != nil {
		return fmt.Errorf("failed to rename metadata file: %w", err)
	}

	return nil
}

// rebuildMetadata rebuilds the metadata index by scanning the cache directory
func (c *PersistentTileCache) rebuildMetadata() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metadata = make(map[string]*TileMetadata)
	var totalSize int64

	// Walk the cache directory
	err := filepath.Walk(c.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".jpg" {
			return nil
		}

		// Parse path: {baseDir}/{provider}/{z}/{x}/{y}.jpg or {y}_{date}.jpg
		relPath, _ := filepath.Rel(c.baseDir, path)
		parts := strings.Split(relPath, string(os.PathSeparator))

		if len(parts) < 4 {
			return nil // Invalid path structure
		}

		provider := parts[0]
		z, _ := parseIntSafe(parts[1])
		x, _ := parseIntSafe(parts[2])

		// Parse Y and optional date from filename
		filename := strings.TrimSuffix(parts[3], ".jpg")
		var y int
		var date string

		if strings.Contains(filename, "_") {
			// Has date: y_date.jpg
			fileParts := strings.SplitN(filename, "_", 2)
			y, _ = parseIntSafe(fileParts[0])
			date = fileParts[1]
		} else {
			y, _ = parseIntSafe(filename)
		}

		// Create metadata
		key := c.buildKey(provider, z, x, y, date)
		meta := &TileMetadata{
			Key:        key,
			Provider:   provider,
			Z:          z,
			X:          x,
			Y:          y,
			Date:       date,
			Size:       info.Size(),
			AccessTime: info.ModTime(),
			CreateTime: info.ModTime(),
		}

		c.metadata[key] = meta
		totalSize += info.Size()

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to scan cache directory: %w", err)
	}

	atomic.StoreInt64(&c.currSize, totalSize)

	// Save rebuilt metadata
	return c.saveMetadata()
}

// parseIntSafe parses an integer safely, returning 0 on error
func parseIntSafe(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// Stats returns cache statistics
func (c *PersistentTileCache) Stats() (entries int, sizeBytes int64, maxBytes int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.metadata), atomic.LoadInt64(&c.currSize), c.maxSize
}

// Clear removes all cached tiles
func (c *PersistentTileCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove all files
	for _, meta := range c.metadata {
		filePath := c.buildFilePath(meta)
		os.Remove(filePath)
	}

	// Clear metadata
	c.metadata = make(map[string]*TileMetadata)
	atomic.StoreInt64(&c.currSize, 0)

	// Save empty metadata
	return c.saveMetadata()
}

// GetCachePath returns the base directory of the cache
func (c *PersistentTileCache) GetCachePath() string {
	return c.baseDir
}
