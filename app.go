package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
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
)

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
	Downloaded int    `json:"downloaded"`
	Total      int    `json:"total"`
	Percent    int    `json:"percent"`
	Status     string `json:"status"`
}

// App struct
type App struct {
	ctx           context.Context
	geClient      *googleearth.Client
	esriClient    *esri.Client
	tileCache     *cache.TileCache
	downloader    *imagery.TileDownloader
	downloadPath  string
	tileServerURL string
	settings      *config.UserSettings
	mu            sync.Mutex
	devMode       bool // Enable verbose logging in dev mode only
	phClient      posthog.Client
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

	return &App{
		geClient:     googleearth.NewClient(),
		esriClient:   esri.NewClient(),
		tileCache:    tileCache,
		downloader:   downloader,
		downloadPath: settings.DownloadPath,
		settings:     settings,
		phClient:     phClient,
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

	// Create tiles directory if saving individual tiles
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(a.downloadPath, fmt.Sprintf("esri_%s_z%d_tiles", date, zoom))
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
		if format == "geotiff" || format == "both" {
			status = fmt.Sprintf("Downloading and merging %d/%d tiles", count, total)
		} else {
			status = fmt.Sprintf("Downloading %d/%d tiles", count, total)
		}
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: int(count),
			Total:      total,
			Percent:    percent,
			Status:     status,
		})

		if result.err != nil {
			// Only log unique errors to avoid spam
			// a.TrackEvent("tile_download_error", map[string]interface{}{"source": "esri", "error": result.err.Error()})
			continue
		}

		// Save individual tile if requested
		if format == "tiles" || format == "both" {
			tilePath := filepath.Join(tilesDir, fmt.Sprintf("tile_%d_%d.jpg", result.tile.Column, result.tile.Row))
			if err := os.WriteFile(tilePath, result.data, 0644); err != nil {
				log.Printf("Failed to save tile: %v", err)
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

		// Save as GeoTIFF with embedded projection (pure Go, no GDAL)
		tifPath := filepath.Join(a.downloadPath, fmt.Sprintf("esri_%s_z%d.tif", date, zoom))

		// Emit progress for GeoTIFF encoding phase
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		a.emitLog("Encoding GeoTIFF file...")
		if err := a.saveAsGeoTIFF(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		a.emitLog(fmt.Sprintf("Saved: %s", tifPath))
	}

	if format == "tiles" || format == "both" {
		a.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
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

// saveAsGeoTIFF saves an image as a georeferenced TIFF using world file and projection file
// World files are universally supported and the most reliable georeferencing method
// saveAsGeoTIFF saves an image as a georeferenced TIFF with embedded tags (EPSG:3857)
func (a *App) saveAsGeoTIFF(img image.Image, outputPath string, originX, originY, pixelWidth, pixelHeight float64) error {
	// Create TIFF file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// Define GeoKeys (EPSG:3857 Web Mercator)
	extraTags := make(map[uint16]interface{})

	// Tag 34735: GeoKeyDirectoryTag (SHORT)
	// Version=1, UPDATE=1, Minor=0, Keys=3
	// 1024 (GTModelType) = 1 (Projected)
	// 1025 (GTRasterType) = 1 (PixelIsArea)
	// 3072 (ProjectedCSType) = 3857 (Web Mercator)
	extraTags[geotiff.TagType_GeoKeyDirectoryTag] = []uint16{
		1, 1, 0, 3,
		1024, 0, 1, 1,
		1025, 0, 1, 1,
		3072, 0, 1, 3857,
	}

	// Tag 33550: ModelPixelScaleTag (DOUBLE)
	// ScaleX, ScaleY, ScaleZ (0)
	// Note: ScaleY is positive magnitude. Standard GeoTIFF assumes Y increases upwards in model space
	// but downwards in raster space, which Tiepoint handles or implied standard.
	extraTags[geotiff.TagType_ModelPixelScaleTag] = []float64{pixelWidth, pixelHeight, 0.0}

	// Tag 33922: ModelTiepointTag (DOUBLE)
	// I, J, K (Raster coords), X, Y, Z (Model coords)
	// Map (0,0) pixel to (originX, originY)
	extraTags[geotiff.TagType_ModelTiepointTag] = []float64{0.0, 0.0, 0.0, originX, originY, 0.0}

	// Encode as GeoTIFF
	if err := geotiff.Encode(f, img, extraTags); err != nil {
		return fmt.Errorf("failed to encode GeoTIFF: %w", err)
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

	// Create tiles directory if saving individual tiles
	timestamp := time.Now().Format("20060102_150405")
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(a.downloadPath, fmt.Sprintf("ge_%s_z%d_tiles", timestamp, zoom))
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
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: i,
			Total:      total,
			Percent:    (i * 100) / total,
			Status:     status,
		})

		// Download tile
		data, err := a.geClient.FetchTile(tile)
		if err != nil {
			// Only log in dev mode
			a.emitLog(fmt.Sprintf("[GEDownload] Failed to download tile %s: %v", tile.Path, err))
			continue
		}

		// Save individual tile if requested
		if format == "tiles" || format == "both" {
			tilePath := filepath.Join(tilesDir, fmt.Sprintf("tile_%d_%d.jpg", tile.Column, tile.Row))
			if err := os.WriteFile(tilePath, data, 0644); err != nil {
				a.emitLog(fmt.Sprintf("Failed to save tile: %v", err))
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

		// Save as GeoTIFF with embedded projection
		tifPath := filepath.Join(a.downloadPath, fmt.Sprintf("ge_%s_z%d.tif", timestamp, zoom))

		// Emit progress for GeoTIFF encoding phase
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		a.emitLog("Encoding GeoTIFF file...")
		if err := a.saveAsGeoTIFF(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		a.emitLog(fmt.Sprintf("Saved: %s", tifPath))
	}

	if format == "tiles" || format == "both" {
		a.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
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

	total := len(dates)
	for i, date := range dates {
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: i,
			Total:      total,
			Percent:    (i * 100) / total,
			Status:     fmt.Sprintf("Checking date %d/%d: %s", i+1, total, date),
		})

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
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: i,
			Total:      total,
			Percent:    (i * 100) / total,
			Status:     fmt.Sprintf("Downloading unique date %d/%d: %s", downloadedCount+1, total, date),
		})

		if err := a.DownloadEsriImagery(bbox, zoom, date, format); err != nil {
			a.emitLog(fmt.Sprintf("Failed to download %s: %v", date, err))
		} else {
			downloadedCount++
		}
	}

	// Emit completion
	wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
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

	// Create tiles directory if saving individual tiles
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(a.downloadPath, fmt.Sprintf("ge_%s_z%d_tiles", dateStr, zoom))
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
				data, err := a.fetchHistoricalGETile(job.tile, hexDate)
				if err != nil {
					log.Printf("[GEHistorical] Failed to download tile %s: %v", job.tile.Path, err)
					resultChan <- tileResult{tile: job.tile, index: job.index, success: false}
					continue
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
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: processedCount,
			Total:      total,
			Percent:    (processedCount * 100) / total,
			Status:     status,
		})

		if !result.success {
			continue
		}

		// Save individual tile if requested
		if format == "tiles" || format == "both" {
			tilePath := filepath.Join(tilesDir, fmt.Sprintf("tile_%d_%d.jpg", result.tile.Column, result.tile.Row))
			if err := os.WriteFile(tilePath, result.data, 0644); err != nil {
				log.Printf("Failed to save tile: %v", err)
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

	// Save GeoTIFF if requested
	if format == "geotiff" || format == "both" {
		// Calculate georeferencing in Web Mercator (EPSG:3857)
		// After Y-inversion, image top-left corresponds to (minCol, maxRow+1) in GE coords
		// Image bottom-right corresponds to (maxCol+1, minRow)
		originX, originY := googleearth.TileToWebMercator(maxRow+1, minCol, zoom)
		endX, endY := googleearth.TileToWebMercator(minRow, maxCol+1, zoom)
		pixelWidth := (endX - originX) / float64(outputWidth)
		pixelHeight := (endY - originY) / float64(outputHeight) // Will be negative (Y decreases going down)

		// Save as GeoTIFF with embedded projection
		tifPath := filepath.Join(a.downloadPath, fmt.Sprintf("ge_%s_z%d.tif", dateStr, zoom))

		// Emit progress for GeoTIFF encoding phase
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		a.emitLog("Encoding GeoTIFF file...")
		if err := a.saveAsGeoTIFF(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		a.emitLog(fmt.Sprintf("Saved: %s", tifPath))
	}

	if format == "tiles" || format == "both" {
		a.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
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

// DownloadGoogleEarthHistoricalImageryRange downloads multiple historical Google Earth imagery dates
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (a *App) DownloadGoogleEarthHistoricalImageryRange(bbox BoundingBox, zoom int, dates []GEDateInfo, format string) error {
	if len(dates) == 0 {
		return fmt.Errorf("no dates provided")
	}

	a.emitLog(fmt.Sprintf("Starting bulk download for %d Google Earth dates", len(dates)))

	total := len(dates)
	for i, dateInfo := range dates {
		wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
			Downloaded: i,
			Total:      total,
			Percent:    (i * 100) / total,
			Status:     fmt.Sprintf("Processing date %d/%d: %s", i+1, total, dateInfo.Date),
		})

		if err := a.DownloadGoogleEarthHistoricalImagery(bbox, zoom, dateInfo.HexDate, dateInfo.Epoch, dateInfo.Date, format); err != nil {
			a.emitLog(fmt.Sprintf("Failed to download %s: %v", dateInfo.Date, err))
		}
	}

	// Emit completion
	wailsRuntime.EventsEmit(a.ctx, "download-progress", DownloadProgress{
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
