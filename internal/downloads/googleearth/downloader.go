package googleearth

import (
	"context"
	"fmt"
	"image"
	"log"

	"golang.org/x/sync/semaphore"

	"imagery-desktop/internal/cache"
	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/googleearth"
	"imagery-desktop/internal/ratelimit"
)

const (
	// MinSuccessRate is the minimum percentage of tiles needed for a valid download
	MinSuccessRate = 0.3
)

// Downloader handles Google Earth imagery downloads with dependency injection
type Downloader struct {
	geClient          *googleearth.Client
	tileCache         *cache.PersistentTileCache
	downloadPath      string
	progressCallback  func(downloads.DownloadProgress)
	logCallback       func(string)
	rateLimitHandler  *ratelimit.Handler
	trackEventCallback func(string, map[string]interface{})

	// Concurrency control
	semaphore    *semaphore.Weighted
	maxWorkers   int64

	// Tile server for historical tile fetching with epoch fallback
	tileServer TileServerInterface
}

// TileServerInterface defines the interface for fetching tiles with zoom fallback
type TileServerInterface interface {
	FetchHistoricalGETileWithZoomFallback(tile *googleearth.Tile, date, hexDate string, maxFallbackLevels int) ([]byte, int, error)
}

// Config holds configuration for the Downloader
type Config struct {
	GEClient          *googleearth.Client
	TileCache         *cache.PersistentTileCache
	DownloadPath      string
	ProgressCallback  func(downloads.DownloadProgress)
	LogCallback       func(string)
	RateLimitHandler  *ratelimit.Handler
	TrackEventCallback func(string, map[string]interface{})
	MaxWorkers        int
	TileServer        TileServerInterface // For historical downloads with epoch fallback
}

// NewDownloader creates a new Google Earth downloader with all dependencies injected
func NewDownloader(cfg Config) (*Downloader, error) {
	if cfg.GEClient == nil {
		return nil, fmt.Errorf("GEClient is required")
	}
	if cfg.DownloadPath == "" {
		return nil, fmt.Errorf("downloadPath is required")
	}

	maxWorkers := cfg.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = downloads.DefaultWorkers
	}

	return &Downloader{
		geClient:          cfg.GEClient,
		tileCache:         cfg.TileCache,
		downloadPath:      cfg.DownloadPath,
		progressCallback:  cfg.ProgressCallback,
		logCallback:       cfg.LogCallback,
		rateLimitHandler:  cfg.RateLimitHandler,
		trackEventCallback: cfg.TrackEventCallback,
		semaphore:         semaphore.NewWeighted(int64(maxWorkers)),
		maxWorkers:        int64(maxWorkers),
		tileServer:        cfg.TileServer,
	}, nil
}

// emitLog sends a log message via callback if available
func (d *Downloader) emitLog(message string) {
	if d.logCallback != nil {
		d.logCallback(message)
	} else {
		log.Println(message)
	}
}

// emitProgress sends progress update via callback if available
func (d *Downloader) emitProgress(progress downloads.DownloadProgress) {
	if d.progressCallback != nil {
		d.progressCallback(progress)
	}
}

// trackEvent tracks an event via callback if available
func (d *Downloader) trackEvent(event string, properties map[string]interface{}) {
	if d.trackEventCallback != nil {
		d.trackEventCallback(event, properties)
	}
}

// TileBounds represents the bounds of a tile grid
type TileBounds struct {
	MinCol int
	MaxCol int
	MinRow int
	MaxRow int
}

// Cols returns the number of columns in the bounds
func (b TileBounds) Cols() int {
	return b.MaxCol - b.MinCol + 1
}

// Rows returns the number of rows in the bounds
func (b TileBounds) Rows() int {
	return b.MaxRow - b.MinRow + 1
}

// calculateTileBounds calculates the bounds of a tile set
func calculateTileBounds(tiles []*googleearth.Tile) (TileBounds, error) {
	if len(tiles) == 0 {
		return TileBounds{}, fmt.Errorf("no tiles provided")
	}

	bounds := TileBounds{
		MinCol: tiles[0].Column,
		MaxCol: tiles[0].Column,
		MinRow: tiles[0].Row,
		MaxRow: tiles[0].Row,
	}

	for _, tile := range tiles {
		if tile.Column < bounds.MinCol {
			bounds.MinCol = tile.Column
		}
		if tile.Column > bounds.MaxCol {
			bounds.MaxCol = tile.Column
		}
		if tile.Row < bounds.MinRow {
			bounds.MinRow = tile.Row
		}
		if tile.Row > bounds.MaxRow {
			bounds.MaxRow = tile.Row
		}
	}

	return bounds, nil
}

// tileResult represents the result of downloading a tile
type tileResult struct {
	tile    *googleearth.Tile
	data    []byte
	index   int
	success bool
	err     error
}

// TileJob represents a tile download job
type TileJob struct {
	tile  *googleearth.Tile
	index int
}

// validateDownloadRequest validates the download request parameters
func (d *Downloader) validateDownloadRequest(bbox downloads.BoundingBox, zoom int, format string) error {
	// Validate coordinates
	if err := downloads.ValidateCoordinates(bbox, zoom); err != nil {
		return fmt.Errorf("invalid coordinates: %w", err)
	}

	// Validate zoom for Google Earth (max 21)
	if err := downloads.ValidateZoomForProvider(zoom, "google_earth"); err != nil {
		return fmt.Errorf("invalid zoom: %w", err)
	}

	// Validate format
	if format != "tiles" && format != "geotiff" && format != "both" {
		return fmt.Errorf("invalid format %q: must be 'tiles', 'geotiff', or 'both'", format)
	}

	return nil
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// checkSuccessRate validates that enough tiles were successfully downloaded
func checkSuccessRate(successCount, total int) error {
	if successCount == 0 {
		return fmt.Errorf("failed to download any tiles - all attempts failed")
	}

	successRate := float64(successCount) / float64(total)
	if successRate < MinSuccessRate {
		return fmt.Errorf("only %d/%d tiles (%.1f%%) downloaded - below minimum threshold of %.1f%%",
			successCount, total, successRate*100, MinSuccessRate*100)
	}

	return nil
}

// acquireWorker acquires a worker slot from the semaphore
func (d *Downloader) acquireWorker(ctx context.Context) error {
	return d.semaphore.Acquire(ctx, 1)
}

// releaseWorker releases a worker slot back to the semaphore
func (d *Downloader) releaseWorker() {
	d.semaphore.Release(1)
}

// createOutputImage creates a new RGBA image for stitching tiles
func createOutputImage(width, height int) *image.RGBA {
	return image.NewRGBA(image.Rect(0, 0, width, height))
}
