package esri

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"imagery-desktop/internal/cache"
	"imagery-desktop/internal/common"
	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/esri"
	"imagery-desktop/internal/ratelimit"
	"imagery-desktop/internal/utils/naming"
	"imagery-desktop/pkg/geotiff"
)

// tileResult holds the result of a tile download
type tileResult struct {
	tile *esri.EsriTile
	data []byte
	err  error
}

// Downloader handles Esri Wayback imagery downloads
type Downloader struct {
	esriClient           *esri.Client
	tileCache            *cache.PersistentTileCache
	downloadPath         string
	progressCallback     func(downloads.DownloadProgress)
	logCallback          func(string)
	rateLimitHandler     *ratelimit.Handler
	trackEventCallback   func(string, map[string]interface{})
	maxWorkers           int
	sem                  *semaphore.Weighted

	// Range download state
	inRangeDownload      bool
	currentDateIndex     int
	totalDatesInRange    int
	mu                   sync.Mutex
}

// NewDownloader creates a new Esri downloader with injected dependencies
func NewDownloader(
	esriClient *esri.Client,
	tileCache *cache.PersistentTileCache,
	downloadPath string,
	progressCallback func(downloads.DownloadProgress),
	logCallback func(string),
	rateLimitHandler *ratelimit.Handler,
	trackEventCallback func(string, map[string]interface{}),
	maxWorkers int,
) *Downloader {
	if maxWorkers <= 0 {
		maxWorkers = downloads.DefaultWorkers
	}

	return &Downloader{
		esriClient:         esriClient,
		tileCache:          tileCache,
		downloadPath:       downloadPath,
		progressCallback:   progressCallback,
		logCallback:        logCallback,
		rateLimitHandler:   rateLimitHandler,
		trackEventCallback: trackEventCallback,
		maxWorkers:         maxWorkers,
		sem:                semaphore.NewWeighted(int64(maxWorkers)),
	}
}

// SetRangeDownloadState sets the range download state for progress tracking
func (d *Downloader) SetRangeDownloadState(inRange bool, currentIndex, totalDates int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inRangeDownload = inRange
	d.currentDateIndex = currentIndex
	d.totalDatesInRange = totalDates
}

// GetRangeDownloadState returns the current range download state
func (d *Downloader) GetRangeDownloadState() (inRange bool, currentIndex, totalDates int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inRangeDownload, d.currentDateIndex, d.totalDatesInRange
}

// emitLog emits a log message if callback is set
func (d *Downloader) emitLog(message string) {
	if d.logCallback != nil {
		d.logCallback(message)
	}
}

// emitProgress emits download progress if callback is set
func (d *Downloader) emitProgress(progress downloads.DownloadProgress) {
	if d.progressCallback != nil {
		d.progressCallback(progress)
	}
}

// trackEvent tracks an analytics event if callback is set
func (d *Downloader) trackEvent(event string, properties map[string]interface{}) {
	if d.trackEventCallback != nil {
		d.trackEventCallback(event, properties)
	}
}

// findLayerForDate finds the layer matching a date
func (d *Downloader) findLayerForDate(date string) (*esri.Layer, error) {
	layers, err := d.esriClient.GetLayers()
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
func (d *Downloader) isBlankTile(data []byte) bool {
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

// DownloadImagery downloads Esri Wayback imagery for a bounding box as georeferenced image
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (d *Downloader) DownloadImagery(ctx context.Context, bbox downloads.BoundingBox, zoom int, date string, format string) error {
	// Validate coordinates
	if err := downloads.ValidateCoordinates(bbox, zoom); err != nil {
		return fmt.Errorf("invalid coordinates: %w", err)
	}

	d.emitLog(fmt.Sprintf("Starting download for %s at zoom %d", date, zoom))

	// Find layer for this date directly (much faster than GetNearestDatedTile)
	layer, err := d.findLayerForDate(date)
	if err != nil {
		d.emitLog(fmt.Sprintf("Error: %v", err))
		return err
	}
	d.emitLog(fmt.Sprintf("Found layer ID %d for date %s", layer.ID, date))

	// Get tiles
	tiles, err := esri.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	if err != nil {
		return err
	}

	total := len(tiles)
	if total == 0 {
		return fmt.Errorf("no tiles in bounding box")
	}
	d.emitLog(fmt.Sprintf("Downloading %d tiles with %d workers...", total, d.maxWorkers))

	// Download tiles concurrently with semaphore-based worker pool
	var downloaded int64
	tileChan := make(chan *esri.EsriTile, total)
	resultChan := make(chan tileResult, total)
	errorChan := make(chan error, total)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < d.maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tile := range tileChan {
				// Acquire semaphore
				if err := d.sem.Acquire(ctx, 1); err != nil {
					errorChan <- err
					continue
				}

				var data []byte
				var err error

				// Check cache first
				if d.tileCache != nil {
					cacheKey := fmt.Sprintf("%s:%d:%d:%d:%s", common.ProviderEsriWayback, zoom, tile.Column, tile.Row, date)
					var found bool
					data, found = d.tileCache.Get(cacheKey)
					if found {
						log.Printf("[Cache HIT] Esri tile z=%d x=%d y=%d (date: %s)", zoom, tile.Column, tile.Row, date)
						d.sem.Release(1)
						resultChan <- tileResult{tile: tile, data: data, err: nil}
						continue
					}
				}

				// Fetch from network if not cached
				data, err = d.esriClient.FetchTile(layer, tile)

				// Release semaphore
				d.sem.Release(1)

				// Cache the result if successful
				if err == nil && d.tileCache != nil {
					d.tileCache.Set(common.ProviderEsriWayback, zoom, tile.Column, tile.Row, date, data)
				}

				resultChan <- tileResult{tile: tile, data: data, err: err}
			}
		}()
	}

	// Send tiles to workers
	go func() {
		for _, tile := range tiles {
			select {
			case <-ctx.Done():
				close(tileChan)
				return
			case tileChan <- tile:
			}
		}
		close(tileChan)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	// Find tile bounds for stitching
	// Convert to common.Tile interface slice
	commonTiles := make([]common.Tile, len(tiles))
	for i, t := range tiles {
		commonTiles[i] = t
	}
	bounds, err := common.CalculateTileBounds(commonTiles)
	if err != nil {
		return fmt.Errorf("failed to calculate tile bounds: %w", err)
	}
	cols := bounds.Cols()
	rows := bounds.Rows()
	d.emitLog(fmt.Sprintf("Grid: %d cols x %d rows", cols, rows))

	// Create output image only if we need GeoTIFF
	var outputImg *image.RGBA
	var outputWidth, outputHeight int
	if format == "geotiff" || format == "both" {
		outputWidth = cols * downloads.TileSize
		outputHeight = rows * downloads.TileSize
		outputImg = image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	}

	// Create tiles directory if saving individual tiles (OGC structure: source_date_z{zoom}_tiles/{z}/{x}/{y}.jpg)
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(d.downloadPath, naming.GenerateTilesDirName("esri", date, zoom))
		if err := os.MkdirAll(tilesDir, 0755); err != nil {
			return fmt.Errorf("failed to create tiles directory: %w", err)
		}
	}

	// Get range download state
	inRangeDownload, currentDateIndex, totalDatesInRange := d.GetRangeDownloadState()

	// Process results and stitch tiles
	successCount := 0
	var errors []error
	for result := range resultChan {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		count := atomic.AddInt64(&downloaded, 1)

		// Emit progress with clear status based on format
		percent := int((count * 100) / int64(total))
		var status string

		// If in range download mode, include date context
		if inRangeDownload {
			dateProgress := fmt.Sprintf("Date %d/%d", currentDateIndex, totalDatesInRange)
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

		d.emitProgress(downloads.DownloadProgress{
			Downloaded:  int(count),
			Total:       total,
			Percent:     percent,
			Status:      status,
			CurrentDate: currentDateIndex,
			TotalDates:  totalDatesInRange,
		})

		if result.err != nil {
			// Collect errors instead of just logging
			errors = append(errors, result.err)
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
			xOff := (result.tile.Column - bounds.MinCol) * downloads.TileSize
			yOff := (result.tile.Row - bounds.MinRow) * downloads.TileSize

			// Draw tile onto output image
			draw.Draw(outputImg, image.Rect(xOff, yOff, xOff+downloads.TileSize, yOff+downloads.TileSize), img, image.Point{0, 0}, draw.Src)
		}
		successCount++
	}

	// Check for errors from error channel
	for err := range errorChan {
		if err != nil {
			errors = append(errors, err)
		}
	}

	d.emitLog(fmt.Sprintf("Processed %d/%d tiles", successCount, total))

	// Track download completion
	d.trackEvent("download_complete", map[string]interface{}{
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
		originX, originY := esri.TileToWebMercator(bounds.MinCol, bounds.MinRow, zoom)
		endX, endY := esri.TileToWebMercator(bounds.MaxCol+1, bounds.MaxRow+1, zoom)
		pixelWidth := (endX - originX) / float64(outputWidth)
		pixelHeight := (originY - endY) / float64(outputHeight)

		// Save as GeoTIFF with embedded projection and rich metadata
		tifPath := filepath.Join(d.downloadPath, naming.GenerateGeoTIFFFilename("esri", date, bbox.South, bbox.West, bbox.North, bbox.East, zoom))

		// Emit progress for GeoTIFF encoding phase
		d.emitProgress(downloads.DownloadProgress{
			Downloaded: total,
			Total:      total,
			Percent:    99,
			Status:     "Encoding GeoTIFF file...",
		})
		d.emitLog("Encoding GeoTIFF file...")
		if err := d.saveAsGeoTIFFWithMetadata(outputImg, tifPath, originX, originY, pixelWidth, pixelHeight, "Esri Wayback", date); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}

		d.emitLog(fmt.Sprintf("Saved: %s", tifPath))

		// Save PNG copy for video export compatibility
		d.savePNGCopy(outputImg, tifPath)
	}

	if format == "tiles" || format == "both" {
		d.emitLog(fmt.Sprintf("Tiles saved to: %s", tilesDir))
	}

	// Emit completion
	d.emitProgress(downloads.DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     "Complete",
	})

	// Return first error if any
	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during download, first: %w", len(errors), errors[0])
	}

	return nil
}

// saveAsGeoTIFFWithMetadata saves an image as a georeferenced TIFF with full metadata
func (d *Downloader) saveAsGeoTIFFWithMetadata(img image.Image, outputPath string, originX, originY, pixelWidth, pixelHeight float64, source, date string) error {
	// Create TIFF file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// Define GeoKeys (EPSG:3857 Web Mercator)
	extraTags := make(map[uint16]interface{})

	// GeoTIFF tags for Web Mercator (EPSG:3857)
	// ModelTiepoint: [I, J, K, X, Y, Z] - ties image coordinate (0,0,0) to world coordinate
	modelTiepoint := []float64{
		0, 0, 0,         // Raster point (I, J, K)
		originX, originY, 0, // World point (X, Y, Z)
	}
	extraTags[33922] = modelTiepoint // ModelTiepointTag

	// ModelPixelScale: [ScaleX, ScaleY, ScaleZ] - pixel size in world units
	modelPixelScale := []float64{
		pixelWidth,
		pixelHeight, // Note: negative in some systems, but positive here as Y increases downward in image
		0,
	}
	extraTags[33550] = modelPixelScale // ModelPixelScaleTag

	// GeoKeyDirectory for EPSG:3857 (Web Mercator)
	// Format: [KeyDirectoryVersion, KeyRevision, MinorRevision, NumberOfKeys, ...]
	geoKeyDirectory := []uint16{
		1, 1, 0, 3, // Header: version 1.1.0, 3 keys follow
		// Each key entry: [KeyID, TIFFTagLocation, Count, Value_Offset]
		1024, 0, 1, 1, // GTModelTypeGeoKey = ModelTypeProjected (1)
		3072, 0, 1, 3857, // ProjectedCSTypeGeoKey = EPSG 3857 (Web Mercator)
		3076, 0, 1, 9001, // ProjLinearUnitsGeoKey = Linear_Meter (9001)
	}
	extraTags[34735] = geoKeyDirectory // GeoKeyDirectoryTag

	// Add metadata tags
	if source != "" {
		extraTags[270] = source // ImageDescription
	}
	if date != "" {
		extraTags[306] = date // DateTime
	}

	// Write GeoTIFF with metadata
	if err := geotiff.Encode(f, img, extraTags); err != nil {
		return fmt.Errorf("failed to encode GeoTIFF: %w", err)
	}

	return nil
}

// savePNGCopy saves a PNG copy of an image alongside its GeoTIFF for video export compatibility
// GeoTIFF files with custom geo tags may not decode properly with standard image decoders,
// so we create a PNG sidecar that video export can reliably use
func (d *Downloader) savePNGCopy(img image.Image, tifPath string) {
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
	d.emitLog(fmt.Sprintf("Saved PNG copy: %s", filepath.Base(pngPath)))
}
