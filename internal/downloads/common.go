package downloads

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// BoundingBox represents a geographic bounding box
type BoundingBox struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

// DownloadProgress tracks the progress of a download operation
type DownloadProgress struct {
	Downloaded  int    `json:"downloaded"`
	Total       int    `json:"total"`
	Percent     int    `json:"percent"`
	Status      string `json:"status"`
	CurrentDate int    `json:"currentDate"` // For range downloads (1-based)
	TotalDates  int    `json:"totalDates"`  // For range downloads
}

// GEDateInfo contains date information for Google Earth historical imagery
type GEDateInfo struct {
	Date    string `json:"date"`    // Human-readable date (YYYY-MM-DD)
	HexDate string `json:"hexDate"` // Hex date for Google API
	Epoch   int    `json:"epoch"`   // Primary epoch from protobuf
}

// GEAvailableDate represents an available Google Earth historical imagery date
type GEAvailableDate struct {
	Date    string `json:"date"`
	Epoch   int    `json:"epoch"`
	HexDate string `json:"hexDate"`
}

// Constants for validation
const (
	MinZoom = 0
	MaxZoom = 23 // Conservative max for both Esri (23) and Google Earth (21)

	MinLat = -85.051129 // Web Mercator limit
	MaxLat = 85.051129
	MinLon = -180.0
	MaxLon = 180.0

	DefaultWorkers = 10 // Default number of concurrent download workers
	TileSize       = 256 // Standard tile size in pixels (256x256)
)

// Provider-specific max zoom levels
const (
	MaxZoomEsri        = 23
	MaxZoomGoogleEarth = 21
)

// Validate checks if the bounding box is valid
func (b BoundingBox) Validate() error {
	if b.South >= b.North {
		return fmt.Errorf("south (%f) must be less than north (%f)", b.South, b.North)
	}
	if b.West >= b.East {
		return fmt.Errorf("west (%f) must be less than east (%f)", b.West, b.East)
	}
	if b.South < -90 || b.North > 90 {
		return fmt.Errorf("latitude out of range [-90, 90]: south=%f, north=%f", b.South, b.North)
	}
	if b.West < -180 || b.East > 180 {
		return fmt.Errorf("longitude out of range [-180, 180]: west=%f, east=%f", b.West, b.East)
	}
	return nil
}

// ValidateCoordinates validates zoom level and bounding box
func ValidateCoordinates(bbox BoundingBox, zoom int) error {
	if zoom < MinZoom || zoom > MaxZoom {
		return fmt.Errorf("zoom level %d out of range [%d, %d]", zoom, MinZoom, MaxZoom)
	}
	return bbox.Validate()
}

// ValidateTileCoordinates validates individual tile coordinates
func ValidateTileCoordinates(z, x, y int) error {
	if z < MinZoom || z > MaxZoom {
		return fmt.Errorf("zoom %d out of range [%d, %d]", z, MinZoom, MaxZoom)
	}

	maxTile := (1 << z) - 1
	if x < 0 || x > maxTile {
		return fmt.Errorf("x %d out of range [0, %d] for zoom %d", x, maxTile, z)
	}
	if y < 0 || y > maxTile {
		return fmt.Errorf("y %d out of range [0, %d] for zoom %d", y, maxTile, z)
	}

	return nil
}

// ValidateZoomForProvider validates zoom level against provider-specific limits
func ValidateZoomForProvider(zoom int, provider string) error {
	var maxZoom int
	switch provider {
	case "esri_wayback":
		maxZoom = MaxZoomEsri
	case "google_earth":
		maxZoom = MaxZoomGoogleEarth
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}

	if zoom > maxZoom {
		return fmt.Errorf("zoom %d exceeds maximum %d for %s", zoom, maxZoom, provider)
	}
	if zoom < MinZoom {
		return fmt.Errorf("zoom %d is below minimum %d", zoom, MinZoom)
	}
	return nil
}

// ValidateCachePath validates that a file path is within the cache directory
// This prevents path traversal attacks from malicious input
func ValidateCachePath(cacheDir, filePath string) error {
	if cacheDir == "" || filePath == "" {
		return fmt.Errorf("cache directory or file path is empty")
	}

	absCacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for cache directory: %w", err)
	}

	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for file: %w", err)
	}

	relPath, err := filepath.Rel(absCacheDir, absFilePath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	// Check for path traversal attempts
	if strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("path traversal attempt detected: %s is outside cache directory %s", filePath, cacheDir)
	}

	return nil
}

// RangeTracker tracks progress across multiple date downloads
type RangeTracker struct {
	currentDate int
	totalDates  int
	mu          sync.Mutex
}

// NewRangeTracker creates a new range tracker
func NewRangeTracker(totalDates int) *RangeTracker {
	return &RangeTracker{
		totalDates: totalDates,
		currentDate: 0,
	}
}

// SetCurrentDate updates the current date index (1-based)
func (rt *RangeTracker) SetCurrentDate(index int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.currentDate = index
}

// GetProgress returns the current date and total dates
func (rt *RangeTracker) GetProgress() (current, total int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.currentDate, rt.totalDates
}

// IncrementDate increments the current date counter and returns the new value
func (rt *RangeTracker) IncrementDate() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.currentDate++
	return rt.currentDate
}
