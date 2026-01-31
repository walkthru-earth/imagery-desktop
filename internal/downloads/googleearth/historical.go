package googleearth

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"imagery-desktop/internal/common"
	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/googleearth"
	"imagery-desktop/internal/utils/naming"
	"imagery-desktop/pkg/geotiff"
)

// DownloadHistoricalImagery downloads historical Google Earth imagery for a specific date
// It uses a 3-layer epoch fallback strategy:
// 1. Try the protobuf-reported epoch for the exact date
// 2. Fall back to other epochs from the same tile (sorted by frequency)
// 3. Try known-good epochs (358, 357, 356, 354, 352) for 2025+ dates
//
// Additionally, it supports zoom fallback - if tiles don't exist at the requested zoom,
// it will try lower zoom levels and extract/upscale the correct quadrant.
//
// Parameters:
//   - bbox: Geographic bounding box
//   - zoom: Zoom level (10-21 for Google Earth)
//   - hexDate: Hex date string for Google API tile fetching
//   - epoch: Primary epoch to try (from protobuf)
//   - dateStr: Human-readable date (YYYY-MM-DD) for cache and filenames
//   - format: "tiles", "geotiff", or "both"
func (d *Downloader) DownloadHistoricalImagery(bbox downloads.BoundingBox, zoom int, hexDate string, epoch int, dateStr string, format string) error {
	d.emitLog(fmt.Sprintf("Starting Google Earth historical download for %s...", dateStr))

	// Validate request
	if err := d.validateDownloadRequest(bbox, zoom, format); err != nil {
		return err
	}

	// Validate historical-specific parameters
	if hexDate == "" {
		return fmt.Errorf("hexDate is required for historical downloads")
	}
	if dateStr == "" {
		return fmt.Errorf("dateStr is required for historical downloads")
	}

	// Get tiles using Google Earth coordinate system
	tiles, err := googleearth.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	if err != nil {
		return fmt.Errorf("failed to get tiles in bounds: %w", err)
	}

	total := len(tiles)
	if total == 0 {
		return fmt.Errorf("no tiles in bounding box")
	}
	d.emitLog(fmt.Sprintf("Downloading %d tiles...", total))

	// Calculate tile bounds for stitching
	bounds, err := calculateTileBounds(tiles)
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
		outputImg = createOutputImage(outputWidth, outputHeight)
	}

	// Create tiles directory if saving individual tiles (OGC structure)
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(d.downloadPath, naming.GenerateTilesDirName(common.ProviderGoogleEarth, dateStr, zoom))
		if err := os.MkdirAll(tilesDir, 0755); err != nil {
			return fmt.Errorf("failed to create tiles directory: %w", err)
		}
	}

	// Download tiles concurrently with semaphore control and zoom fallback
	ctx := context.Background()
	successCount := 0
	errors := make(chan error, total)

	// Create channels for work distribution
	jobChan := make(chan TileJob, total)
	resultChan := make(chan tileResult, total)

	// Start workers
	numWorkers := int(d.maxWorkers)
	if total < numWorkers {
		numWorkers = total
	}

	for w := 0; w < numWorkers; w++ {
		go func() {
			for job := range jobChan {
				// Acquire semaphore
				if err := d.acquireWorker(ctx); err != nil {
					resultChan <- tileResult{tile: job.tile, index: job.index, success: false, err: err}
					continue
				}

				// Try with zoom fallback using the tile server's epoch fallback logic
				// The tile server implements the 3-layer epoch fallback strategy:
				// 1. Protobuf-reported epoch
				// 2. Other epochs from the same tile (by frequency)
				// 3. Known-good epochs for 2025+ dates
				maxFallback := 3
				if zoom < 17 {
					maxFallback = 6 // More aggressive fallback for lower zooms
				}

				data, actualZoom, err := d.tileServer.FetchHistoricalGETileWithZoomFallback(
					job.tile,
					dateStr,
					hexDate,
					maxFallback,
				)
				d.releaseWorker()

				if err != nil {
					log.Printf("[GEHistorical] Failed to download tile %s (tried zoom %d to %d): %v",
						job.tile.Path, zoom, max(zoom-maxFallback, 10), err)
					resultChan <- tileResult{tile: job.tile, index: job.index, success: false, err: err}
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

	// Send jobs to workers
	go func() {
		for i, tile := range tiles {
			jobChan <- TileJob{tile: tile, index: i}
		}
		close(jobChan)
	}()

	// Collect results and process tiles
	processedCount := 0
	for processedCount < total {
		result := <-resultChan
		processedCount++

		// Emit progress with clear status based on format
		var status string
		if format == "geotiff" || format == "both" {
			status = fmt.Sprintf("Downloading and merging tile %d/%d", processedCount, total)
		} else {
			status = fmt.Sprintf("Downloading tile %d/%d", processedCount, total)
		}
		d.emitProgress(downloads.DownloadProgress{
			Downloaded: processedCount,
			Total:      total,
			Percent:    (processedCount * 100) / total,
			Status:     status,
		})

		if !result.success {
			errors <- result.err
			continue
		}

		// Save individual tile if requested (OGC structure: source/date/z/x/y.jpg)
		if format == "tiles" || format == "both" {
			if err := d.saveTile(tilesDir, "google_earth_historical", dateStr, zoom, result.tile, result.data); err != nil {
				log.Printf("Failed to save tile: %v", err)
			}
		}

		// Decode and stitch for GeoTIFF
		if format == "geotiff" || format == "both" {
			if err := d.stitchTile(outputImg, result.tile, result.data, bounds); err != nil {
				log.Printf("[GEHistorical] Failed to decode tile %s: %v", result.tile.Path, err)
				continue
			}
		}
		successCount++
	}
	close(errors)

	d.emitLog(fmt.Sprintf("Processed %d/%d tiles", successCount, total))

	// Check if we have enough tiles
	if err := checkSuccessRate(successCount, total); err != nil {
		d.emitLog(fmt.Sprintf("Warning: %v - GeoTIFF may have gaps", err))
	}

	// Track download completion
	d.trackEvent("download_complete", map[string]interface{}{
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
		if err := d.saveHistoricalGeoTIFF(outputImg, bbox, zoom, bounds, dateStr, outputWidth, outputHeight); err != nil {
			return fmt.Errorf("failed to save GeoTIFF: %w", err)
		}
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

	return nil
}

// saveHistoricalGeoTIFF saves the stitched historical image as a GeoTIFF with metadata
func (d *Downloader) saveHistoricalGeoTIFF(outputImg *image.RGBA, bbox downloads.BoundingBox, zoom int, bounds TileBounds, dateStr string, outputWidth, outputHeight int) error {
	// Calculate georeferencing in Web Mercator (EPSG:3857)
	// After Y-inversion, image top-left corresponds to (bounds.MinCol, bounds.MaxRow+1) in GE coords
	// Image bottom-right corresponds to (bounds.MaxCol+1, bounds.MinRow)
	originX, originY := googleearth.TileToWebMercator(bounds.MaxRow+1, bounds.MinCol, zoom)
	endX, endY := googleearth.TileToWebMercator(bounds.MinRow, bounds.MaxCol+1, zoom)
	pixelWidth := (endX - originX) / float64(outputWidth)
	pixelHeight := (endY - originY) / float64(outputHeight) // Will be negative (Y decreases going down)

	// Generate GeoTIFF filename
	tifPath := filepath.Join(d.downloadPath, naming.GenerateGeoTIFFFilename(common.ProviderGoogleEarth, dateStr, bbox.South, bbox.West, bbox.North, bbox.East, zoom))

	// Emit progress for GeoTIFF encoding phase
	d.emitProgress(downloads.DownloadProgress{
		Percent: 99,
		Status:  "Encoding GeoTIFF file...",
	})
	d.emitLog("Encoding GeoTIFF file...")

	// Save as GeoTIFF with embedded projection and metadata
	if err := geotiff.SaveAsGeoTIFFWithMetadata(
		outputImg,
		tifPath,
		originX,
		originY,
		pixelWidth,
		pixelHeight,
		"Google Earth Historical",
		dateStr,
		"", // appVersion - not available in downloader context
	); err != nil {
		return fmt.Errorf("failed to save GeoTIFF: %w", err)
	}

	d.emitLog(fmt.Sprintf("Saved: %s", tifPath))

	// Save PNG copy for video export compatibility
	pngPath := tifPath[:len(tifPath)-4] + ".png"
	if err := saveHistoricalPNGCopy(outputImg, pngPath); err != nil {
		log.Printf("Warning: Failed to save PNG copy: %v", err)
	}

	return nil
}

// saveHistoricalPNGCopy saves a PNG copy of the historical image for video export
func saveHistoricalPNGCopy(img *image.RGBA, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Use standard PNG encoding
	encoder := &png.Encoder{
		CompressionLevel: png.DefaultCompression,
	}

	return encoder.Encode(f, img)
}
