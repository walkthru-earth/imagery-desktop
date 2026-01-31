package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"imagery-desktop/pkg/geotiff"

	"github.com/posthog/posthog-go"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"imagery-desktop/internal/cache"
	"imagery-desktop/internal/common"
	"imagery-desktop/internal/config"
	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/downloads/esri"
	geDownloader "imagery-desktop/internal/downloads/googleearth"
	esriClient "imagery-desktop/internal/esri"
	"imagery-desktop/internal/googleearth"
	"imagery-desktop/internal/handlers/tileserver"
	"imagery-desktop/internal/imagery"
	"imagery-desktop/internal/ratelimit"
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
)

// Helper function for max of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Concrete types for Wails bindings (duplicated from downloads package)
// Wails doesn't generate TypeScript bindings for type aliases, only concrete struct types

// BoundingBox represents a geographic bounding box (duplicated for Wails bindings)
type BoundingBox struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

// DownloadProgress tracks download progress (duplicated for Wails bindings)
type DownloadProgress struct {
	Downloaded  int    `json:"downloaded"`
	Total       int    `json:"total"`
	Percent     int    `json:"percent"`
	Status      string `json:"status"`
	CurrentDate int    `json:"currentDate"`
	TotalDates  int    `json:"totalDates"`
}

// GEDateInfo contains Google Earth historical date information (duplicated for Wails bindings)
type GEDateInfo struct {
	Date    string `json:"date"`
	HexDate string `json:"hexDate"`
	Epoch   int    `json:"epoch"`
}

// GEAvailableDate represents an available Google Earth historical date (duplicated for Wails bindings)
type GEAvailableDate struct {
	Date    string `json:"date"`
	Epoch   int    `json:"epoch"`
	HexDate string `json:"hexDate"`
}

// Conversion helpers between app types and downloads package types

func (b BoundingBox) toDownloadsBBox() downloads.BoundingBox {
	return downloads.BoundingBox{
		South: b.South,
		West:  b.West,
		North: b.North,
		East:  b.East,
	}
}

func (d GEDateInfo) toDownloadsDateInfo() downloads.GEDateInfo {
	return downloads.GEDateInfo{
		Date:    d.Date,
		HexDate: d.HexDate,
		Epoch:   d.Epoch,
	}
}

func convertGEDateInfoSlice(dates []GEDateInfo) []downloads.GEDateInfo {
	result := make([]downloads.GEDateInfo, len(dates))
	for i, d := range dates {
		result[i] = d.toDownloadsDateInfo()
	}
	return result
}

func fromDownloadsGEAvailableDate(d downloads.GEAvailableDate) GEAvailableDate {
	return GEAvailableDate{
		Date:    d.Date,
		Epoch:   d.Epoch,
		HexDate: d.HexDate,
	}
}

func fromDownloadsGEAvailableDateSlice(dates []downloads.GEAvailableDate) []GEAvailableDate {
	result := make([]GEAvailableDate, len(dates))
	for i, d := range dates {
		result[i] = fromDownloadsGEAvailableDate(d)
	}
	return result
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

// App struct
type App struct {
	ctx               context.Context
	geClient          *googleearth.Client
	esriClient        *esriClient.Client
	tileCache         *cache.PersistentTileCache // Changed to PersistentTileCache
	downloader        *imagery.TileDownloader
	esriDownloader    *esri.Downloader        // Esri-specific downloader
	geDownloader      *geDownloader.Downloader // Google Earth downloader
	downloadPath      string
	tileServer        *tileserver.Server // Tile server for serving decrypted Google Earth tiles
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

	// Folder open tracking (to avoid opening duplicate windows on Windows)
	lastOpenedFolders map[string]time.Time // Map of folder path -> last opened time
	folderOpenMu      sync.Mutex           // Mutex for folder open tracking

	// Rate limit handling
	rateLimitHandler *ratelimit.Handler // Rate limit detection and retry

	// Video export manager
	videoManager *video.Manager // Handles timelapse video export
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

	// Initialize persistent tile cache with OGC ZXY structure
	cachePath := config.GetCachePath(settings)
	tileCache, err := cache.NewPersistentTileCache(cachePath, settings.CacheMaxSizeMB, settings.CacheTTLDays)
	if err != nil {
		log.Printf("Failed to initialize tile cache: %v", err)
		tileCache = nil // Continue without cache
	} else {
		entries, sizeBytes, maxBytes := tileCache.Stats()
		log.Printf("Tile cache initialized at %s (%d tiles, %.2f MB / %.2f MB, TTL %d days)",
			cachePath, entries, float64(sizeBytes)/1024/1024, float64(maxBytes)/1024/1024, settings.CacheTTLDays)
	}

	// Initialize rate limit handler
	rateLimitHandler := ratelimit.NewHandler(nil) // Use default retry strategy
	rateLimitHandler.SetAutoRetry(settings.AutoRetryOnRateLimit)
	log.Printf("Rate limit handler initialized (auto-retry: %v)", settings.AutoRetryOnRateLimit)

	// Initialize unified downloader (pass nil for now, will update cache calls separately)
	downloader := imagery.NewTileDownloader(downloads.DefaultWorkers, nil)

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

	esriClientInstance := esriClient.NewClient()

	// Note: esriDownloader will be initialized after app is created
	// so it can access app's callback methods

	app := &App{
		geClient:          googleearth.NewClient(),
		esriClient:        esriClientInstance,
		tileCache:         tileCache,
		downloader:        downloader,
		downloadPath:      settings.DownloadPath,
		settings:          settings,
		phClient:          phClient,
		taskQueue:         taskQueue,
		lastOpenedFolders: make(map[string]time.Time),
		rateLimitHandler:  rateLimitHandler,
	}

	// Initialize Esri downloader with app callbacks
	app.esriDownloader = esri.NewDownloader(
		esriClientInstance,
		tileCache,
		settings.DownloadPath,
		app.emitDownloadProgressFromDownloads,
		app.emitLog,
		rateLimitHandler,
		app.TrackEvent,
		downloads.DefaultWorkers,
	)

	// Set up rate limit callbacks (will be called when rate limits are detected)
	rateLimitHandler.SetOnRateLimit(func(event ratelimit.RateLimitEvent) {
		log.Printf("[RateLimit] %s", event.Message)
		// Event will be emitted to frontend in startup() after ctx is available
	})

	rateLimitHandler.SetOnRetry(func(event ratelimit.RateLimitEvent) {
		log.Printf("[RateLimit] Retrying %s (attempt %d)", event.Provider, event.RetryAttempt+1)
	})

	rateLimitHandler.SetOnRecovered(func(provider string) {
		log.Printf("[RateLimit] %s rate limit cleared - downloads resumed", provider)
	})

	// Initialize video manager with callbacks
	app.videoManager = video.NewManager(video.Config{
		DownloadPath: settings.DownloadPath,
		DateFontData: dateFontData,
		ProgressCallback: func(current, total, percent int, status string) {
			// Convert video progress to download progress format
			app.emitDownloadProgress(DownloadProgress{
				Downloaded: current,
				Total:      total,
				Percent:    percent,
				Status:     status,
			})
		},
		LogCallback: app.emitLog,
		ImageLoader: app.loadGeoTIFFImage,
		LogoLoader:  app.loadLogoImage,
		SpotlightCalculator: func(bbox video.BoundingBox, zoom int, centerLat, centerLon, radiusKm float64, imageBounds image.Rectangle) video.SpotlightPixels {
			// Convert video.BoundingBox to app.BoundingBox and call app method
			appBBox := BoundingBox{
				South: bbox.South,
				West:  bbox.West,
				North: bbox.North,
				East:  bbox.East,
			}
			appSpotlight := app.calculateSpotlightPixels(appBBox, zoom, centerLat, centerLon, radiusKm, imageBounds)
			// Convert app.SpotlightPixels to video.SpotlightPixels
			return video.SpotlightPixels{
				X:      appSpotlight.X,
				Y:      appSpotlight.Y,
				Width:  appSpotlight.Width,
				Height: appSpotlight.Height,
			}
		},
	})

	return app
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

	// Load Esri layers for tile server caching
	esriLayers, err := a.esriClient.GetLayers()
	if err != nil {
		wailsRuntime.LogWarning(ctx, fmt.Sprintf("Failed to load Esri layers: %v", err))
		esriLayers = []*esriClient.Layer{} // Use empty slice if loading fails
	}

	// Initialize and start local tile server
	a.tileServer = tileserver.NewServer(ctx, a.geClient, a.esriClient, esriLayers, a.tileCache, a.devMode)
	go func() {
		if err := a.tileServer.Start(); err != nil {
			wailsRuntime.LogError(ctx, fmt.Sprintf("Failed to start tile server: %v", err))
		}
	}()

	// Initialize Google Earth downloader with all dependencies
	geDownloaderInstance, err := geDownloader.NewDownloader(geDownloader.Config{
		GEClient:          a.geClient,
		TileCache:         a.tileCache,
		DownloadPath:      a.settings.DownloadPath,
		ProgressCallback:  a.emitDownloadProgressFromDownloads,
		LogCallback:       a.emitLog,
		RateLimitHandler:  a.rateLimitHandler,
		TrackEventCallback: a.TrackEvent,
		MaxWorkers:        downloads.DefaultWorkers,
		TileServer:        a.tileServer,
	})
	if err != nil {
		wailsRuntime.LogError(ctx, fmt.Sprintf("Failed to initialize Google Earth downloader: %v", err))
	} else {
		a.geDownloader = geDownloaderInstance
		wailsRuntime.LogInfo(ctx, "Google Earth downloader initialized")
	}

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

			// Open download folder once after task completion (only if successful)
			if success {
				if openErr := a.OpenDownloadFolder(); openErr != nil {
					log.Printf("Failed to open download folder: %v", openErr)
				}
			}
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
	tiles, _ := esriClient.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
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

// GetEsriWaybackDatesForArea returns available Esri Wayback dates for a specific area
// Parameters bbox and zoom are currently unused but match the GetGoogleEarthDatesForArea signature
func (a *App) GetEsriWaybackDatesForArea(bbox BoundingBox, zoom int) ([]AvailableDate, error) {
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

	tile, err := esriClient.GetTileForWgs84(centerLat, centerLon, zoom)
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

// emitDownloadProgressFromDownloads is a wrapper that converts downloads.DownloadProgress to app DownloadProgress
// This is used as a callback for downloaders that work with the downloads package types
func (a *App) emitDownloadProgressFromDownloads(progress downloads.DownloadProgress) {
	a.emitDownloadProgress(DownloadProgress{
		Downloaded:  progress.Downloaded,
		Total:       progress.Total,
		Percent:     progress.Percent,
		Status:      progress.Status,
		CurrentDate: progress.CurrentDate,
		TotalDates:  progress.TotalDates,
	})
}

// findLayerForDate finds the layer matching a date
func (a *App) findLayerForDate(date string) (*esriClient.Layer, error) {
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
		log.Printf("[isBlankTile] Failed to decode image: %v", err)
		return false // Can't decode, assume it's valid
	}

	bounds := img.Bounds()
	if bounds.Dx() < 10 || bounds.Dy() < 10 {
		return true // Too small
	}

	// Sample many pixels across the image
	sampleCount := 0
	whiteCount := 0
	blackCount := 0
	totalR, totalG, totalB := uint64(0), uint64(0), uint64(0)

	// Sample a grid of points
	stepX := bounds.Dx() / 8
	stepY := bounds.Dy() / 8
	if stepX < 1 {
		stepX = 1
	}
	if stepY < 1 {
		stepY = 1
	}

	for y := bounds.Min.Y + stepY; y < bounds.Max.Y-stepY; y += stepY {
		for x := bounds.Min.X + stepX; x < bounds.Max.X-stepX; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			totalR += uint64(r)
			totalG += uint64(g)
			totalB += uint64(b)
			sampleCount++

			// Check for white (RGBA values are 0-65535)
			if r > 63000 && g > 63000 && b > 63000 {
				whiteCount++
			}
			// Check for black
			if r < 2500 && g < 2500 && b < 2500 {
				blackCount++
			}
		}
	}

	if sampleCount == 0 {
		return false
	}

	// If more than 90% of samples are white or black, it's blank
	whitePercent := (whiteCount * 100) / sampleCount
	blackPercent := (blackCount * 100) / sampleCount

	if whitePercent > 90 {
		log.Printf("[isBlankTile] Detected blank tile: %d%% white pixels", whitePercent)
		return true
	}
	if blackPercent > 90 {
		log.Printf("[isBlankTile] Detected blank tile: %d%% black pixels", blackPercent)
		return true
	}

	// Also check for very low color variance (uniform gray/beige)
	avgR := totalR / uint64(sampleCount)
	avgG := totalG / uint64(sampleCount)
	avgB := totalB / uint64(sampleCount)

	// Calculate variance
	varR, varG, varB := uint64(0), uint64(0), uint64(0)
	for y := bounds.Min.Y + stepY; y < bounds.Max.Y-stepY; y += stepY {
		for x := bounds.Min.X + stepX; x < bounds.Max.X-stepX; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			varR += absDiff64(uint64(r), avgR) * absDiff64(uint64(r), avgR)
			varG += absDiff64(uint64(g), avgG) * absDiff64(uint64(g), avgG)
			varB += absDiff64(uint64(b), avgB) * absDiff64(uint64(b), avgB)
		}
	}

	// Very low variance indicates uniform/blank image
	avgVariance := (varR + varG + varB) / (3 * uint64(sampleCount))
	// Threshold: variance of ~1000^2 = 1000000 is considered "uniform"
	if avgVariance < 2000000 {
		log.Printf("[isBlankTile] Detected blank tile: low variance %d, avg RGB: %d,%d,%d", avgVariance, avgR/257, avgG/257, avgB/257)
		return true
	}

	return false
}

// absDiff64 returns absolute difference between two uint64 values
func absDiff64(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

// tileResult holds the result of a tile download
type tileResult struct {
	tile *esriClient.EsriTile
	data []byte
	err  error
}

// DownloadEsriImagery downloads Esri Wayback imagery for a bounding box as georeferenced image
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadEsriImagery(bbox BoundingBox, zoom int, date string, format string) error {
	// Set up callbacks for the downloader
	a.esriDownloader.SetRangeDownloadState(a.inRangeDownload, a.currentDateIndex, a.totalDatesInRange)

	// Use the esri downloader (convert bbox to downloads.BoundingBox)
	err := a.esriDownloader.DownloadImagery(a.ctx, bbox.toDownloadsBBox(), zoom, date, format)
	if err != nil {
		return err
	}

	// Auto-open download folder (only if not running in task queue)
	if a.currentTaskID == "" {
		a.emitLog("Opening download folder...")
		if err := a.OpenDownloadFolder(); err != nil {
			log.Printf("Failed to open download folder: %v", err)
		}
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
	if a.geDownloader == nil {
		return fmt.Errorf("Google Earth downloader not initialized")
	}

	// Use the Google Earth downloader (convert bbox to downloads.BoundingBox)
	err := a.geDownloader.DownloadImagery(bbox.toDownloadsBBox(), zoom, format)
	if err != nil {
		return err
	}

	// Auto-open download folder (only if not running in task queue)
	if a.currentTaskID == "" {
		a.emitLog("Opening download folder...")
		if err := a.OpenDownloadFolder(); err != nil {
			log.Printf("Failed to open download folder: %v", err)
		}
	}

	return nil
}

// DownloadEsriImageryRange downloads Esri Wayback imagery for multiple dates (bulk download)
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
// This function deduplicates by checking the center tile - dates with identical imagery are skipped
func (a *App) DownloadEsriImageryRange(bbox BoundingBox, zoom int, dates []string, format string) error {
	// Use the esri downloader (convert bbox to downloads.BoundingBox)
	err := a.esriDownloader.DownloadImageryRange(a.ctx, bbox.toDownloadsBBox(), zoom, dates, format)
	if err != nil {
		return err
	}

	// Auto-open download folder (only if not running in task queue)
	if a.currentTaskID == "" {
		a.emitLog("Opening download folder...")
		if err := a.OpenDownloadFolder(); err != nil {
			log.Printf("Failed to open download folder: %v", err)
		}
	}

	return nil
}

// OpenDownloadFolder opens the download folder in the system file manager
func (a *App) OpenDownloadFolder() error {
	return a.OpenFolder(a.downloadPath)
}

// OpenFolder opens a specific folder in the OS file explorer
// On Windows, it tracks recently opened folders to avoid opening duplicate windows
func (a *App) OpenFolder(path string) error {
	// Verify the path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("folder does not exist: %s", path)
	}

	// Normalize path for consistent tracking
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	// On Windows, check if this folder was recently opened to avoid duplicate windows
	// macOS Finder handles this automatically, but Windows Explorer always opens new windows
	if goruntime.GOOS == "windows" {
		a.folderOpenMu.Lock()
		if lastOpened, exists := a.lastOpenedFolders[absPath]; exists {
			// Skip if opened within the last 30 seconds
			if time.Since(lastOpened) < 30*time.Second {
				a.folderOpenMu.Unlock()
				log.Printf("Skipping folder open (recently opened): %s", absPath)
				return nil
			}
		}
		a.lastOpenedFolders[absPath] = time.Now()
		a.folderOpenMu.Unlock()
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
// Routes through backend tile server for caching, matching Google Earth pattern
func (a *App) GetEsriTileURL(date string) (string, error) {
	if a.tileServer == nil || a.tileServer.GetTileServerURL() == "" {
		return "", fmt.Errorf("tile server not started")
	}

	// Verify the date has a valid layer (validate before returning URL)
	layers, err := a.esriClient.GetLayers()
	if err != nil {
		return "", fmt.Errorf("failed to get Esri layers: %w", err)
	}

	// Find layer matching the date to validate it exists
	found := false
	for _, layer := range layers {
		if layer.Date.Format("2006-01-02") == date {
			found = true
			break
		}
	}

	if !found {
		return "", fmt.Errorf("no layer found for date: %s", date)
	}

	// Return tile server URL that routes through backend caching proxy
	// Format: http://localhost:PORT/esri-wayback/{date}/{z}/{x}/{y}
	return fmt.Sprintf("%s/esri-wayback/%s/{z}/{x}/{y}", a.tileServer.GetTileServerURL(), date), nil
}


// GetGoogleEarthTileURL returns the tile URL template for Google Earth (for map preview)
func (a *App) GetGoogleEarthTileURL(date string) (string, error) {
	if a.tileServer == nil || a.tileServer.GetTileServerURL() == "" {
		return "", fmt.Errorf("tile server not started")
	}
	// Date must be in YYYY-MM-DD format
	return fmt.Sprintf("%s/google-earth/%s/{z}/{x}/{y}", a.tileServer.GetTileServerURL(), date), nil
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
func (a *App) GetGoogleEarthHistoricalTileURL(date string, hexDate string, epoch int) (string, error) {
	if a.tileServer == nil || a.tileServer.GetTileServerURL() == "" {
		return "", fmt.Errorf("tile server not started")
	}
	// Note: epoch parameter kept for API compatibility but not used in URL
	// Each tile looks up its own epoch from the quadtree
	// Use regular date format (YYYY-MM-DD) in URL for human-readable cache structure
	// Format: /google-earth-historical/{date}_{hexDate}/{z}/{x}/{y}
	// This allows the handler to extract both date (for caching) and hexDate (for fetching)
	return fmt.Sprintf("%s/google-earth-historical/%s_%s/{z}/{x}/{y}", a.tileServer.GetTileServerURL(), date, hexDate), nil
}

// DownloadGoogleEarthHistoricalImagery downloads historical Google Earth imagery for a bounding box
// Note: epoch parameter kept for API compatibility but the correct epoch is looked up per-tile
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadGoogleEarthHistoricalImagery(bbox BoundingBox, zoom int, hexDate string, epoch int, dateStr string, format string) error {
	if a.geDownloader == nil {
		return fmt.Errorf("Google Earth downloader not initialized")
	}

	// Use the Google Earth downloader (convert bbox to downloads.BoundingBox)
	err := a.geDownloader.DownloadHistoricalImagery(bbox.toDownloadsBBox(), zoom, hexDate, epoch, dateStr, format)
	if err != nil {
		return err
	}

	// Auto-open download folder
	a.emitLog("Opening download folder...")
	if err := a.OpenDownloadFolder(); err != nil {
		log.Printf("Failed to open download folder: %v", err)
	}

	return nil
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
	if a.geDownloader == nil {
		return fmt.Errorf("Google Earth downloader not initialized")
	}

	// Use the Google Earth downloader (convert bbox and dates to downloads types)
	err := a.geDownloader.DownloadHistoricalImageryRange(bbox.toDownloadsBBox(), zoom, convertGEDateInfoSlice(dates), format, nil)
	if err != nil {
		return err
	}

	// Auto-open download folder (only if not running in task queue)
	if a.currentTaskID == "" {
		a.emitLog("Opening download folder...")
		if err := a.OpenDownloadFolder(); err != nil {
			log.Printf("Failed to open download folder: %v", err)
		}
	}

	return nil
}

// ExportTimelapseVideo exports a timelapse video from a range of downloaded imagery
func (a *App) ExportTimelapseVideo(bbox BoundingBox, zoom int, dates []GEDateInfo, source string, videoOpts VideoExportOptions) error {
	return a.exportTimelapseVideoInternal(bbox, zoom, dates, source, videoOpts, true)
}

// exportTimelapseVideoInternal is the internal implementation with option to skip opening folder
func (a *App) exportTimelapseVideoInternal(bbox BoundingBox, zoom int, dates []GEDateInfo, source string, videoOpts VideoExportOptions, openFolder bool) error {
	// Convert app types to video package types
	videoBBox := video.BoundingBox{
		South: bbox.South,
		West:  bbox.West,
		North: bbox.North,
		East:  bbox.East,
	}

	videoDates := make([]video.DateInfo, len(dates))
	for i, d := range dates {
		videoDates[i] = video.DateInfo{
			Date:    d.Date,
			HexDate: d.HexDate,
			Epoch:   d.Epoch,
		}
	}

	videoTimelapseOpts := video.TimelapseOptions{
		Width:              videoOpts.Width,
		Height:             videoOpts.Height,
		Preset:             videoOpts.Preset,
		Presets:            videoOpts.Presets,
		CropX:              videoOpts.CropX,
		CropY:              videoOpts.CropY,
		SpotlightEnabled:   videoOpts.SpotlightEnabled,
		SpotlightCenterLat: videoOpts.SpotlightCenterLat,
		SpotlightCenterLon: videoOpts.SpotlightCenterLon,
		SpotlightRadiusKm:  videoOpts.SpotlightRadiusKm,
		OverlayOpacity:     videoOpts.OverlayOpacity,
		ShowDateOverlay:    videoOpts.ShowDateOverlay,
		DateFontSize:       videoOpts.DateFontSize,
		DatePosition:       videoOpts.DatePosition,
		ShowLogo:           videoOpts.ShowLogo,
		LogoPosition:       videoOpts.LogoPosition,
		FrameDelay:         videoOpts.FrameDelay,
		OutputFormat:       videoOpts.OutputFormat,
		Quality:            videoOpts.Quality,
	}

	// Use videoManager to export
	var err error
	if openFolder && a.currentTaskID == "" {
		err = a.videoManager.ExportTimelapse(videoBBox, zoom, videoDates, source, videoTimelapseOpts)
		// Auto-open download folder after export (only if not in task queue)
		if err == nil {
			if openErr := a.OpenDownloadFolder(); openErr != nil {
				log.Printf("Failed to open download folder: %v", openErr)
			}
		}
	} else {
		err = a.videoManager.ExportTimelapseNoOpen(videoBBox, zoom, videoDates, source, videoTimelapseOpts)
	}

	return err
}

// ReExportVideo re-exports video from a completed task with new presets
func (a *App) ReExportVideo(taskID string, presets []string, videoFormat string) error {
	log.Printf("[ReExport] Starting re-export for task %s with presets: %v, format: %s", taskID, presets, videoFormat)

	// Validate video format
	if videoFormat != "mp4" && videoFormat != "gif" {
		return fmt.Errorf("invalid video format: %s (must be 'mp4' or 'gif')", videoFormat)
	}

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

	// Convert types for video manager
	bbox := video.BoundingBox{
		South: task.BBox.South,
		West:  task.BBox.West,
		North: task.BBox.North,
		East:  task.BBox.East,
	}

	dates := make([]video.DateInfo, len(task.Dates))
	for i, d := range task.Dates {
		dates[i] = video.DateInfo{
			Date:    d.Date,
			HexDate: d.HexDate,
			Epoch:   d.Epoch,
		}
	}

	// Save original download path and update videoManager
	originalDownloadPath := a.downloadPath
	a.downloadPath = task.OutputPath
	a.videoManager.SetDownloadPath(task.OutputPath)
	defer func() {
		a.downloadPath = originalDownloadPath
		a.videoManager.SetDownloadPath(originalDownloadPath)
	}()

	// Export for each preset
	log.Printf("[ReExport] Starting export of %d preset(s): %v", len(presets), presets)
	a.emitLog(fmt.Sprintf("Re-exporting %d preset(s) as %s: %v", len(presets), videoFormat, presets))

	successCount := 0
	failedPresets := []string{}

	for i, presetID := range presets {
		log.Printf("[ReExport] Exporting preset %d/%d: %s (format: %s)", i+1, len(presets), presetID, videoFormat)

		a.emitDownloadProgress(DownloadProgress{
			Downloaded:  i,
			Total:       len(presets),
			Percent:     (i * 100) / len(presets),
			Status:      fmt.Sprintf("Exporting %s as %s (%d/%d)", presetID, videoFormat, i+1, len(presets)),
			CurrentDate: i + 1,
			TotalDates:  len(presets),
		})

		// Create video options for this preset using video manager types
		videoOpts := video.TimelapseOptions{
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

		// Use video manager for export (no folder opening)
		if err := a.videoManager.ExportTimelapseNoOpen(bbox, task.Zoom, dates, task.Source, videoOpts); err != nil {
			log.Printf("[ReExport] Failed to export preset %s: %v", presetID, err)
			a.emitLog(fmt.Sprintf("❌ Failed to export preset %s: %v", presetID, err))
			failedPresets = append(failedPresets, presetID)
			// Continue with other presets
		} else {
			successCount++
			a.emitLog(fmt.Sprintf("✅ Successfully exported preset: %s", presetID))
		}
	}

	// Open download folder once at the end (only if at least one export succeeded)
	if successCount > 0 {
		if err := a.OpenDownloadFolder(); err != nil {
			log.Printf("Failed to open download folder: %v", err)
		}
	}

	// Report final results
	if len(failedPresets) > 0 {
		a.emitLog(fmt.Sprintf("⚠️ Re-export completed with %d success(es) and %d failure(s). Failed presets: %v",
			successCount, len(failedPresets), failedPresets))
	} else {
		a.emitLog(fmt.Sprintf("✅ All %d preset(s) re-exported successfully", successCount))
	}

	a.emitDownloadProgress(DownloadProgress{
		Downloaded:  len(presets),
		Total:       len(presets),
		Percent:     100,
		Status:      fmt.Sprintf("Re-export complete (%d/%d successful)", successCount, len(presets)),
		CurrentDate: len(presets),
		TotalDates:  len(presets),
	})

	log.Printf("[ReExport] Completed re-export for task %s: %d success, %d failed", taskID, successCount, len(failedPresets))

	// Return an error if all presets failed
	if successCount == 0 {
		return fmt.Errorf("all %d preset(s) failed to export", len(presets))
	}

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

	// Update downloaders and videoManager to use task-specific path
	a.esriDownloader.SetDownloadPath(a.taskOutputPath)
	if a.geDownloader != nil {
		a.geDownloader.SetDownloadPath(a.taskOutputPath)
	}
	a.videoManager.SetDownloadPath(a.taskOutputPath)

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

		// Restore downloaders and videoManager to original path
		a.esriDownloader.SetDownloadPath(originalDownloadPath)
		if a.geDownloader != nil {
			a.geDownloader.SetDownloadPath(originalDownloadPath)
		}
		a.videoManager.SetDownloadPath(originalDownloadPath)
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
	var esriCenterTile *esriClient.EsriTile
	if task.Source == common.ProviderEsriWayback {
		esriSeenHashes = make(map[string]string)
		centerLat := (bbox.South + bbox.North) / 2
		centerLon := (bbox.West + bbox.East) / 2
		esriCenterTile, _ = esriClient.GetTileForWgs84(centerLat, centerLon, task.Zoom)
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
		case common.ProviderGoogleEarth:
			err = a.DownloadGoogleEarthHistoricalImagery(bbox, task.Zoom, dateInfo.HexDate, dateInfo.Epoch, dateInfo.Date, task.Format)
			if err == nil {
				downloadedCount++
			}
		case common.ProviderEsriWayback:
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
		a.emitLog(fmt.Sprintf("Exporting %d video preset(s): %v", len(presetsToExport), presetsToExport))

		successCount := 0
		failedPresets := []string{}

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

			// Use internal function with openFolder=false to avoid opening folder multiple times
			if err := a.exportTimelapseVideoInternal(bbox, task.Zoom, dates, task.Source, videoOpts, false); err != nil {
				log.Printf("[TaskQueue] Failed to export preset %s: %v", presetID, err)
				a.emitLog(fmt.Sprintf("❌ Failed to export preset %s: %v", presetID, err))
				failedPresets = append(failedPresets, presetID)
				// Continue with other presets, don't fail the entire task
			} else {
				successCount++
				a.emitLog(fmt.Sprintf("✅ Successfully exported preset: %s", presetID))
			}
		}

		// Note: Download folder will be opened by task completion callback

		// Report final results
		if len(failedPresets) > 0 {
			a.emitLog(fmt.Sprintf("⚠️ Export completed with %d success(es) and %d failure(s). Failed presets: %v",
				successCount, len(failedPresets), failedPresets))
		} else {
			a.emitLog(fmt.Sprintf("✅ All %d preset(s) exported successfully", successCount))
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
