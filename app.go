package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg" // For PNG encoding
	"image/png"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"imagery-desktop/pkg/geotiff"

	"github.com/posthog/posthog-go"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"imagery-desktop/internal/cache"
	"imagery-desktop/internal/config"
	"imagery-desktop/internal/esri"
	"imagery-desktop/internal/googleearth"
	"imagery-desktop/internal/imagery"
	"imagery-desktop/internal/taskqueue"
	"imagery-desktop/internal/video"

	_ "golang.org/x/image/tiff" // Register TIFF decoder for GeoTIFF loading
)

//go:embed frontend/src/assets/images/icon.png
var logoImageData []byte

//go:embed assets/fonts/ArialUnicode.ttf
var dateFontData []byte

// Linker flags
var (
	PostHogKey  string
	PostHogHost string
	AppVersion  string = "0.0.0-dev"
)

// ImagerySource represents the source of imagery
type ImagerySource string

const (
	SourceGoogleEarth ImagerySource = "google_earth"
	SourceEsriWayback ImagerySource = "esri_wayback"

	// Number of concurrent download workers
	DownloadWorkers = 10
	TileSize        = 256
)

// Helper function for max of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// generateQuadkey generates a quadkey string for a tile at zoom level z covering a bbox
// Uses the center tile as reference
func generateQuadkey(south, west, north, east float64, zoom int) string {
	centerLat := (south + north) / 2
	centerLon := (west + east) / 2

	// Convert to tile coordinates (Web Mercator)
	n := math.Pow(2, float64(zoom))
	x := int((centerLon + 180.0) / 360.0 * n)
	y := int((1.0 - math.Log(math.Tan(centerLat*math.Pi/180.0)+1.0/math.Cos(centerLat*math.Pi/180.0))/math.Pi) / 2.0 * n)

	// Generate quadkey from x, y, z
	var quadkey strings.Builder
	for i := zoom; i > 0; i-- {
		digit := 0
		mask := 1 << (i - 1)
		if (x & mask) != 0 {
			digit++
		}
		if (y & mask) != 0 {
			digit += 2
		}
		quadkey.WriteByte(byte('0' + digit))
	}
	return quadkey.String()
}

// generateBBoxString creates a human-readable bbox string for filenames
func generateBBoxString(south, west, north, east float64) string {
	return fmt.Sprintf("%.4f_%.4f_%.4f_%.4f", south, west, north, east)
}

// sanitizeCoordinate formats a coordinate for use in filenames (removes minus sign, uses N/S/E/W)
// Replaces decimal point with 'p' for Windows compatibility
func sanitizeCoordinate(coord float64, isLat bool) string {
	dir := "E"
	if isLat {
		if coord < 0 {
			dir = "S"
		} else {
			dir = "N"
		}
	} else {
		if coord < 0 {
			dir = "W"
		} else {
			dir = "E"
		}
	}
	// Format and replace decimal point with 'p'
	coordStr := fmt.Sprintf("%.4f", math.Abs(coord))
	coordStr = strings.Replace(coordStr, ".", "p", 1)
	return coordStr + dir
}

// generateGeoTIFFFilename creates a standardized GeoTIFF filename with metadata
// Format: {source}_{date}_{quadkey}_z{zoom}_{bbox}.tif
func generateGeoTIFFFilename(source, date string, bbox BoundingBox, zoom int) string {
	quadkey := generateQuadkey(bbox.South, bbox.West, bbox.North, bbox.East, zoom)

	// Short bbox representation for filename
	bboxStr := fmt.Sprintf("%s-%s_%s-%s",
		sanitizeCoordinate(bbox.South, true),
		sanitizeCoordinate(bbox.North, true),
		sanitizeCoordinate(bbox.West, false),
		sanitizeCoordinate(bbox.East, false))

	return fmt.Sprintf("%s_%s_%s_z%d_%s.tif", source, date, quadkey, zoom, bboxStr)
}

// generateTilesDirName creates a standardized tiles directory name
// Format: {source}_{date}_z{zoom}_tiles
func generateTilesDirName(source, date string, zoom int) string {
	return fmt.Sprintf("%s_%s_z%d_tiles", source, date, zoom)
}

// BoundingBox represents a geographic bounding box
type BoundingBox struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

// AvailableDate represents an available imagery date
type AvailableDate struct {
	Date   string `json:"date"`
	Source string `json:"source"`
}

// TileInfo represents information about tiles in a region
type TileInfo struct {
	TileCount  int     `json:"tileCount"`
	ZoomLevel  int     `json:"zoomLevel"`
	Resolution float64 `json:"resolution"` // meters per pixel
	EstSizeMB  float64 `json:"estSizeMB"`
}

// DownloadProgress represents download progress
type DownloadProgress struct {
	Downloaded  int    `json:"downloaded"`
	Total       int    `json:"total"`
	Percent     int    `json:"percent"`
	Status      string `json:"status"`
	CurrentDate int    `json:"currentDate"` // Current date index in range download (1-based)
	TotalDates  int    `json:"totalDates"`  // Total dates in range download
}

// App struct
type App struct {
	ctx               context.Context
	geClient          *googleearth.Client
	esriClient        *esri.Client
	tileCache         *cache.TileCache
	downloader        *imagery.TileDownloader
	downloadPath      string
	tileServerURL     string
	settings          *config.UserSettings
	mu                sync.Mutex
	devMode           bool // Enable verbose logging in dev mode only
	phClient          posthog.Client
	inRangeDownload   bool // Track if we're downloading a date range (suppress per-tile progress)
	currentDateIndex  int  // Current date being processed in range download
	totalDatesInRange int  // Total dates in range download
	taskQueue         *taskqueue.QueueManager // Task queue for background exports

	// Task queue progress tracking
	currentTaskID     string                          // Current task ID when running in queue mode
	taskProgressChan  chan<- taskqueue.TaskProgress   // Channel to forward progress to task worker
	taskOutputPath    string                          // Output directory for current task
}

// NewApp creates a new App application struct
func NewApp() *App {
	// Load user settings
	settings, err := config.LoadSettings()
	if err != nil {
		log.Printf("Failed to load settings, using defaults: %v", err)
		settings = config.DefaultSettings()
	}
	log.Printf("Settings loaded from: %s", config.GetSettingsPath())

	// Initialize cache with settings
	cacheDir := cache.GetCacheDir()
	tileCache, err := cache.NewTileCache(cacheDir, settings.CacheMaxSizeMB)
	if err != nil {
		log.Printf("Failed to initialize tile cache: %v", err)
		tileCache = nil // Continue without cache
	} else {
		log.Printf("Tile cache initialized at %s (max %d MB)", cacheDir, settings.CacheMaxSizeMB)
	}

	// Initialize unified downloader
	downloader := imagery.NewTileDownloader(DownloadWorkers, tileCache)

	// Initialize PostHog
	var phClient posthog.Client
	if PostHogKey != "" {
		phConfig := posthog.Config{
			Endpoint: PostHogHost,
		}
		client, err := posthog.NewWithConfig(PostHogKey, phConfig)
		if err != nil {
			log.Printf("Failed to initialize PostHog: %v", err)
		} else {
			phClient = client
		}
	}

	// Initialize task queue
	homeDir, _ := os.UserHomeDir()
	queuePath := filepath.Join(homeDir, ".walkthru-earth", "imagery-desktop", "queue")
	taskQueue := taskqueue.NewQueueManager(queuePath, settings.MaxConcurrentTasks)
	log.Printf("Task queue initialized at %s (max concurrent: %d)", queuePath, settings.MaxConcurrentTasks)

	return &App{
		geClient:     googleearth.NewClient(),
		esriClient:   esri.NewClient(),
		tileCache:    tileCache,
		downloader:   downloader,
		downloadPath: settings.DownloadPath,
		settings:     settings,
		phClient:     phClient,
		taskQueue:    taskQueue,
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Create download directory if it doesn't exist
	os.MkdirAll(a.downloadPath, 0755)

	// Initialize clients in background
	go func() {
		if err := a.esriClient.Initialize(); err != nil {
			wailsRuntime.LogError(ctx, fmt.Sprintf("Failed to initialize Esri client: %v", err))
		} else {
			wailsRuntime.LogInfo(ctx, "Esri Wayback client initialized")
		}
	}()

	go func() {
		if err := a.geClient.Initialize(); err != nil {
			wailsRuntime.LogError(ctx, fmt.Sprintf("Failed to initialize Google Earth client: %v", err))
		} else {
			wailsRuntime.LogInfo(ctx, "Google Earth client initialized")
		}
	}()

	// Start local tile server
	go a.StartTileServer()

	// Set up task queue callbacks and executor
	a.taskQueue.SetExecutor(a)
	a.taskQueue.SetCallbacks(
		func(status taskqueue.QueueStatus) {
			wailsRuntime.EventsEmit(ctx, "task-queue-update", status)
		},
		func(tasks []*taskqueue.ExportTask) {
			// Emit full task list for immediate UI updates
			wailsRuntime.EventsEmit(ctx, "task-list-changed", tasks)
		},
		func(taskID string, progress taskqueue.TaskProgress) {
			wailsRuntime.EventsEmit(ctx, "task-progress", map[string]interface{}{
				"taskId":   taskID,
				"progress": progress,
			})
		},
		func(taskID string, success bool, err error) {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			wailsRuntime.EventsEmit(ctx, "task-complete", map[string]interface{}{
				"taskId":  taskID,
				"success": success,
				"error":   errStr,
			})
		},
		func(title, message, notifType string) {
			wailsRuntime.EventsEmit(ctx, "system-notification", map[string]interface{}{
				"title":   title,
				"message": message,
				"type":    notifType,
			})
		},
	)

	// Track app start
	a.TrackEvent("app_started", map[string]interface{}{
		"version": a.GetAppVersion(),
		"os":      goruntime.GOOS,
		"arch":    goruntime.GOARCH,
	})
}

// TrackEvent sends an event to PostHog
func (a *App) TrackEvent(event string, props map[string]interface{}) {
	if a.phClient != nil {
		// Use a distinct ID if possible, for now we use anonymous or machine ID if we had one
		// For desktop apps without login, usually we might generate a UUID and store it in settings
		// Falling back to "anonymous_backend" for now, or better:
		// We could defer to frontend for user tracking, but backend tracking is useful for errors
		a.phClient.Enqueue(posthog.Capture{
			DistinctId: "backend_user", // Ideally should be unique per install
			Event:      event,
			Properties: props,
		})
	}
}

// Shutdown cleans up resources
func (a *App) Shutdown(ctx context.Context) {
	if a.taskQueue != nil {
		a.taskQueue.Close()
	}
	if a.phClient != nil {
		a.phClient.Close()
	}
}

// GetAppVersion returns the current application version
func (a *App) GetAppVersion() string {
	return AppVersion
}

// GetTileInfo calculates tile information for a bounding box
func (a *App) GetTileInfo(bbox BoundingBox, zoom int) TileInfo {
	tiles, _ := esri.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	tileCount := len(tiles)

	// Approximate tile size: 20-50KB per tile for JPEG
	estSizeMB := float64(tileCount) * 0.035 // ~35KB average

	// Resolution at center latitude
	centerLat := (bbox.South + bbox.North) / 2
	resolution := googleearth.ResolutionAtZoom(zoom, centerLat)

	return TileInfo{
		TileCount:  tileCount,
		ZoomLevel:  zoom,
		Resolution: resolution,
		EstSizeMB:  estSizeMB,
	}
}

// GetEsriLayers returns available Esri Wayback layers (dates)
func (a *App) GetEsriLayers() ([]AvailableDate, error) {
	layers, err := a.esriClient.GetLayers()
	if err != nil {
		return nil, err
	}

	dates := make([]AvailableDate, len(layers))
	for i, layer := range layers {
		dates[i] = AvailableDate{
			Date:   layer.Date.Format("2006-01-02"),
			Source: string(SourceEsriWayback),
		}
	}

	return dates, nil
}

// GetAvailableDatesForArea returns available imagery dates for a specific area
// Returns LayerDate (not CaptureDate) since download functions need the layer date to find tiles
func (a *App) GetAvailableDatesForArea(bbox BoundingBox, zoom int) ([]AvailableDate, error) {
	// Get center tile
	centerLat := (bbox.South + bbox.North) / 2
	centerLon := (bbox.West + bbox.East) / 2

	tile, err := esri.GetTileForWgs84(centerLat, centerLon, zoom)
	if err != nil {
		return nil, err
	}

	// Get available dates from Esri
	datedTiles, err := a.esriClient.GetAvailableDates(tile)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var dates []AvailableDate

	for _, dt := range datedTiles {
		// Use LayerDate (Esri Wayback layer date) not CaptureDate
		// LayerDate is needed by download functions to find the correct layer
		dateStr := dt.LayerDate.Format("2006-01-02")
		if !seen[dateStr] {
			seen[dateStr] = true
			dates = append(dates, AvailableDate{
				Date:   dateStr,
				Source: string(SourceEsriWayback),
			})
		}
	}

	return dates, nil
}

// SetDownloadPath sets the download directory
func (a *App) SetDownloadPath(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}

	a.downloadPath = path
	return nil
}

// GetDownloadPath returns the current download directory
func (a *App) GetDownloadPath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.downloadPath
}

// SelectDownloadFolder opens a folder picker dialog
func (a *App) SelectDownloadFolder() (string, error) {
	path, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title:            "Select Download Folder",
		DefaultDirectory: a.downloadPath,
	})
	if err != nil {
		return "", err
	}

	if path != "" {
		a.SetDownloadPath(path)
	}

	return path, nil
}

// emitLog sends a log message to the frontend (only in dev mode)
func (a *App) emitLog(message string) {
	if a.devMode {
		wailsRuntime.EventsEmit(a.ctx, "log", message)
	}
}

// emitDownloadProgress emits download progress and forwards to task queue if active
func (a *App) emitDownloadProgress(progress DownloadProgress) {
	// Always emit the download-progress event for any listeners
	wailsRuntime.EventsEmit(a.ctx, "download-progress", progress)

	// If we're running in task queue context, also forward to task progress
	if a.currentTaskID != "" && a.taskProgressChan != nil {
		taskProgress := taskqueue.TaskProgress{
			CurrentPhase:   progress.Status,
			TotalDates:     progress.TotalDates,
			CurrentDate:    progress.CurrentDate,
			TilesTotal:     progress.Total,
			TilesCompleted: progress.Downloaded,
			Percent:        progress.Percent,
		}
		// Non-blocking send
		select {
		case a.taskProgressChan <- taskProgress:
		default:
		}
	}
}

// findLayerForDate finds the layer matching a date
func (a *App) findLayerForDate(date string) (*esri.Layer, error) {
	layers, err := a.esriClient.GetLayers()
	if err != nil {
		return nil, err
	}

	for _, layer := range layers {
		if layer.Date.Format("2006-01-02") == date {
			return layer, nil
		}
	}

	return nil, fmt.Errorf("no layer found for date: %s", date)
}

// isBlankTile checks if a tile is blank/uniform (white, black, or single color)
// This happens when imagery isn't available at the requested zoom level for older dates
func isBlankTile(data []byte) bool {
	if len(data) < 100 {
		return true // Too small to be a real image
	}

	// Decode image to check pixel uniformity
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false // Can't decode, assume it's valid
	}

	bounds := img.Bounds()
	if bounds.Dx() < 10 || bounds.Dy() < 10 {
		return true // Too small
	}

	// Sample pixels at various positions
	samplePoints := []image.Point{
		{bounds.Min.X + bounds.Dx()/4, bounds.Min.Y + bounds.Dy()/4},
		{bounds.Min.X + bounds.Dx()/2, bounds.Min.Y + bounds.Dy()/2},
		{bounds.Min.X + 3*bounds.Dx()/4, bounds.Min.Y + 3*bounds.Dy()/4},
		{bounds.Min.X + bounds.Dx()/4, bounds.Min.Y + 3*bounds.Dy()/4},
		{bounds.Min.X + 3*bounds.Dx()/4, bounds.Min.Y + bounds.Dy()/4},
	}

	// Get first sample as reference
	refR, refG, refB, _ := img.At(samplePoints[0].X, samplePoints[0].Y).RGBA()

	// Check if all samples are nearly identical (within tolerance)
	tolerance := uint32(500) // Very small tolerance for "same" color
	allSame := true
	for _, pt := range samplePoints[1:] {
		r, g, b, _ := img.At(pt.X, pt.Y).RGBA()
		if absDiff(r, refR) > tolerance || absDiff(g, refG) > tolerance || absDiff(b, refB) > tolerance {
			allSame = false
			break
		}
	}

	if allSame {
		// Check if it's a known blank color (white or near-white)
		// RGBA values are in 0-65535 range
		if refR > 60000 && refG > 60000 && refB > 60000 {
			return true // White/blank
		}
		if refR < 5000 && refG < 5000 && refB < 5000 {
			return true // Black/blank
		}
	}

	return false
}

// absDiff returns absolute difference between two uint32 values
func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// tileResult holds the result of a tile download
type tileResult struct {
	tile *esri.EsriTile
	data []byte
	err  error
}

// DownloadEsriImagery downloads Esri Wayback imagery for a bounding box as georeferenced image
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadEsriImagery(bbox BoundingBox, zoom int, date string, format string) error {
	a.emitLog(fmt.Sprintf("Starting download for %s at zoom %d", date, zoom))

	// Find layer for this date directly (much faster than GetNearestDatedTile)
	layer, err := a.findLayerForDate(date)
	if err != nil {
		a.emitLog(fmt.Sprintf("Error: %v", err))
		return err
	}
	a.emitLog(fmt.Sprintf("Found layer ID %d for date %s", layer.ID, date))

	// Get tiles
	tiles, err := esri.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	if err != nil {
		return err
	}

	total := len(tiles)
	if total == 0 {
		return fmt.Errorf("no tiles in bounding box")
	}
	a.emitLog(fmt.Sprintf("Downloading %d tiles with %d workers...", total, DownloadWorkers))

	// Download tiles concurrently
	var downloaded int64
	tileChan := make(chan *esri.EsriTile, total)
	resultChan := make(chan tileResult, total)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < DownloadWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tile := range tileChan {
				data, err := a.esriClient.FetchTile(layer, tile)
				resultChan <- tileResult{tile: tile, data: data, err: err}
			}
		}()
	}

	// Send tiles to workers
	go func() {
		for _, tile := range tiles {
			tileChan <- tile
		}
		close(tileChan)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Find tile bounds for stitching
	var minCol, maxCol, minRow, maxRow int
	first := true
	for _, tile := range tiles {
		if first {
			minCol, maxCol = tile.Column, tile.Column
			minRow, maxRow = tile.Row, tile.Row
			first = false
		} else {
			if tile.Column < minCol {
				minCol = tile.Column
			}
			if tile.Column > maxCol {
				maxCol = tile.Column
			}
			if tile.Row < minRow {
				minRow = tile.Row
			}
			if tile.Row > maxRow {
				maxRow = tile.Row
			}
		}
	}

	cols := maxCol - minCol + 1
	rows := maxRow - minRow + 1
	a.emitLog(fmt.Sprintf("Grid: %d cols x %d rows", cols, rows))

	// Create output image only if we need GeoTIFF
	var outputImg *image.RGBA
	var outputWidth, outputHeight int
	if format == "geotiff" || format == "both" {
		outputWidth = cols * TileSize
		outputHeight = rows * TileSize
		outputImg = image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	}

	// Create tiles directory if saving individual tiles (OGC structure: source_date_z{zoom}_tiles/{z}/{x}/{y}.jpg)
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(a.downloadPath, generateTilesDirName("esri", date, zoom))
		if err := os.MkdirAll(tilesDir, 0755); err != nil {
			return fmt.Errorf("failed to create tiles directory: %w", err)
		}
	}

	// Process results and stitch tiles
	successCount := 0
	for result := range resultChan {
		count := atomic.AddInt64(&downloaded, 1)

		// Emit progress with clear status based on format
		percent := int((count * 100) / int64(total))
		var status string

		// If in range download mode, include date context
		if a.inRangeDownload {
			dateProgress := fmt.Sprintf("Date %d/%d", a.currentDateIndex, a.totalDatesInRange)
			if format == "geotiff" || format == "both" {
				status = fmt.Sprintf("%s: Downloading tile %d/%d", dateProgress, count, total)
			} else {
				status = fmt.Sprintf("%s: Downloading tile %d/%d", dateProgress, count, total)
			}
		} else {
			if format == "geotiff" || format == "both" {
				status = fmt.Sprintf("Downloading and merging %d/%d tiles", count, total)
			} else {
				status = fmt.Sprintf("Downloading %d/%d tiles", count, total)
			}
		}

		a.emitDownloadProgress(DownloadProgress{
			Downloaded:  int(count),
			Total:       total,
			Percent:     percent,
			Status:      status,
			CurrentDate: a.currentDateIndex,
			TotalDates:  a.totalDatesInRange,
		})

		if result.err != nil {
			// Only log unique errors to avoid spam
			// a.TrackEvent("tile_download_error", map[string]interface{}{"source": "esri", "error": result.err.Error()})
			continue
		}

		// Save individual tile if requested (OGC structure: source/date/z/x/y.jpg)
		if format == "tiles" || format == "both" {
			// Create esri/date/z/x subdirectories
			sourceDir := filepath.Join(tilesDir, "esri", date)
			zDir := filepath.Join(sourceDir, fmt.Sprintf("%d", zoom))
			xDir := filepath.Join(zDir, fmt.Sprintf("%d", result.tile.Column))
			if err := os.MkdirAll(xDir, 0755); err != nil {
				log.Printf("Failed to create tile directories: %v", err)
			} else {
				tilePath := filepath.Join(xDir, fmt.Sprintf("%d.jpg", result.tile.Row))
				if err := os.WriteFile(tilePath, result.data, 0644); err != nil {
					log.Printf("Failed to save tile: %v", err)
				}
			}
		}

		// Decode and stitch for GeoTIFF
		if format == "geotiff" || format == "both" {
			img, err := jpeg.Decode(bytes.NewReader(result.data))
			if err != nil {
				continue
			}

			// Calculate position in output image
			xOff := (result.tile.Column - minCol) * TileSize
			yOff := (result.tile.Row - minRow) * TileSize

			// Draw tile onto output image
			draw.Draw(outputImg, image.Rect(xOff, yOff, xOff+TileSize, yOff+TileSize), img, image.Point{0, 0}, draw.Src)
		}
		successCount++
	}

	a.emitLog(fmt.Sprintf("Processed %d/%d tiles", successCount, total))

	// Track download completion
	a.TrackEvent("download_complete", map[string]interface{}{
		"source":  "esri",
		"zoom":    zoom,
		"total":   total,
		"success": successCount,
		"failed":  total - successCount,
		"format":  format,
	})

	// Save GeoTIFF if requested
	if format == "geotiff" || format == "both" {
		// Calculate georeferencing in Web Mercator (EPSG:3857)
		originX, originY := esri.TileToWebMercator(minCol, minRow, zoom)
		endX, endY := esri.TileToWebMercator(maxCol+1, maxRow+1, zoom)
		pixelWidth := (endX - originX) / float64(outputWidth)
		pixelHeight := (originY - endY) / float64(outputHeight)

		// Save as GeoTIFF with embedded projection and rich metadata
		tifPath := filepath.Join(a.downloadPath, generateGeoTIFFFilename("esri", date, bbox, zoom))

		// Emit progress for GeoTIFF encoding phase
		a.emitDownloadProgress(DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		a.emitLog("Encoding GeoTIFF file...")
		if err := a.saveAsGeoTIFFWithMetadata(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight, "Esri Wayback", date); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		a.emitLog(fmt.Sprintf("Saved: %s", tifPath))

		// Save PNG copy for video export compatibility
		a.savePNGCopy(outputImg, tifPath)
	}

	if format == "tiles" || format == "both" {
		a.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     "Complete",
	})

	// Auto-open download folder
	a.emitLog("Opening download folder...")
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
}

// saveAsGeoTIFF saves an image as a georeferenced TIFF with embedded tags (EPSG:3857)
// Includes proper geospatial metadata for GIS software compatibility
func (a *App) saveAsGeoTIFF(img image.Image, outputPath string, originX, originY, pixelWidth, pixelHeight float64) error {
	return a.saveAsGeoTIFFWithMetadata(img, outputPath, originX, originY, pixelWidth, pixelHeight, "", "")
}

// savePNGCopy saves a PNG copy of an image alongside its GeoTIFF for video export compatibility
// GeoTIFF files with custom geo tags may not decode properly with standard image decoders,
// so we create a PNG sidecar that video export can reliably use
func (a *App) savePNGCopy(img image.Image, tifPath string) {
	pngPath := strings.TrimSuffix(tifPath, ".tif") + ".png"
	pngFile, err := os.Create(pngPath)
	if err != nil {
		log.Printf("Failed to create PNG file: %v", err)
		return
	}
	defer pngFile.Close()

	if err := png.Encode(pngFile, img); err != nil {
		log.Printf("Failed to encode PNG: %v", err)
		return
	}
	a.emitLog(fmt.Sprintf("Saved PNG copy: %s", filepath.Base(pngPath)))
}

// saveAsGeoTIFFWithMetadata saves an image as a georeferenced TIFF with full metadata
func (a *App) saveAsGeoTIFFWithMetadata(img image.Image, outputPath string, originX, originY, pixelWidth, pixelHeight float64, source, date string) error {
	// Create TIFF file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// Define GeoKeys (EPSG:3857 Web Mercator)
	extraTags := make(map[uint16]interface{})

	// Tag 34735: GeoKeyDirectoryTag (SHORT)
	// Version=1, Revision=1, Minor=0, Keys=3
	// 1024 (GTModelType) = 1 (Projected CRS)
	// 1025 (GTRasterType) = 1 (PixelIsArea - pixel represents area, not point)
	// 3072 (ProjectedCSType) = 3857 (WGS 84 / Pseudo-Mercator - EPSG:3857)
	extraTags[geotiff.TagType_GeoKeyDirectoryTag] = []uint16{
		1, 1, 0, 3,
		1024, 0, 1, 1, // GTModelTypeGeoKey: Projected
		1025, 0, 1, 1, // GTRasterTypeGeoKey: PixelIsArea
		3072, 0, 1, 3857, // ProjectedCSTypeGeoKey: EPSG:3857
	}

	// Tag 33550: ModelPixelScaleTag (DOUBLE)
	// ScaleX, ScaleY, ScaleZ
	// Pixel dimensions in the model space (meters for EPSG:3857)
	// ScaleY is typically abs(pixelHeight) as it represents magnitude
	scaleY := pixelHeight
	if scaleY < 0 {
		scaleY = -scaleY
	}
	extraTags[geotiff.TagType_ModelPixelScaleTag] = []float64{pixelWidth, scaleY, 0.0}

	// Tag 33922: ModelTiepointTag (DOUBLE)
	// (I, J, K, X, Y, Z) - ties raster pixel (I,J,K) to model coordinates (X,Y,Z)
	// Map pixel (0,0,0) to model coordinate (originX, originY, 0)
	extraTags[geotiff.TagType_ModelTiepointTag] = []float64{0.0, 0.0, 0.0, originX, originY, 0.0}

	// Encode as GeoTIFF with metadata
	if err := geotiff.Encode(f, img, extraTags); err != nil {
		return fmt.Errorf("failed to encode GeoTIFF: %w", err)
	}

	// Also write a metadata sidecar file (.aux.xml) for complete metadata
	if source != "" && date != "" {
		auxPath := outputPath + ".aux.xml"
		auxContent := fmt.Sprintf(`<PAMDataset>
  <Metadata domain="IMAGE_STRUCTURE">
    <MDI key="COMPRESSION">NONE</MDI>
    <MDI key="INTERLEAVE">PIXEL</MDI>
  </Metadata>
  <Metadata domain="">
    <MDI key="Source">%s</MDI>
    <MDI key="Date">%s</MDI>
    <MDI key="CRS">EPSG:3857</MDI>
    <MDI key="Generated_By">WalkThru Earth Imagery Desktop v%s</MDI>
  </Metadata>
</PAMDataset>
`, source, date, AppVersion)
		if err := os.WriteFile(auxPath, []byte(auxContent), 0644); err != nil {
			log.Printf("Warning: Failed to write metadata sidecar file: %v", err)
		}
	}

	return nil
}

// DownloadGoogleEarthImagery downloads Google Earth imagery for a bounding box
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadGoogleEarthImagery(bbox BoundingBox, zoom int, format string) error {
	a.emitLog("Starting Google Earth download...")

	// Get tiles using Google Earth coordinate system
	tiles, err := googleearth.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	if err != nil {
		return err
	}

	total := len(tiles)
	if total == 0 {
		return fmt.Errorf("no tiles in bounding box")
	}
	a.emitLog(fmt.Sprintf("Downloading %d tiles...", total))

	// Find tile bounds for stitching
	var minCol, maxCol, minRow, maxRow int
	first := true
	for _, tile := range tiles {
		if first {
			minCol, maxCol = tile.Column, tile.Column
			minRow, maxRow = tile.Row, tile.Row
			first = false
		} else {
			if tile.Column < minCol {
				minCol = tile.Column
			}
			if tile.Column > maxCol {
				maxCol = tile.Column
			}
			if tile.Row < minRow {
				minRow = tile.Row
			}
			if tile.Row > maxRow {
				maxRow = tile.Row
			}
		}
	}

	cols := maxCol - minCol + 1
	rows := maxRow - minRow + 1
	a.emitLog(fmt.Sprintf("Grid: %d cols x %d rows", cols, rows))

	// Create output image only if we need GeoTIFF
	var outputImg *image.RGBA
	var outputWidth, outputHeight int
	if format == "geotiff" || format == "both" {
		outputWidth = cols * TileSize
		outputHeight = rows * TileSize
		outputImg = image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	}

	// Create tiles directory if saving individual tiles (OGC structure)
	timestamp := time.Now().Format("2006-01-02")
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(a.downloadPath, generateTilesDirName("ge", timestamp, zoom))
		if err := os.MkdirAll(tilesDir, 0755); err != nil {
			return fmt.Errorf("failed to create tiles directory: %w", err)
		}
	}

	// Download and stitch tiles
	successCount := 0
	for i, tile := range tiles {
		// Emit progress with clear status based on format
		var status string
		if format == "geotiff" || format == "both" {
			status = fmt.Sprintf("Downloading and merging tile %d/%d", i+1, total)
		} else {
			status = fmt.Sprintf("Downloading tile %d/%d", i+1, total)
		}
		a.emitDownloadProgress(DownloadProgress{
			Downloaded:  i,
			Total:       total,
			Percent:     (i * 100) / total,
			Status:      status,
			CurrentDate: a.currentDateIndex,
			TotalDates:  a.totalDatesInRange,
		})

		// Download tile
		data, err := a.geClient.FetchTile(tile)
		if err != nil {
			// Only log in dev mode
			a.emitLog(fmt.Sprintf("[GEDownload] Failed to download tile %s: %v", tile.Path, err))
			continue
		}

		// Save individual tile if requested (OGC structure: source/date/z/x/y.jpg)
		if format == "tiles" || format == "both" {
			// Create google_earth/current/z/x subdirectories
			sourceDir := filepath.Join(tilesDir, "google_earth", timestamp)
			zDir := filepath.Join(sourceDir, fmt.Sprintf("%d", zoom))
			xDir := filepath.Join(zDir, fmt.Sprintf("%d", tile.Column))
			if err := os.MkdirAll(xDir, 0755); err != nil {
				log.Printf("Failed to create tile directories: %v", err)
			} else {
				tilePath := filepath.Join(xDir, fmt.Sprintf("%d.jpg", tile.Row))
				if err := os.WriteFile(tilePath, data, 0644); err != nil {
					log.Printf("Failed to save tile: %v", err)
				}
			}
		}

		// Decode and stitch for GeoTIFF
		if format == "geotiff" || format == "both" {
			img, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				a.emitLog(fmt.Sprintf("[GEDownload] Failed to decode tile %s: %v", tile.Path, err))
				continue
			}

			// Calculate position in output image
			// GE rows increase from south to north, but image Y=0 is at top
			// So we need to invert: higher row numbers go to lower Y positions
			xOff := (tile.Column - minCol) * TileSize
			yOff := (maxRow - tile.Row) * TileSize

			// Draw tile onto output image
			draw.Draw(outputImg, image.Rect(xOff, yOff, xOff+TileSize, yOff+TileSize), img, image.Point{0, 0}, draw.Src)
		}
		successCount++
	}

	a.emitLog(fmt.Sprintf("Processed %d/%d tiles", successCount, total))

	// Check if we have enough tiles to create a meaningful GeoTIFF
	minSuccessRate := 0.3 // Require at least 30% of tiles to succeed
	if successCount == 0 {
		return fmt.Errorf("failed to download any tiles - all attempts failed")
	}
	if float64(successCount)/float64(total) < minSuccessRate {
		log.Printf("[GEDownload] Warning: Only %d/%d tiles (%.1f%%) downloaded successfully",
			successCount, total, float64(successCount)/float64(total)*100)
		a.emitLog(fmt.Sprintf("Warning: Only %d/%d tiles downloaded - GeoTIFF may have gaps", successCount, total))
	}

	// Track download completion
	a.TrackEvent("download_complete", map[string]interface{}{
		"source":  "google_earth",
		"zoom":    zoom,
		"total":   total,
		"success": successCount,
		"failed":  total - successCount,
		"format":  format,
	})

	// Save GeoTIFF if requested
	if format == "geotiff" || format == "both" {
		// Calculate georeferencing in Web Mercator (EPSG:3857)
		// After Y-inversion, image top-left corresponds to (minCol, maxRow+1) in GE coords
		// Image bottom-right corresponds to (maxCol+1, minRow)
		originX, originY := googleearth.TileToWebMercator(maxRow+1, minCol, zoom)
		endX, endY := googleearth.TileToWebMercator(minRow, maxCol+1, zoom)
		pixelWidth := (endX - originX) / float64(outputWidth)
		pixelHeight := (endY - originY) / float64(outputHeight) // Will be negative (Y decreases going down)

		// Save as GeoTIFF with embedded projection and rich metadata
		tifPath := filepath.Join(a.downloadPath, generateGeoTIFFFilename("ge", timestamp, bbox, zoom))

		// Emit progress for GeoTIFF encoding phase
		a.emitDownloadProgress(DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		a.emitLog("Encoding GeoTIFF file...")
		if err := a.saveAsGeoTIFFWithMetadata(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight, "Google Earth", timestamp); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		a.emitLog(fmt.Sprintf("Saved: %s", tifPath))

		// Save PNG copy for video export compatibility
		a.savePNGCopy(outputImg, tifPath)
	}

	if format == "tiles" || format == "both" {
		a.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     "Complete",
	})

	// Auto-open download folder
	a.emitLog("Opening download folder...")
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
}

// DownloadEsriImageryRange downloads Esri Wayback imagery for multiple dates (bulk download)
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
// This function deduplicates by checking the center tile - dates with identical imagery are skipped
func (a *App) DownloadEsriImageryRange(bbox BoundingBox, zoom int, dates []string, format string) error {
	if len(dates) == 0 {
		return fmt.Errorf("no dates provided")
	}

	a.emitLog(fmt.Sprintf("Starting bulk download for %d dates (with deduplication)", len(dates)))

	// Sort dates for consistent output
	sort.Strings(dates)

	// Get center tile for deduplication check
	centerLat := (bbox.South + bbox.North) / 2
	centerLon := (bbox.West + bbox.East) / 2
	centerTile, err := esri.GetTileForWgs84(centerLat, centerLon, zoom)
	if err != nil {
		return fmt.Errorf("failed to get center tile: %w", err)
	}

	// Track seen tile hashes to skip duplicates
	seenHashes := make(map[string]string) // hash -> first date that had this imagery
	downloadedCount := 0
	skippedCount := 0

	// Enable range download mode for unified progress
	a.inRangeDownload = true
	a.totalDatesInRange = len(dates)
	defer func() {
		a.inRangeDownload = false
	}()

	total := len(dates)
	for i, date := range dates {
		a.currentDateIndex = i + 1

		// Find layer for this date
		layer, err := a.findLayerForDate(date)
		if err != nil {
			a.emitLog(fmt.Sprintf("Skipping %s: %v", date, err))
			skippedCount++
			continue
		}

		// Fetch center tile to check for duplicates
		tileData, err := a.esriClient.FetchTile(layer, centerTile)
		if err != nil || len(tileData) == 0 {
			a.emitLog(fmt.Sprintf("Skipping %s: no tile data available", date))
			skippedCount++
			continue
		}

		// Compute simple hash of tile data (first 1KB + last 1KB + length)
		var hashKey string
		if len(tileData) < 2048 {
			hashKey = fmt.Sprintf("%x-%d", tileData, len(tileData))
		} else {
			hashKey = fmt.Sprintf("%x-%x-%d", tileData[:1024], tileData[len(tileData)-1024:], len(tileData))
		}

		// Check if we've seen this imagery before
		if firstDate, exists := seenHashes[hashKey]; exists {
			a.emitLog(fmt.Sprintf("Skipping %s: identical to %s", date, firstDate))
			skippedCount++
			continue
		}
		seenHashes[hashKey] = date

		// Download this unique date
		if err := a.DownloadEsriImagery(bbox, zoom, date, format); err != nil {
			a.emitLog(fmt.Sprintf("Failed to download %s: %v", date, err))
		} else {
			downloadedCount++
		}
	}

	// Emit completion
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     fmt.Sprintf("Downloaded %d unique dates (skipped %d duplicates)", downloadedCount, skippedCount),
	})

	a.emitLog(fmt.Sprintf("Bulk download complete: %d unique, %d skipped", downloadedCount, skippedCount))

	// Auto-open download folder
	a.emitLog("Opening download folder...")
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
}

// OpenDownloadFolder opens the download folder in the system file manager
func (a *App) OpenDownloadFolder() error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", a.downloadPath)
	case "windows":
		cmd = exec.Command("explorer", a.downloadPath)
	default: // Linux and others
		cmd = exec.Command("xdg-open", a.downloadPath)
	}
	return cmd.Start()
}

// OpenFolder opens a specific folder in the OS file explorer
func (a *App) OpenFolder(path string) error {
	// Verify the path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("folder does not exist: %s", path)
	}

	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default: // Linux and others
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// Greet returns a greeting for the given name (kept for template compatibility)
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// GetEsriTileURL returns the tile URL template for a given date (for map preview)
func (a *App) GetEsriTileURL(date string) (string, error) {
	layers, err := a.esriClient.GetLayers()
	if err != nil {
		return "", err
	}

	// Find layer matching the date
	for _, layer := range layers {
		if layer.Date.Format("2006-01-02") == date {
			// Return a simplified tile URL template for MapLibre
			return fmt.Sprintf("https://wayback.maptiles.arcgis.com/arcgis/rest/services/world_imagery/mapserver/tile/%d/{z}/{y}/{x}", layer.ID), nil
		}
	}

	return "", fmt.Errorf("no layer found for date: %s", date)
}

// corsMiddleware adds CORS headers to allow requests from Wails frontend
// On macOS/Linux, Wails uses wails://wails origin which requires CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins (needed for wails://wails on macOS/Linux)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// StartTileServer starts a local HTTP server to serve decrypted Google Earth tiles
func (a *App) StartTileServer() {
	// Create a new mux to avoid global state conflicts
	mux := http.NewServeMux()
	mux.HandleFunc("/ge/", a.handleGoogleEarthTile)
	mux.HandleFunc("/ge-historical/", a.handleGoogleEarthHistoricalTile)

	// Listen on a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		wailsRuntime.LogError(a.ctx, fmt.Sprintf("Failed to start tile server: %v", err))
		return
	}

	port := listener.Addr().(*net.TCPAddr).Port
	a.tileServerURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	wailsRuntime.LogInfo(a.ctx, fmt.Sprintf("Tile server started on %s", a.tileServerURL))

	// Wrap mux with CORS middleware
	server := &http.Server{
		Handler: corsMiddleware(mux),
	}

	if err := server.Serve(listener); err != nil {
		wailsRuntime.LogError(a.ctx, fmt.Sprintf("Tile server stopped: %v", err))
	}
}

// handleGoogleEarthTile handles requests for Google Earth tiles
// URL format: /ge/{z}/{x}/{y}
// This handler reprojects GE tiles (Plate Carrée) to Web Mercator for MapLibre
func (a *App) handleGoogleEarthTile(w http.ResponseWriter, r *http.Request) {
	// Parse path components
	// Expected: /ge/z/x/y
	path := r.URL.Path
	if len(path) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	parts := strings.Split(path[4:], "/") // Remove /ge/ prefix
	if len(parts) < 3 {
		http.Error(w, "Invalid tile coordinates", http.StatusBadRequest)
		return
	}

	var z, x, y int
	if _, err := fmt.Sscanf(parts[0], "%d", &z); err != nil {
		http.Error(w, "Invalid zoom", http.StatusBadRequest)
		return
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &x); err != nil {
		http.Error(w, "Invalid x", http.StatusBadRequest)
		return
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &y); err != nil {
		http.Error(w, "Invalid y", http.StatusBadRequest)
		return
	}

	// Get all GE tiles needed to cover this Web Mercator tile
	// Try at the requested zoom level first, then fall back to lower zooms if tiles aren't available
	geTiles := make(map[string]image.Image)
	sourceZoom := z

	// Get geographic bounds of the requested Web Mercator tile (fixed for all attempts)
	south, west, north, east := googleearth.WebMercatorTileBounds(x, y, z)

	// Try to fetch tiles, with fallback to lower zoom levels
	for tryZoom := z; tryZoom >= 10 && len(geTiles) == 0; tryZoom-- {
		// Find GE tiles at tryZoom that cover the same geographic area
		requiredTiles := googleearth.GetGETilesForBounds(south, west, north, east, tryZoom)
		if len(requiredTiles) == 0 {
			continue
		}

		for _, tc := range requiredTiles {
			tile, err := googleearth.NewTileFromRowCol(tc.Row, tc.Column, tc.Level)
			if err != nil {
				continue
			}

			// Try cache first
			cacheKey := fmt.Sprintf("ge:%s:current", tile.Path)
			var data []byte

			if a.tileCache != nil {
				if cachedData, found := a.tileCache.Get(cacheKey); found {
					data = cachedData
				}
			}

			// Fetch from source if not cached
			if data == nil {
				data, err = a.geClient.FetchTile(tile)
				if err != nil {
					continue
				}

				// Cache the result
				if a.tileCache != nil {
					a.tileCache.Set(cacheKey, data)
				}
			}

			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				continue
			}

			key := fmt.Sprintf("%d,%d", tc.Row, tc.Column)
			geTiles[key] = img
		}

		if len(geTiles) > 0 {
			sourceZoom = tryZoom
			if tryZoom < z {
				log.Printf("[GETile] z=%d x=%d y=%d: fell back to zoom %d", z, x, y, tryZoom)
			}
		}
	}

	if len(geTiles) == 0 {
		log.Printf("[GETile] z=%d x=%d y=%d: no tiles available at any zoom level", z, x, y)
		http.Error(w, "No tiles available", http.StatusNotFound)
		return
	}

	// Reproject to Web Mercator (using source zoom for tile lookups)
	output := googleearth.ReprojectToWebMercatorWithSourceZoom(geTiles, x, y, z, sourceZoom, TileSize)

	// Encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, output, &jpeg.Options{Quality: 90}); err != nil {
		http.Error(w, "Failed to encode tile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(buf.Bytes())
}

// fetchHistoricalGETileWithZoomFallback attempts to fetch a historical tile with automatic zoom fallback
// If the tile doesn't exist at the requested zoom, it tries lower zoom levels (z-1, z-2, etc.)
// When using a lower zoom tile, it extracts and upscales the correct portion to match the original tile
// Returns the tile data and the zoom level that succeeded, or error if all attempts fail
func (a *App) fetchHistoricalGETileWithZoomFallback(tile *googleearth.Tile, hexDate string, maxFallbackLevels int) ([]byte, int, error) {
	// Try the requested zoom first
	data, err := a.fetchHistoricalGETile(tile, hexDate)
	if err == nil {
		return data, tile.Level, nil
	}

	// Log the initial failure
	log.Printf("[ZoomFallback] Tile %s at zoom %d failed, trying fallback...", tile.Path, tile.Level)

	originalRow := tile.Row
	originalCol := tile.Column
	originalZoom := tile.Level

	// Try lower zoom levels
	for fallbackLevel := 1; fallbackLevel <= maxFallbackLevels; fallbackLevel++ {
		lowerZoom := tile.Level - fallbackLevel
		if lowerZoom < 10 {
			break // Don't go below zoom 10
		}

		// Create a tile at the lower zoom level covering the same geographic area
		// Get the center of the original tile
		lat, lon := tile.Center()
		lowerTile, err := googleearth.GetTileForCoord(lat, lon, lowerZoom)
		if err != nil {
			continue
		}

		log.Printf("[ZoomFallback] Trying zoom %d (tile: %s)...", lowerZoom, lowerTile.Path)
		data, err := a.fetchHistoricalGETile(lowerTile, hexDate)
		if err == nil {
			log.Printf("[ZoomFallback] SUCCESS at zoom %d, extracting quadrant for original tile", lowerZoom)

			// Extract and upscale the correct portion of the lower zoom tile
			// to match the original requested tile
			croppedData, err := a.extractQuadrantFromFallbackTile(data, originalRow, originalCol, originalZoom, lowerTile.Row, lowerTile.Column, lowerZoom)
			if err != nil {
				log.Printf("[ZoomFallback] Failed to extract quadrant: %v, returning full tile", err)
				return data, lowerZoom, nil
			}

			return croppedData, originalZoom, nil // Return originalZoom since we've upscaled to match
		}
	}

	return nil, 0, fmt.Errorf("tile not available at zoom %d or any fallback levels", tile.Level)
}

// extractQuadrantFromFallbackTile extracts and upscales the portion of a lower-zoom tile
// that corresponds to a higher-zoom tile position
func (a *App) extractQuadrantFromFallbackTile(data []byte, origRow, origCol, origZoom, fallbackRow, fallbackCol, fallbackZoom int) ([]byte, error) {
	// Decode the source image
	srcImg, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		// Try other formats
		srcImg, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode fallback tile: %w", err)
		}
	}

	srcBounds := srcImg.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	// Calculate the scale factor (how many higher-zoom tiles fit in one lower-zoom tile)
	zoomDiff := origZoom - fallbackZoom
	scale := 1 << zoomDiff // 2^zoomDiff (e.g., 2 for 1 level diff, 4 for 2 levels diff)

	// Calculate the position of the original tile within the fallback tile
	// The original tile's position relative to the fallback tile
	relRow := origRow - (fallbackRow * scale)
	relCol := origCol - (fallbackCol * scale)

	// Calculate the source rectangle to extract
	quadrantWidth := srcWidth / scale
	quadrantHeight := srcHeight / scale
	srcX := relCol * quadrantWidth
	srcY := relRow * quadrantHeight

	log.Printf("[ZoomFallback] Extracting quadrant: zoomDiff=%d, scale=%d, rel(%d,%d), src(%d,%d), size(%d,%d)",
		zoomDiff, scale, relCol, relRow, srcX, srcY, quadrantWidth, quadrantHeight)

	// Create output image (256x256 like a normal tile)
	tileSize := 256
	dstImg := image.NewRGBA(image.Rect(0, 0, tileSize, tileSize))

	// Scale factor for upsampling
	scaleX := float64(tileSize) / float64(quadrantWidth)
	scaleY := float64(tileSize) / float64(quadrantHeight)

	// Nearest-neighbor upscaling (fast and works well for satellite imagery)
	for dstY := 0; dstY < tileSize; dstY++ {
		for dstX := 0; dstX < tileSize; dstX++ {
			// Map destination coordinates to source coordinates
			srcPosX := srcX + int(float64(dstX)/scaleX)
			srcPosY := srcY + int(float64(dstY)/scaleY)

			// Clamp to valid range
			if srcPosX >= srcBounds.Max.X {
				srcPosX = srcBounds.Max.X - 1
			}
			if srcPosY >= srcBounds.Max.Y {
				srcPosY = srcBounds.Max.Y - 1
			}
			if srcPosX < srcBounds.Min.X {
				srcPosX = srcBounds.Min.X
			}
			if srcPosY < srcBounds.Min.Y {
				srcPosY = srcBounds.Min.Y
			}

			dstImg.Set(dstX, dstY, srcImg.At(srcPosX, srcPosY))
		}
	}

	// Encode back to JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dstImg, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("failed to encode extracted quadrant: %w", err)
	}

	return buf.Bytes(), nil
}

// fetchHistoricalGETile fetches a historical tile for the given GE tile coordinates and hexDate
// It handles epoch lookup and fallback to nearest date
func (a *App) fetchHistoricalGETile(tile *googleearth.Tile, hexDate string) ([]byte, error) {
	// Get available dates for this specific tile to find the correct epoch
	dates, err := a.geClient.GetAvailableDates(tile)
	if err != nil {
		return nil, fmt.Errorf("GetAvailableDates failed: %w", err)
	}

	if len(dates) == 0 {
		return nil, fmt.Errorf("no dates available for tile")
	}

	// DEBUG: Log all available dates and their epochs for this tile
	log.Printf("[DEBUG fetchHistoricalGETile] Tile %s has %d dates:", tile.Path, len(dates))
	for i, dt := range dates {
		if i < 5 { // Log first 5
			log.Printf("[DEBUG fetchHistoricalGETile]   Date %s (hex: %s) -> epoch %d",
				dt.Date.Format("2006-01-02"), dt.HexDate, dt.Epoch)
		}
	}
	log.Printf("[DEBUG fetchHistoricalGETile] Looking for hexDate: %s", hexDate)

	// Find the epoch for the requested hexDate
	var epoch int
	var foundHexDate string
	found := false
	for _, dt := range dates {
		if dt.HexDate == hexDate {
			epoch = dt.Epoch
			foundHexDate = hexDate
			found = true
			log.Printf("[DEBUG fetchHistoricalGETile] EXACT MATCH found: hexDate %s -> epoch %d", hexDate, epoch)
			break
		}
	}

	// If exact date not found, find the nearest date
	if !found {
		closestIdx := 0
		closestDiff := int64(^uint64(0) >> 1) // Max int64
		requestedVal, _ := strconv.ParseInt(hexDate, 16, 64)

		for i, dt := range dates {
			dtVal, _ := strconv.ParseInt(dt.HexDate, 16, 64)
			diff := requestedVal - dtVal
			if diff < 0 {
				diff = -diff
			}
			if diff < closestDiff {
				closestDiff = diff
				closestIdx = i
			}
		}

		epoch = dates[closestIdx].Epoch
		foundHexDate = dates[closestIdx].HexDate
		log.Printf("[DEBUG fetchHistoricalGETile] NO EXACT MATCH - using nearest: hexDate %s -> epoch %d (requested was %s)",
			foundHexDate, epoch, hexDate)
	}

	// Try fetching with the protobuf-reported epoch first
	log.Printf("[DEBUG fetchHistoricalGETile] Attempting fetch: tile %s, epoch %d, hexDate %s", tile.Path, epoch, foundHexDate)
	data, err := a.geClient.FetchHistoricalTile(tile, epoch, foundHexDate)
	if err == nil {
		log.Printf("[DEBUG fetchHistoricalGETile] SUCCESS on first attempt with epoch %d", epoch)
		return data, nil
	}

	// If the primary epoch fails (404), try with older epochs from the same tile
	// This mimics Google Earth Pro's behavior which uses older, stable epochs
	log.Printf("[DEBUG fetchHistoricalGETile] Primary epoch %d failed, trying fallback epochs...", epoch)

	// Collect unique epochs from all dates, sorted by frequency (most common first)
	epochCounts := make(map[int]int)
	for _, dt := range dates {
		epochCounts[dt.Epoch]++
	}

	// Sort epochs by frequency (descending)
	type epochFreq struct {
		epoch int
		count int
	}
	var epochList []epochFreq
	for ep, cnt := range epochCounts {
		if ep != epoch { // Skip the one we already tried
			epochList = append(epochList, epochFreq{ep, cnt})
		}
	}
	sort.Slice(epochList, func(i, j int) bool {
		return epochList[i].count > epochList[j].count
	})

	// Try epochs in order of frequency (most common = most likely to have tiles)
	for _, ef := range epochList {
		log.Printf("[DEBUG fetchHistoricalGETile] Trying fallback epoch %d (used by %d dates)...", ef.epoch, ef.count)
		data, err := a.geClient.FetchHistoricalTile(tile, ef.epoch, foundHexDate)
		if err == nil {
			log.Printf("[DEBUG fetchHistoricalGETile] SUCCESS with fallback epoch %d", ef.epoch)
			return data, nil
		}
		log.Printf("[DEBUG fetchHistoricalGETile] Fallback epoch %d also failed", ef.epoch)
	}

	// Last resort: Try known-good epochs for 2025+ dates
	// These epochs may not be in the protobuf but are known to work from testing
	knownGoodEpochs := []int{358, 357, 356, 354, 352}
	log.Printf("[DEBUG fetchHistoricalGETile] Trying known-good epochs for recent dates: %v", knownGoodEpochs)
	for _, knownEpoch := range knownGoodEpochs {
		// Skip if already tried
		if knownEpoch == epoch {
			continue
		}
		alreadyTried := false
		for _, ef := range epochList {
			if ef.epoch == knownEpoch {
				alreadyTried = true
				break
			}
		}
		if alreadyTried {
			continue
		}

		log.Printf("[DEBUG fetchHistoricalGETile] Trying known-good epoch %d...", knownEpoch)
		data, err := a.geClient.FetchHistoricalTile(tile, knownEpoch, foundHexDate)
		if err == nil {
			log.Printf("[DEBUG fetchHistoricalGETile] SUCCESS with known-good epoch %d", knownEpoch)
			return data, nil
		}
	}

	// All epochs failed
	return nil, fmt.Errorf("tile not available with any known epoch (tried %d epochs)", len(epochList)+1+len(knownGoodEpochs))
}

// handleGoogleEarthHistoricalTile handles requests for historical Google Earth tiles
// URL format: /ge-historical/{hexDate}/{z}/{x}/{y}
// This handler reprojects GE tiles (Plate Carrée) to Web Mercator for MapLibre
func (a *App) handleGoogleEarthHistoricalTile(w http.ResponseWriter, r *http.Request) {
	// Parse path components
	// Expected: /ge-historical/hexDate/z/x/y
	path := r.URL.Path
	prefix := "/ge-historical/"
	if !strings.HasPrefix(path, prefix) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	parts := strings.Split(path[len(prefix):], "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path format, expected /ge-historical/{hexDate}/{z}/{x}/{y}", http.StatusBadRequest)
		return
	}

	hexDate := parts[0]
	var z, x, y int

	if _, err := fmt.Sscanf(parts[1], "%d", &z); err != nil {
		http.Error(w, "Invalid zoom", http.StatusBadRequest)
		return
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &x); err != nil {
		http.Error(w, "Invalid x", http.StatusBadRequest)
		return
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &y); err != nil {
		http.Error(w, "Invalid y", http.StatusBadRequest)
		return
	}

	// Try to fetch historical tiles with smart zoom fallback
	// Strategy: Try harder at requested zoom before falling back (epoch fallback happens per tile)
	geTiles := make(map[string]image.Image)
	sourceZoom := z

	// Get geographic bounds of the requested Web Mercator tile (fixed for all attempts)
	south, west, north, east := googleearth.WebMercatorTileBounds(x, y, z)

	// Smart fallback: only try z, z-1, z-2, z-3 (instead of all the way to 10)
	// High zoom tiles (17-19) usually exist with the right epoch (358 for 2025+)
	// fetchHistoricalGETile already has three-layer epoch fallback, so give it a chance
	maxFallback := 3
	if z <= 16 {
		maxFallback = 6 // More aggressive fallback for lower zooms where coverage is sparser
	}

	for tryZoom := z; tryZoom >= max(z-maxFallback, 10) && len(geTiles) == 0; tryZoom-- {
		// Find GE tiles at tryZoom that cover the same geographic area
		requiredTiles := googleearth.GetGETilesForBounds(south, west, north, east, tryZoom)

		log.Printf("[GEHistorical] z=%d x=%d y=%d: trying zoom %d, need %d tiles", z, x, y, tryZoom, len(requiredTiles))

		successCount := 0
		for _, tc := range requiredTiles {
			tile, err := googleearth.NewTileFromRowCol(tc.Row, tc.Column, tc.Level)
			if err != nil {
				log.Printf("[GEHistorical] Failed to create tile from row=%d col=%d level=%d: %v", tc.Row, tc.Column, tc.Level, err)
				continue
			}

			// Try cache first
			cacheKey := fmt.Sprintf("ge:%s:%s", tile.Path, hexDate)
			var data []byte

			if a.tileCache != nil {
				if cachedData, found := a.tileCache.Get(cacheKey); found {
					data = cachedData
					successCount++
				}
			}

			// Fetch from source if not cached (with full epoch fallback)
			if data == nil {
				data, err = a.fetchHistoricalGETile(tile, hexDate)
				if err != nil {
					log.Printf("[GEHistorical] Tile %s at zoom %d failed: %v", tile.Path, tryZoom, err)
					continue
				}

				// Cache the successful result
				if a.tileCache != nil {
					a.tileCache.Set(cacheKey, data)
				}
				successCount++
			}

			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				log.Printf("[GEHistorical] Failed to decode tile %s: %v", tile.Path, err)
				continue
			}

			key := fmt.Sprintf("%d,%d", tc.Row, tc.Column)
			geTiles[key] = img
		}

		log.Printf("[GEHistorical] z=%d x=%d y=%d: zoom %d got %d/%d tiles", z, x, y, tryZoom, len(geTiles), len(requiredTiles))

		if len(geTiles) > 0 {
			sourceZoom = tryZoom
			if tryZoom < z {
				log.Printf("[GEHistorical] z=%d x=%d y=%d hexDate=%s: fell back to zoom %d (got %d/%d tiles)",
					z, x, y, hexDate, tryZoom, len(geTiles), len(requiredTiles))
			}
			// Early exit - we got tiles, stop trying lower zooms
			break
		}
	}

	if len(geTiles) == 0 {
		a.serveTransparentTile(w)
		return
	}

	// Reproject to Web Mercator (using source zoom for tile lookups)
	output := googleearth.ReprojectToWebMercatorWithSourceZoom(geTiles, x, y, z, sourceZoom, TileSize)

	// Encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, output, &jpeg.Options{Quality: 90}); err != nil {
		http.Error(w, "Failed to encode tile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=86400") // Cache for 24 hours
	w.Write(buf.Bytes())
}

// serveTransparentTile serves a 256x256 transparent PNG tile for missing data
func (a *App) serveTransparentTile(w http.ResponseWriter) {
	// 1x1 transparent PNG, scaled by MapLibre to 256x256
	// This is a minimal valid PNG with transparency
	transparentPNG := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00,
		0x01, 0x03, 0x00, 0x00, 0x00, 0x66, 0xbc, 0x3a, 0x25, 0x00, 0x00, 0x00,
		0x03, 0x50, 0x4c, 0x54, 0x45, 0x00, 0x00, 0x00, 0xa7, 0x7a, 0x3d, 0xda,
		0x00, 0x00, 0x00, 0x01, 0x74, 0x52, 0x4e, 0x53, 0x00, 0x40, 0xe6, 0xd8,
		0x66, 0x00, 0x00, 0x00, 0x1f, 0x49, 0x44, 0x41, 0x54, 0x68, 0xde, 0xed,
		0xc1, 0x01, 0x0d, 0x00, 0x00, 0x00, 0xc2, 0xa0, 0xf7, 0x4f, 0x6d, 0x0e,
		0x37, 0xa0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xbe, 0x0d,
		0x21, 0x00, 0x00, 0x01, 0x9a, 0x60, 0xe1, 0xd5, 0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=3600") // Cache for 1 hour
	w.Write(transparentPNG)
}

// GetGoogleEarthTileURL returns the tile URL template for Google Earth (for map preview)
func (a *App) GetGoogleEarthTileURL() (string, error) {
	if a.tileServerURL == "" {
		return "", fmt.Errorf("tile server not started")
	}
	return fmt.Sprintf("%s/ge/{z}/{x}/{y}", a.tileServerURL), nil
}

// GEAvailableDate represents an available Google Earth historical imagery date
type GEAvailableDate struct {
	Date    string `json:"date"`
	Epoch   int    `json:"epoch"`
	HexDate string `json:"hexDate"`
}

// GetGoogleEarthDatesForArea returns available historical imagery dates for a specific area
// This samples multiple tiles across the viewport to ensure returned dates are available
// at the current zoom level and location - critical for zoom levels 17-19 where date
// availability varies significantly between tiles
func (a *App) GetGoogleEarthDatesForArea(bbox BoundingBox, zoom int) ([]GEAvailableDate, error) {
	a.emitLog(fmt.Sprintf("Fetching Google Earth historical dates for zoom %d...", zoom))

	// IMPORTANT: Sample at zoom 16 to get stable, reliable epoch values
	// At zoom 17-19, the protobuf reports newer epochs (like 359) that don't have actual tiles
	// Zoom 16 provides epochs (like 358) that work across ALL zoom levels including 17-19
	// This is critical for 2025+ dates where high zoom epochs in protobuf are incorrect
	sampleZoom := 16
	if zoom < 16 {
		sampleZoom = zoom // Use requested zoom if it's lower than 16
	}
	log.Printf("[GEDates] Sampling at zoom %d for epoch stability (requested zoom: %d)", sampleZoom, zoom)

	// Sample multiple tiles across the viewport for better date coverage
	// At high zoom levels (17-19), different tiles have different available dates
	samplePoints := []struct{ lat, lon float64 }{
		{(bbox.South + bbox.North) / 2, (bbox.West + bbox.East) / 2},                        // Center
		{bbox.North - (bbox.North-bbox.South)*0.25, bbox.West + (bbox.East-bbox.West)*0.25}, // NW quadrant
		{bbox.North - (bbox.North-bbox.South)*0.25, bbox.East - (bbox.East-bbox.West)*0.25}, // NE quadrant
		{bbox.South + (bbox.North-bbox.South)*0.25, bbox.West + (bbox.East-bbox.West)*0.25}, // SW quadrant
		{bbox.South + (bbox.North-bbox.South)*0.25, bbox.East - (bbox.East-bbox.West)*0.25}, // SE quadrant
	}

	// Collect dates from all sample tiles
	allDatesMap := make(map[string]map[string]GEAvailableDate) // hexDate -> tileID -> date info
	tileSampleCount := 0

	for i, point := range samplePoints {
		tile, err := googleearth.GetTileForCoord(point.lat, point.lon, sampleZoom)
		if err != nil {
			log.Printf("[GEDates] Failed to get tile %d: %v", i, err)
			continue
		}

		log.Printf("[GEDates] Sampling tile %d/%d: %s at zoom %d", i+1, len(samplePoints), tile.Path, sampleZoom)

		datedTiles, err := a.geClient.GetAvailableDates(tile)
		if err != nil {
			log.Printf("[GEDates] Failed to get dates for tile %s: %v", tile.Path, err)
			continue
		}

		tileSampleCount++
		tileID := tile.Path

		// Add this tile's dates to the map
		for _, dt := range datedTiles {
			if allDatesMap[dt.HexDate] == nil {
				allDatesMap[dt.HexDate] = make(map[string]GEAvailableDate)
			}
			allDatesMap[dt.HexDate][tileID] = GEAvailableDate{
				Date:    dt.Date.Format("2006-01-02"),
				Epoch:   dt.Epoch,
				HexDate: dt.HexDate,
			}
		}
	}

	if tileSampleCount == 0 {
		return nil, fmt.Errorf("failed to sample any tiles in the area")
	}

	// Filter to dates that appear in at least 60% of sampled tiles
	// This ensures good coverage while allowing for some tile variation
	minTileCount := int(float64(tileSampleCount) * 0.6)
	if minTileCount < 1 {
		minTileCount = 1
	}

	var dates []GEAvailableDate
	seen := make(map[string]bool)

	for hexDate, tilesWithDate := range allDatesMap {
		if len(tilesWithDate) >= minTileCount {
			// Find the most common epoch for this date across all tiles
			// Different tiles may report different epochs for the same date
			epochCounts := make(map[int]int)
			var sampleDateInfo GEAvailableDate

			for _, dateInfo := range tilesWithDate {
				epochCounts[dateInfo.Epoch]++
				sampleDateInfo = dateInfo // Keep one for the date string
			}

			// Use the most frequently occurring epoch
			bestEpoch := sampleDateInfo.Epoch
			maxCount := 0
			for epoch, count := range epochCounts {
				if count > maxCount {
					maxCount = count
					bestEpoch = epoch
				}
			}

			if !seen[sampleDateInfo.Date] {
				seen[sampleDateInfo.Date] = true
				dates = append(dates, GEAvailableDate{
					Date:    sampleDateInfo.Date,
					Epoch:   bestEpoch, // Use most common epoch
					HexDate: hexDate,
				})
				log.Printf("[GEDates] Date %s (hex: %s, epoch: %d) available in %d/%d tiles (epoch used by %d tiles)",
					sampleDateInfo.Date, hexDate, bestEpoch, len(tilesWithDate), tileSampleCount, maxCount)
			}
		}
	}

	if len(dates) == 0 {
		a.emitLog("No common dates found across sampled tiles - showing all available dates")
		// Fallback: show all dates if filtering is too strict
		for hexDate, tilesWithDate := range allDatesMap {
			// Find most common epoch even in fallback
			epochCounts := make(map[int]int)
			var sampleDateInfo GEAvailableDate

			for _, dateInfo := range tilesWithDate {
				epochCounts[dateInfo.Epoch]++
				sampleDateInfo = dateInfo
			}

			bestEpoch := sampleDateInfo.Epoch
			maxCount := 0
			for epoch, count := range epochCounts {
				if count > maxCount {
					maxCount = count
					bestEpoch = epoch
				}
			}

			if !seen[sampleDateInfo.Date] {
				seen[sampleDateInfo.Date] = true
				dates = append(dates, GEAvailableDate{
					Date:    sampleDateInfo.Date,
					Epoch:   bestEpoch,
					HexDate: hexDate,
				})
				log.Printf("[GEDates] Fallback: Date %s (hex: %s, epoch: %d) from %d tiles",
					sampleDateInfo.Date, hexDate, bestEpoch, len(tilesWithDate))
			}
		}
	}

	// Sort dates newest first so index 0 is the latest
	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Date > dates[j].Date
	})

	a.emitLog(fmt.Sprintf("Found %d dates available across viewport (sampled at zoom %d, requested zoom %d)", len(dates), sampleZoom, zoom))
	return dates, nil
}

// GetGoogleEarthHistoricalTileURL returns the tile URL template for historical Google Earth imagery
// Note: epoch is no longer used in URL - it's looked up per-tile for accuracy
func (a *App) GetGoogleEarthHistoricalTileURL(hexDate string, epoch int) (string, error) {
	if a.tileServerURL == "" {
		return "", fmt.Errorf("tile server not started")
	}
	// Note: epoch parameter kept for API compatibility but not used in URL
	// Each tile looks up its own epoch from the quadtree
	return fmt.Sprintf("%s/ge-historical/%s/{z}/{x}/{y}", a.tileServerURL, hexDate), nil
}

// DownloadGoogleEarthHistoricalImagery downloads historical Google Earth imagery for a bounding box
// Note: epoch parameter kept for API compatibility but the correct epoch is looked up per-tile
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadGoogleEarthHistoricalImagery(bbox BoundingBox, zoom int, hexDate string, epoch int, dateStr string, format string) error {
	a.emitLog(fmt.Sprintf("Starting Google Earth historical download for %s...", dateStr))

	// Get tiles using Google Earth coordinate system
	tiles, err := googleearth.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	if err != nil {
		return err
	}

	total := len(tiles)
	if total == 0 {
		return fmt.Errorf("no tiles in bounding box")
	}
	a.emitLog(fmt.Sprintf("Downloading %d tiles...", total))

	// Find tile bounds for stitching
	var minCol, maxCol, minRow, maxRow int
	first := true
	for _, tile := range tiles {
		if first {
			minCol, maxCol = tile.Column, tile.Column
			minRow, maxRow = tile.Row, tile.Row
			first = false
		} else {
			if tile.Column < minCol {
				minCol = tile.Column
			}
			if tile.Column > maxCol {
				maxCol = tile.Column
			}
			if tile.Row < minRow {
				minRow = tile.Row
			}
			if tile.Row > maxRow {
				maxRow = tile.Row
			}
		}
	}

	cols := maxCol - minCol + 1
	rows := maxRow - minRow + 1
	a.emitLog(fmt.Sprintf("Grid: %d cols x %d rows", cols, rows))

	// Create output image only if we need GeoTIFF
	var outputImg *image.RGBA
	var outputWidth, outputHeight int
	if format == "geotiff" || format == "both" {
		outputWidth = cols * TileSize
		outputHeight = rows * TileSize
		outputImg = image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	}

	// Create tiles directory if saving individual tiles (OGC structure)
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(a.downloadPath, generateTilesDirName("ge_historical", dateStr, zoom))
		if err := os.MkdirAll(tilesDir, 0755); err != nil {
			return fmt.Errorf("failed to create tiles directory: %w", err)
		}
	}

	// Download tiles concurrently with 10 workers
	type tileResult struct {
		tile    *googleearth.Tile
		data    []byte
		index   int
		success bool
	}

	tileChan := make(chan struct {
		tile  *googleearth.Tile
		index int
	}, total)
	resultChan := make(chan tileResult, total)

	// Start worker goroutines
	numWorkers := 10
	if total < numWorkers {
		numWorkers = total
	}

	for w := 0; w < numWorkers; w++ {
		go func() {
			for job := range tileChan {
				// Try with zoom fallback: up to 3 levels down for high zoom (z>=17), 6 levels for lower zoom
				maxFallback := 3
				if zoom < 17 {
					maxFallback = 6
				}

				data, actualZoom, err := a.fetchHistoricalGETileWithZoomFallback(job.tile, hexDate, maxFallback)
				if err != nil {
					log.Printf("[GEHistorical] Failed to download tile %s (tried zoom %d to %d): %v",
						job.tile.Path, zoom, max(zoom-maxFallback, 10), err)
					resultChan <- tileResult{tile: job.tile, index: job.index, success: false}
					continue
				}

				if actualZoom != zoom {
					log.Printf("[GEHistorical] Tile %s downloaded from zoom %d (requested %d)",
						job.tile.Path, actualZoom, zoom)
				}

				resultChan <- tileResult{tile: job.tile, data: data, index: job.index, success: true}
			}
		}()
	}

	// Send tiles to workers
	go func() {
		for i, tile := range tiles {
			tileChan <- struct {
				tile  *googleearth.Tile
				index int
			}{tile: tile, index: i}
		}
		close(tileChan)
	}()

	// Collect results and stitch
	successCount := 0
	processedCount := 0
	for processedCount < total {
		result := <-resultChan
		processedCount++

		// Emit progress
		var status string
		if format == "geotiff" || format == "both" {
			status = fmt.Sprintf("Downloading and merging tile %d/%d", processedCount, total)
		} else {
			status = fmt.Sprintf("Downloading tile %d/%d", processedCount, total)
		}
		a.emitDownloadProgress(DownloadProgress{
			Downloaded:  processedCount,
			Total:       total,
			Percent:     (processedCount * 100) / total,
			Status:      status,
			CurrentDate: a.currentDateIndex,
			TotalDates:  a.totalDatesInRange,
		})

		if !result.success {
			continue
		}

		// Save individual tile if requested (OGC structure: source/date/z/x/y.jpg)
		if format == "tiles" || format == "both" {
			// Create google_earth_historical/date/z/x subdirectories
			sourceDir := filepath.Join(tilesDir, "google_earth_historical", dateStr)
			zDir := filepath.Join(sourceDir, fmt.Sprintf("%d", zoom))
			xDir := filepath.Join(zDir, fmt.Sprintf("%d", result.tile.Column))
			if err := os.MkdirAll(xDir, 0755); err != nil {
				log.Printf("Failed to create tile directories: %v", err)
			} else {
				tilePath := filepath.Join(xDir, fmt.Sprintf("%d.jpg", result.tile.Row))
				if err := os.WriteFile(tilePath, result.data, 0644); err != nil {
					log.Printf("Failed to save tile: %v", err)
				}
			}
		}

		// Decode and stitch for GeoTIFF
		if format == "geotiff" || format == "both" {
			img, err := jpeg.Decode(bytes.NewReader(result.data))
			if err != nil {
				log.Printf("[GEHistorical] Failed to decode tile %s: %v", result.tile.Path, err)
				continue
			}

			// Calculate position in output image
			xOff := (result.tile.Column - minCol) * TileSize
			yOff := (maxRow - result.tile.Row) * TileSize

			// Draw tile onto output image (thread-safe since each tile writes to different location)
			draw.Draw(outputImg, image.Rect(xOff, yOff, xOff+TileSize, yOff+TileSize), img, image.Point{0, 0}, draw.Src)
		}
		successCount++
	}

	a.emitLog(fmt.Sprintf("Processed %d/%d tiles", successCount, total))

	// Check if we have enough tiles to create a meaningful GeoTIFF
	minSuccessRate := 0.3 // Require at least 30% of tiles to succeed
	if successCount == 0 {
		return fmt.Errorf("failed to download any tiles - all attempts failed at all zoom levels")
	}
	if float64(successCount)/float64(total) < minSuccessRate {
		log.Printf("[GEHistorical] Warning: Only %d/%d tiles (%.1f%%) downloaded successfully",
			successCount, total, float64(successCount)/float64(total)*100)
		a.emitLog(fmt.Sprintf("Warning: Only %d/%d tiles downloaded - GeoTIFF may have gaps", successCount, total))
	}

	// Track download completion
	a.TrackEvent("download_complete", map[string]interface{}{
		"source":  "google_earth_historical",
		"zoom":    zoom,
		"total":   total,
		"success": successCount,
		"failed":  total - successCount,
		"format":  format,
		"date":    dateStr,
	})

	// Save GeoTIFF if requested
	if format == "geotiff" || format == "both" {
		// Calculate georeferencing in Web Mercator (EPSG:3857)
		// After Y-inversion, image top-left corresponds to (minCol, maxRow+1) in GE coords
		// Image bottom-right corresponds to (maxCol+1, minRow)
		originX, originY := googleearth.TileToWebMercator(maxRow+1, minCol, zoom)
		endX, endY := googleearth.TileToWebMercator(minRow, maxCol+1, zoom)
		pixelWidth := (endX - originX) / float64(outputWidth)
		pixelHeight := (endY - originY) / float64(outputHeight) // Will be negative (Y decreases going down)

		// Save as GeoTIFF with embedded projection and rich metadata
		tifPath := filepath.Join(a.downloadPath, generateGeoTIFFFilename("ge_historical", dateStr, bbox, zoom))

		// Emit progress for GeoTIFF encoding phase
		a.emitDownloadProgress(DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		a.emitLog("Encoding GeoTIFF file...")
		if err := a.saveAsGeoTIFFWithMetadata(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight, "Google Earth Historical", dateStr); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		a.emitLog(fmt.Sprintf("Saved: %s", tifPath))

		// Save PNG copy for video export compatibility
		a.savePNGCopy(outputImg, tifPath)
	}

	if format == "tiles" || format == "both" {
		a.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     "Complete",
	})

	// Auto-open download folder
	a.emitLog("Opening download folder...")
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
}

// GEDateInfo contains the date info needed for multi-date download
type GEDateInfo struct {
	Date    string `json:"date"`
	HexDate string `json:"hexDate"`
	Epoch   int    `json:"epoch"`
}

// VideoExportOptions contains options for timelapse video export
type VideoExportOptions struct {
	// Dimensions
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Preset  string   `json:"preset"`            // "instagram_square", "tiktok", "youtube", etc.
	Presets []string `json:"presets,omitempty"` // Multiple presets for batch export

	// Crop position (0.0-1.0, where 0.5 is center)
	CropX float64 `json:"cropX"` // 0=left, 0.5=center, 1=right
	CropY float64 `json:"cropY"` // 0=top, 0.5=center, 1=bottom

	// Spotlight area (relative coordinates 0-1 in bbox)
	SpotlightEnabled   bool    `json:"spotlightEnabled"`
	SpotlightCenterLat float64 `json:"spotlightCenterLat"`
	SpotlightCenterLon float64 `json:"spotlightCenterLon"`
	SpotlightRadiusKm  float64 `json:"spotlightRadiusKm"`

	// Overlay
	OverlayOpacity float64 `json:"overlayOpacity"` // 0.0 to 1.0

	// Date overlay
	ShowDateOverlay bool    `json:"showDateOverlay"`
	DateFontSize    float64 `json:"dateFontSize"`
	DatePosition    string  `json:"datePosition"` // "top-left", "top-right", "bottom-left", "bottom-right"

	// Logo overlay
	ShowLogo     bool   `json:"showLogo"`
	LogoPosition string `json:"logoPosition"` // "top-left", "top-right", "bottom-left", "bottom-right"

	// Video settings
	FrameDelay   float64 `json:"frameDelay"`   // Seconds between frames
	OutputFormat string  `json:"outputFormat"` // "mp4", "gif"
	Quality      int     `json:"quality"`      // 0-100
}

// DownloadGoogleEarthHistoricalImageryRange downloads multiple historical Google Earth imagery dates
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadGoogleEarthHistoricalImageryRange(bbox BoundingBox, zoom int, dates []GEDateInfo, format string) error {
	if len(dates) == 0 {
		return fmt.Errorf("no dates provided")
	}

	a.emitLog(fmt.Sprintf("Starting bulk download for %d Google Earth dates", len(dates)))

	// Enable range download mode for unified progress
	a.inRangeDownload = true
	a.totalDatesInRange = len(dates)
	defer func() {
		a.inRangeDownload = false
	}()

	total := len(dates)
	for i, dateInfo := range dates {
		a.currentDateIndex = i + 1

		if err := a.DownloadGoogleEarthHistoricalImagery(bbox, zoom, dateInfo.HexDate, dateInfo.Epoch, dateInfo.Date, format); err != nil {
			a.emitLog(fmt.Sprintf("Failed to download %s: %v", dateInfo.Date, err))
		}
	}

	// Emit completion
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     fmt.Sprintf("Downloaded %d dates", total),
	})

	// Auto-open download folder
	a.emitLog("Opening download folder...")
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
}

// ExportTimelapseVideo exports a timelapse video from a range of downloaded imagery
func (a *App) ExportTimelapseVideo(bbox BoundingBox, zoom int, dates []GEDateInfo, source string, videoOpts VideoExportOptions) error {
	log.Printf("=== ExportTimelapseVideo CALLED ===")
	log.Printf("Parameters: bbox=%+v, zoom=%d, source=%s, dateCount=%d", bbox, zoom, source, len(dates))
	log.Printf("VideoOpts: %+v", videoOpts)

	if len(dates) == 0 {
		log.Printf("ERROR: No dates provided to ExportTimelapseVideo")
		return fmt.Errorf("no dates provided")
	}

	log.Printf("[VideoExport] Starting timelapse video export for %d dates", len(dates))
	log.Printf("[VideoExport] Source: %s, Zoom: %d", source, zoom)
	a.emitLog(fmt.Sprintf("Starting timelapse video export for %d dates", len(dates)))
	a.emitLog(fmt.Sprintf("Source: %s, Zoom: %d", source, zoom))

	// Get download directory
	downloadDir := a.downloadPath
	log.Printf("[VideoExport] Download directory: %s", downloadDir)
	a.emitLog(fmt.Sprintf("Download directory: %s", downloadDir))

	// Prepare video export options
	var preset video.SocialMediaPreset
	switch videoOpts.Preset {
	case "instagram_square":
		preset = video.PresetInstagramSquare
	case "instagram_portrait":
		preset = video.PresetInstagramPortrait
	case "instagram_story":
		preset = video.PresetInstagramStory
	case "tiktok":
		preset = video.PresetTikTok
	case "youtube":
		preset = video.PresetYouTube
	case "youtube_shorts":
		preset = video.PresetYouTubeShorts
	case "twitter":
		preset = video.PresetTwitter
	case "facebook":
		preset = video.PresetFacebook
	default:
		preset = video.PresetCustom
	}

	// Get dimensions from preset or custom
	width, height := videoOpts.Width, videoOpts.Height
	if preset != video.PresetCustom {
		width, height = video.GetPresetDimensions(preset)
	}

	// Default crop position to center if not specified
	cropX := videoOpts.CropX
	cropY := videoOpts.CropY
	if cropX == 0 && cropY == 0 {
		cropX = 0.5
		cropY = 0.5
	}

	opts := &video.ExportOptions{
		Width:           width,
		Height:          height,
		Preset:          preset,
		CropX:           cropX,
		CropY:           cropY,
		UseSpotlight:    videoOpts.SpotlightEnabled,
		OverlayOpacity:  videoOpts.OverlayOpacity,
		OverlayColor:    video.DefaultExportOptions().OverlayColor, // Use default black
		ShowDateOverlay: videoOpts.ShowDateOverlay,
		DateFontSize:    videoOpts.DateFontSize,
		DatePosition:    videoOpts.DatePosition,
		DateColor:       video.DefaultExportOptions().DateColor, // Use default white
		DateShadow:      true,
		DateFormat:      "Jan 02, 2006",
		DateFontData:    dateFontData, // Use embedded Arial Unicode font
		ShowLogo:        videoOpts.ShowLogo,
		LogoPosition:    videoOpts.LogoPosition,
		LogoScale:       0.6,
		FrameRate:       30,
		FrameDelay:      videoOpts.FrameDelay,
		OutputFormat:    videoOpts.OutputFormat,
		Quality:         videoOpts.Quality,
		UseH264:         true, // Try to use H.264 if FFmpeg is available
	}

	// Load logo image if enabled
	if videoOpts.ShowLogo {
		logoImg, err := a.loadLogoImage()
		if err != nil {
			log.Printf("[VideoExport] Warning: Failed to load logo: %v", err)
		} else {
			opts.LogoImage = logoImg
			log.Printf("[VideoExport] Logo image loaded")
		}
	}

	// If spotlight is enabled, calculate pixel coordinates from geographic coordinates
	if videoOpts.SpotlightEnabled {
		// For now, we'll process the full image and let the video processor handle spotlight
		// This requires loading the full GeoTIFF to get pixel dimensions first
		a.emitLog("Spotlight mode enabled - will calculate coordinates from first frame")
	}

	// Create video exporter
	log.Printf("[VideoExport] Creating video exporter...")
	exporter, err := video.NewExporter(opts)
	if err != nil {
		log.Printf("[VideoExport] ERROR: Failed to create video exporter: %v", err)
		return fmt.Errorf("failed to create video exporter: %w", err)
	}
	defer exporter.Close()
	log.Printf("[VideoExport] Video exporter created successfully")

	// Load frames from GeoTIFFs
	frames := make([]video.Frame, 0, len(dates))
	log.Printf("[VideoExport] Starting frame loading loop for %d dates", len(dates))

	for i, dateInfo := range dates {
		log.Printf("[VideoExport] Processing date %d/%d: %s", i+1, len(dates), dateInfo.Date)
		a.emitDownloadProgress(DownloadProgress{
			Downloaded: i,
			Total:      len(dates),
			Percent:    (i * 100) / len(dates),
			Status:     fmt.Sprintf("Loading frame %d/%d: %s", i+1, len(dates), dateInfo.Date),
		})

		// Construct GeoTIFF path using same generateGeoTIFFFilename function as downloads
		// Convert source to match download naming convention
		downloadSource := source
		if source == "google" || source == "ge" {
			downloadSource = "ge_historical"
		}
		filename := generateGeoTIFFFilename(downloadSource, dateInfo.Date, bbox, zoom)
		basePath := filepath.Join(downloadDir, filename)

		// Try loading PNG first (created as sidecar for better compatibility)
		imagePath := strings.TrimSuffix(basePath, ".tif") + ".png"
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			// Fallback to GeoTIFF if PNG not found
			imagePath = basePath
		}

		log.Printf("[VideoExport] Looking for frame: %s", imagePath)
		a.emitLog(fmt.Sprintf("Looking for frame: %s", imagePath))

		// Check if file exists
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			log.Printf("[VideoExport] ❌ Frame not found for %s: %s", dateInfo.Date, imagePath)
			a.emitLog(fmt.Sprintf("❌ Frame not found for %s: %s", dateInfo.Date, imagePath))
			continue
		}

		log.Printf("[VideoExport] ✅ Found frame for %s", dateInfo.Date)
		a.emitLog(fmt.Sprintf("✅ Found frame for %s", dateInfo.Date))

		// Load image
		log.Printf("[VideoExport] Attempting to load image from: %s", imagePath)
		img, err := a.loadGeoTIFFImage(imagePath)
		if err != nil {
			log.Printf("[VideoExport] ❌ ERROR: Failed to load GeoTIFF for %s: %v", dateInfo.Date, err)
			a.emitLog(fmt.Sprintf("Failed to load GeoTIFF for %s: %v", dateInfo.Date, err))
			continue
		}
		log.Printf("[VideoExport] ✅ Successfully loaded image for %s", dateInfo.Date)

		// Convert to RGBA if needed
		var rgba *image.RGBA
		if rgbaImg, ok := img.(*image.RGBA); ok {
			rgba = rgbaImg
		} else {
			bounds := img.Bounds()
			rgba = image.NewRGBA(bounds)
			draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)
		}

		// Calculate spotlight coordinates from geographic coordinates on first frame
		if videoOpts.SpotlightEnabled && i == 0 {
			// Calculate spotlight pixel coordinates based on geographic center and radius
			spotlightPixels := a.calculateSpotlightPixels(
				bbox, zoom,
				videoOpts.SpotlightCenterLat, videoOpts.SpotlightCenterLon,
				videoOpts.SpotlightRadiusKm,
				rgba.Bounds(),
			)
			opts.SpotlightX = spotlightPixels.X
			opts.SpotlightY = spotlightPixels.Y
			opts.SpotlightWidth = spotlightPixels.Width
			opts.SpotlightHeight = spotlightPixels.Height
			a.emitLog(fmt.Sprintf("Spotlight area: x=%d y=%d w=%d h=%d",
				spotlightPixels.X, spotlightPixels.Y, spotlightPixels.Width, spotlightPixels.Height))
		}

		// Parse date
		parsedDate, err := time.Parse("2006-01-02", dateInfo.Date)
		if err != nil {
			a.emitLog(fmt.Sprintf("Failed to parse date %s: %v", dateInfo.Date, err))
			parsedDate = time.Now()
		}

		frames = append(frames, video.Frame{
			Image: rgba,
			Date:  parsedDate,
		})
	}

	log.Printf("[VideoExport] Total frames loaded: %d", len(frames))
	a.emitLog(fmt.Sprintf("Total frames loaded: %d", len(frames)))

	if len(frames) == 0 {
		log.Printf("[VideoExport] ❌ ERROR: No frames loaded - ensure GeoTIFFs are downloaded first")
		a.emitLog("❌ ERROR: No frames loaded - ensure GeoTIFFs are downloaded first")
		return fmt.Errorf("no frames loaded - ensure GeoTIFFs are downloaded first")
	}

	log.Printf("[VideoExport] ✅ Loaded %d frames successfully, starting video encoding...", len(frames))
	a.emitLog(fmt.Sprintf("✅ Loaded %d frames successfully, starting video encoding...", len(frames)))

	// Generate output filename
	outputFilename := fmt.Sprintf("%s_timelapse_%s_to_%s_%s.%s",
		source,
		dates[0].Date,
		dates[len(dates)-1].Date,
		videoOpts.Preset,
		videoOpts.OutputFormat,
	)
	outputPath := filepath.Join(downloadDir, "timelapse_exports", outputFilename)

	// Create output directory
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Export video
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: len(frames),
		Total:      len(frames),
		Percent:    99,
		Status:     "Encoding video...",
	})

	if err := exporter.ExportVideo(frames, outputPath); err != nil {
		return fmt.Errorf("failed to export video: %w", err)
	}

	a.emitLog(fmt.Sprintf("Video exported successfully: %s", outputPath))

	// Emit completion
	a.emitDownloadProgress(DownloadProgress{
		Downloaded: len(frames),
		Total:      len(frames),
		Percent:    100,
		Status:     fmt.Sprintf("Video export complete: %s", filepath.Base(outputPath)),
	})

	// Auto-open download folder
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
}

// ReExportVideo re-exports video from a completed task with new presets
func (a *App) ReExportVideo(taskID string, presets []string, videoFormat string) error {
	log.Printf("[ReExport] Starting re-export for task %s with presets: %v", taskID, presets)

	// Get the task from the queue
	task, err := a.taskQueue.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if task.Status != "completed" {
		return fmt.Errorf("task is not completed (status: %s)", task.Status)
	}

	if task.OutputPath == "" {
		return fmt.Errorf("task has no output path")
	}

	if task.VideoOpts == nil {
		return fmt.Errorf("task has no video options")
	}

	// Convert types for internal use
	bbox := BoundingBox(task.BBox)
	dates := make([]GEDateInfo, len(task.Dates))
	for i, d := range task.Dates {
		dates[i] = GEDateInfo{
			Date:    d.Date,
			HexDate: d.HexDate,
			Epoch:   d.Epoch,
		}
	}

	// Save original download path
	originalDownloadPath := a.downloadPath
	a.downloadPath = task.OutputPath
	defer func() {
		a.downloadPath = originalDownloadPath
	}()

	// Export for each preset
	for i, presetID := range presets {
		log.Printf("[ReExport] Exporting preset %d/%d: %s", i+1, len(presets), presetID)

		a.emitDownloadProgress(DownloadProgress{
			Downloaded:  i,
			Total:       len(presets),
			Percent:     (i * 100) / len(presets),
			Status:      fmt.Sprintf("Exporting %s (%d/%d)", presetID, i+1, len(presets)),
			CurrentDate: i + 1,
			TotalDates:  len(presets),
		})

		// Create video options for this preset
		videoOpts := VideoExportOptions{
			Preset:             presetID,
			CropX:              task.VideoOpts.CropX,
			CropY:              task.VideoOpts.CropY,
			SpotlightEnabled:   task.VideoOpts.SpotlightEnabled,
			SpotlightCenterLat: task.VideoOpts.SpotlightCenterLat,
			SpotlightCenterLon: task.VideoOpts.SpotlightCenterLon,
			SpotlightRadiusKm:  task.VideoOpts.SpotlightRadiusKm,
			OverlayOpacity:     task.VideoOpts.OverlayOpacity,
			ShowDateOverlay:    task.VideoOpts.ShowDateOverlay,
			DateFontSize:       task.VideoOpts.DateFontSize,
			DatePosition:       task.VideoOpts.DatePosition,
			ShowLogo:           task.VideoOpts.ShowLogo,
			LogoPosition:       task.VideoOpts.LogoPosition,
			FrameDelay:         task.VideoOpts.FrameDelay,
			OutputFormat:       videoFormat,
			Quality:            task.VideoOpts.Quality,
		}

		if err := a.ExportTimelapseVideo(bbox, task.Zoom, dates, task.Source, videoOpts); err != nil {
			log.Printf("[ReExport] Failed to export preset %s: %v", presetID, err)
			// Continue with other presets
		}
	}

	a.emitDownloadProgress(DownloadProgress{
		Downloaded:  len(presets),
		Total:       len(presets),
		Percent:     100,
		Status:      fmt.Sprintf("Re-export complete (%d videos)", len(presets)),
		CurrentDate: len(presets),
		TotalDates:  len(presets),
	})

	log.Printf("[ReExport] Completed re-export for task %s", taskID)
	return nil
}

// loadGeoTIFFImage loads an image from a GeoTIFF file
func (a *App) loadGeoTIFFImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Decode using standard image package (supports TIFF)
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// SpotlightPixels represents pixel coordinates for spotlight area
type SpotlightPixels struct {
	X, Y, Width, Height int
}

// calculateSpotlightPixels converts geographic spotlight coordinates to pixel coordinates
func (a *App) calculateSpotlightPixels(bbox BoundingBox, zoom int, centerLat, centerLon, radiusKm float64, imgBounds image.Rectangle) SpotlightPixels {
	// Convert bbox and center to Web Mercator coordinates (meters)
	toWebMercator := func(lat, lon float64) (x, y float64) {
		x = lon * 20037508.34 / 180.0
		y = math.Log(math.Tan((90+lat)*math.Pi/360.0)) / (math.Pi / 180.0)
		y = y * 20037508.34 / 180.0
		return
	}

	// Calculate image extent in Web Mercator
	westX, southY := toWebMercator(bbox.South, bbox.West)
	eastX, northY := toWebMercator(bbox.North, bbox.East)

	// Calculate spotlight center in Web Mercator
	centerX, centerY := toWebMercator(centerLat, centerLon)

	// Convert radius from km to meters
	radiusMeters := radiusKm * 1000.0

	// Calculate image dimensions
	imgWidth := float64(imgBounds.Dx())
	imgHeight := float64(imgBounds.Dy())

	// Calculate pixel scale
	scaleX := imgWidth / (eastX - westX)
	scaleY := imgHeight / (northY - southY)

	// Convert spotlight center to pixel coordinates
	spotlightCenterX := int((centerX - westX) * scaleX)
	spotlightCenterY := int((northY - centerY) * scaleY) // Y is inverted in image coordinates

	// Convert radius to pixels (use average of X and Y scales)
	avgScale := (scaleX + scaleY) / 2.0
	spotlightRadiusPixels := int(radiusMeters * avgScale)

	return SpotlightPixels{
		X:      spotlightCenterX - spotlightRadiusPixels,
		Y:      spotlightCenterY - spotlightRadiusPixels,
		Width:  spotlightRadiusPixels * 2,
		Height: spotlightRadiusPixels * 2,
	}
}

// ============================================================================
// Task Queue API Methods
// ============================================================================

// TaskQueueExportTask is the frontend-facing export task structure
type TaskQueueExportTask struct {
	ID          string                        `json:"id"`
	Name        string                        `json:"name"`
	Status      string                        `json:"status"`
	Priority    int                           `json:"priority"`
	CreatedAt   string                        `json:"createdAt"`
	StartedAt   string                        `json:"startedAt,omitempty"`
	CompletedAt string                        `json:"completedAt,omitempty"`
	Source      string                        `json:"source"`
	BBox        BoundingBox                   `json:"bbox"`
	Zoom        int                           `json:"zoom"`
	Format      string                        `json:"format"`
	Dates       []GEDateInfo                  `json:"dates"`
	VideoExport bool                          `json:"videoExport"`
	VideoOpts   *VideoExportOptions           `json:"videoOpts,omitempty"`
	CropPreview *taskqueue.CropPreview        `json:"cropPreview,omitempty"`
	Progress    taskqueue.TaskProgress        `json:"progress"`
	Error       string                        `json:"error,omitempty"`
	OutputPath  string                        `json:"outputPath,omitempty"`
}

// convertTaskToFrontend converts internal task to frontend format
func convertTaskToFrontend(t *taskqueue.ExportTask) TaskQueueExportTask {
	result := TaskQueueExportTask{
		ID:          t.ID,
		Name:        t.Name,
		Status:      string(t.Status),
		Priority:    t.Priority,
		CreatedAt:   t.CreatedAt,   // Already a string (RFC3339)
		StartedAt:   t.StartedAt,   // Already a string (RFC3339)
		CompletedAt: t.CompletedAt, // Already a string (RFC3339)
		Source:      t.Source,
		BBox:        BoundingBox(t.BBox),
		Zoom:        t.Zoom,
		Format:      t.Format,
		VideoExport: t.VideoExport,
		CropPreview: t.CropPreview,
		Progress:    t.Progress,
		Error:       t.Error,
		OutputPath:  t.OutputPath,
	}

	// Convert dates
	result.Dates = make([]GEDateInfo, len(t.Dates))
	for i, d := range t.Dates {
		result.Dates[i] = GEDateInfo{
			Date:    d.Date,
			HexDate: d.HexDate,
			Epoch:   d.Epoch,
		}
	}

	// Convert video options
	if t.VideoOpts != nil {
		result.VideoOpts = &VideoExportOptions{
			Width:              t.VideoOpts.Width,
			Height:             t.VideoOpts.Height,
			Preset:             t.VideoOpts.Preset,
			CropX:              t.VideoOpts.CropX,
			CropY:              t.VideoOpts.CropY,
			SpotlightEnabled:   t.VideoOpts.SpotlightEnabled,
			SpotlightCenterLat: t.VideoOpts.SpotlightCenterLat,
			SpotlightCenterLon: t.VideoOpts.SpotlightCenterLon,
			SpotlightRadiusKm:  t.VideoOpts.SpotlightRadiusKm,
			OverlayOpacity:     t.VideoOpts.OverlayOpacity,
			ShowDateOverlay:    t.VideoOpts.ShowDateOverlay,
			DateFontSize:       t.VideoOpts.DateFontSize,
			DatePosition:       t.VideoOpts.DatePosition,
			ShowLogo:           t.VideoOpts.ShowLogo,
			LogoPosition:       t.VideoOpts.LogoPosition,
			FrameDelay:         t.VideoOpts.FrameDelay,
			OutputFormat:       t.VideoOpts.OutputFormat,
			Quality:            t.VideoOpts.Quality,
		}
	}

	return result
}

// AddExportTask adds a new export task to the queue
func (a *App) AddExportTask(taskData TaskQueueExportTask) (string, error) {
	// Convert dates
	dates := make([]taskqueue.GEDateInfo, len(taskData.Dates))
	for i, d := range taskData.Dates {
		dates[i] = taskqueue.GEDateInfo{
			Date:    d.Date,
			HexDate: d.HexDate,
			Epoch:   d.Epoch,
		}
	}

	// Create task
	task := taskqueue.NewExportTask(
		taskData.Name,
		taskData.Source,
		taskqueue.BoundingBox(taskData.BBox),
		taskData.Zoom,
		dates,
	)

	task.Format = taskData.Format
	task.Priority = taskData.Priority
	task.VideoExport = taskData.VideoExport
	task.CropPreview = taskData.CropPreview

	// Convert video options
	if taskData.VideoOpts != nil {
		task.VideoOpts = &taskqueue.VideoExportOptions{
			Width:              taskData.VideoOpts.Width,
			Height:             taskData.VideoOpts.Height,
			Preset:             taskData.VideoOpts.Preset,
			Presets:            taskData.VideoOpts.Presets, // Multi-preset support
			CropX:              taskData.VideoOpts.CropX,
			CropY:              taskData.VideoOpts.CropY,
			SpotlightEnabled:   taskData.VideoOpts.SpotlightEnabled,
			SpotlightCenterLat: taskData.VideoOpts.SpotlightCenterLat,
			SpotlightCenterLon: taskData.VideoOpts.SpotlightCenterLon,
			SpotlightRadiusKm:  taskData.VideoOpts.SpotlightRadiusKm,
			OverlayOpacity:     taskData.VideoOpts.OverlayOpacity,
			ShowDateOverlay:    taskData.VideoOpts.ShowDateOverlay,
			DateFontSize:       taskData.VideoOpts.DateFontSize,
			DatePosition:       taskData.VideoOpts.DatePosition,
			ShowLogo:           taskData.VideoOpts.ShowLogo,
			LogoPosition:       taskData.VideoOpts.LogoPosition,
			FrameDelay:         taskData.VideoOpts.FrameDelay,
			OutputFormat:       taskData.VideoOpts.OutputFormat,
			Quality:            taskData.VideoOpts.Quality,
		}
	}

	if err := a.taskQueue.AddTask(task); err != nil {
		return "", err
	}

	return task.ID, nil
}

// GetTaskQueue returns all tasks in the queue
func (a *App) GetTaskQueue() ([]TaskQueueExportTask, error) {
	tasks := a.taskQueue.GetAllTasks()
	result := make([]TaskQueueExportTask, len(tasks))
	for i, t := range tasks {
		result[i] = convertTaskToFrontend(t)
	}
	return result, nil
}

// GetTask returns a single task by ID
func (a *App) GetTask(id string) (*TaskQueueExportTask, error) {
	task, err := a.taskQueue.GetTask(id)
	if err != nil {
		return nil, err
	}
	result := convertTaskToFrontend(task)
	return &result, nil
}

// UpdateTask updates a task's properties
func (a *App) UpdateTask(id string, updates map[string]interface{}) error {
	return a.taskQueue.UpdateTask(id, updates)
}

// DeleteTask removes a task from the queue
func (a *App) DeleteTask(id string) error {
	return a.taskQueue.DeleteTask(id)
}

// StartTaskQueue begins processing tasks
func (a *App) StartTaskQueue() error {
	return a.taskQueue.StartQueue()
}

// PauseTaskQueue pauses the queue after the current task completes
func (a *App) PauseTaskQueue() error {
	return a.taskQueue.PauseQueue()
}

// StopTaskQueue stops the queue immediately
func (a *App) StopTaskQueue() {
	a.taskQueue.StopQueue()
}

// CancelTask cancels a running or pending task
func (a *App) CancelTask(id string) error {
	return a.taskQueue.CancelTask(id)
}

// ReorderTask moves a task to a new position in the queue
func (a *App) ReorderTask(id string, newIndex int) error {
	return a.taskQueue.ReorderTask(id, newIndex)
}

// GetTaskQueueStatus returns the current queue status
func (a *App) GetTaskQueueStatus() taskqueue.QueueStatus {
	return a.taskQueue.GetStatus()
}

// ClearCompletedTasks removes all completed/failed/cancelled tasks
func (a *App) ClearCompletedTasks() {
	a.taskQueue.ClearCompleted()
}

// ExecuteExportTask implements the TaskExecutor interface
// This is called by the queue worker to actually perform the export
func (a *App) ExecuteExportTask(ctx context.Context, task *taskqueue.ExportTask, progressChan chan<- taskqueue.TaskProgress) error {
	log.Printf("[TaskQueue] Executing task: %s - %s", task.ID, task.Name)

	// Set up task context for progress tracking
	a.mu.Lock()
	a.currentTaskID = task.ID
	a.taskProgressChan = progressChan
	// Create task-specific output directory
	a.taskOutputPath = filepath.Join(a.downloadPath, task.ID)
	if err := os.MkdirAll(a.taskOutputPath, 0755); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("failed to create task output directory: %w", err)
	}
	// Save original download path to restore later
	originalDownloadPath := a.downloadPath
	a.downloadPath = a.taskOutputPath
	a.mu.Unlock()

	// Ensure we clean up task context when done
	defer func() {
		a.mu.Lock()
		a.currentTaskID = ""
		a.taskProgressChan = nil
		a.downloadPath = originalDownloadPath
		// Set the output path on the task
		task.OutputPath = a.taskOutputPath
		a.taskOutputPath = ""
		a.mu.Unlock()
	}()

	// Convert types for internal use
	bbox := BoundingBox(task.BBox)
	dates := make([]GEDateInfo, len(task.Dates))
	for i, d := range task.Dates {
		dates[i] = GEDateInfo{
			Date:    d.Date,
			HexDate: d.HexDate,
			Epoch:   d.Epoch,
		}
	}

	// Enable range download mode for proper progress tracking
	a.inRangeDownload = true
	a.totalDatesInRange = len(dates)
	defer func() {
		a.inRangeDownload = false
	}()

	// For Esri: deduplicate by checking center tile hash
	var esriSeenHashes map[string]string
	var esriCenterTile *esri.EsriTile
	if task.Source == "esri" {
		esriSeenHashes = make(map[string]string)
		centerLat := (bbox.South + bbox.North) / 2
		centerLon := (bbox.West + bbox.East) / 2
		esriCenterTile, _ = esri.GetTileForWgs84(centerLat, centerLon, task.Zoom)
	}

	// Track progress
	totalDates := len(dates)
	downloadedCount := 0
	skippedCount := 0

	for i, dateInfo := range dates {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		a.currentDateIndex = i + 1

		// Download imagery based on source
		var err error
		switch task.Source {
		case "google", "ge":
			err = a.DownloadGoogleEarthHistoricalImagery(bbox, task.Zoom, dateInfo.HexDate, dateInfo.Epoch, dateInfo.Date, task.Format)
			if err == nil {
				downloadedCount++
			}
		case "esri":
			// Deduplicate Esri downloads by checking center tile hash
			// Also detect blank tiles (no coverage at this zoom level)
			shouldDownload := true
			if esriCenterTile != nil {
				layer, layerErr := a.findLayerForDate(dateInfo.Date)
				if layerErr == nil {
					tileData, tileErr := a.esriClient.FetchTile(layer, esriCenterTile)
					if tileErr == nil {
						// Check if tile is blank (no coverage at this zoom level)
						if isBlankTile(tileData) {
							log.Printf("[TaskQueue] Esri date %s has no coverage at zoom %d, skipping", dateInfo.Date, task.Zoom)
							skippedCount++
							shouldDownload = false
						} else {
							// Check for duplicate imagery
							hashKey := fmt.Sprintf("%x", sha256.Sum256(tileData))
							if firstDate, seen := esriSeenHashes[hashKey]; seen {
								log.Printf("[TaskQueue] Esri date %s has same imagery as %s, skipping", dateInfo.Date, firstDate)
								skippedCount++
								shouldDownload = false
							} else {
								esriSeenHashes[hashKey] = dateInfo.Date
							}
						}
					}
				}
			}

			if shouldDownload {
				err = a.DownloadEsriImagery(bbox, task.Zoom, dateInfo.Date, task.Format)
				if err == nil {
					downloadedCount++
				}
			}
		default:
			err = fmt.Errorf("unknown source: %s", task.Source)
		}

		if err != nil {
			log.Printf("[TaskQueue] Failed to download date %s: %v", dateInfo.Date, err)
			// Continue with other dates, don't fail the entire task
		}
	}

	if skippedCount > 0 {
		log.Printf("[TaskQueue] Downloaded %d unique dates, skipped %d duplicates", downloadedCount, skippedCount)
	}

	// If video export is requested, do it after all imagery is downloaded
	if task.VideoExport && task.VideoOpts != nil {
		// Determine which presets to export
		presetsToExport := task.VideoOpts.Presets
		if len(presetsToExport) == 0 {
			// Fallback to single preset if no presets array provided
			presetsToExport = []string{task.VideoOpts.Preset}
		}

		log.Printf("[TaskQueue] Exporting %d video presets: %v", len(presetsToExport), presetsToExport)

		for i, presetID := range presetsToExport {
			a.emitDownloadProgress(DownloadProgress{
				Downloaded:  i,
				Total:       len(presetsToExport),
				Percent:     95 + (i * 5 / len(presetsToExport)),
				Status:      fmt.Sprintf("Encoding video %d/%d (%s)...", i+1, len(presetsToExport), presetID),
				CurrentDate: totalDates,
				TotalDates:  totalDates,
			})

			// Convert video options for this preset
			videoOpts := VideoExportOptions{
				Preset:             presetID,
				CropX:              task.VideoOpts.CropX,
				CropY:              task.VideoOpts.CropY,
				SpotlightEnabled:   task.VideoOpts.SpotlightEnabled,
				SpotlightCenterLat: task.VideoOpts.SpotlightCenterLat,
				SpotlightCenterLon: task.VideoOpts.SpotlightCenterLon,
				SpotlightRadiusKm:  task.VideoOpts.SpotlightRadiusKm,
				OverlayOpacity:     task.VideoOpts.OverlayOpacity,
				ShowDateOverlay:    task.VideoOpts.ShowDateOverlay,
				DateFontSize:       task.VideoOpts.DateFontSize,
				DatePosition:       task.VideoOpts.DatePosition,
				ShowLogo:           task.VideoOpts.ShowLogo,
				LogoPosition:       task.VideoOpts.LogoPosition,
				FrameDelay:         task.VideoOpts.FrameDelay,
				OutputFormat:       task.VideoOpts.OutputFormat,
				Quality:            task.VideoOpts.Quality,
			}

			if err := a.ExportTimelapseVideo(bbox, task.Zoom, dates, task.Source, videoOpts); err != nil {
				log.Printf("[TaskQueue] Failed to export preset %s: %v", presetID, err)
				// Continue with other presets, don't fail the entire task
			}
		}
	}

	// Final progress update
	progress := taskqueue.TaskProgress{
		CurrentPhase:   "completed",
		CurrentDate:    totalDates,
		TotalDates:     totalDates,
		TilesCompleted: 0,
		TilesTotal:     0,
		Percent:        100,
	}
	progressChan <- progress

	log.Printf("[TaskQueue] Task completed: %s", task.ID)
	return nil
}

// loadLogoImage loads the embedded logo image for video overlays
func (a *App) loadLogoImage() (image.Image, error) {
	if len(logoImageData) == 0 {
		return nil, fmt.Errorf("logo image not embedded")
	}

	img, err := png.Decode(bytes.NewReader(logoImageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode logo: %w", err)
	}

	return img, nil
}
