package googleearth

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
	"time"

	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/googleearth"
	"imagery-desktop/internal/utils/naming"
	"imagery-desktop/pkg/geotiff"
)

// DownloadImagery downloads current Google Earth imagery for a bounding box
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
func (d *Downloader) DownloadImagery(bbox downloads.BoundingBox, zoom int, format string) error {
	d.emitLog("Starting Google Earth download...")

	// Validate request
	if err := d.validateDownloadRequest(bbox, zoom, format); err != nil {
		return err
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
	timestamp := time.Now().Format("2006-01-02")
	var tilesDir string
	if format == "tiles" || format == "both" {
		tilesDir = filepath.Join(d.downloadPath, naming.GenerateTilesDirName("ge", timestamp, zoom))
		if err := os.MkdirAll(tilesDir, 0755); err != nil {
			return fmt.Errorf("failed to create tiles directory: %w", err)
		}
	}

	// Download and stitch tiles with semaphore-based concurrency
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

				// Download tile
				data, err := d.geClient.FetchTile(job.tile)
				d.releaseWorker()

				if err != nil {
					d.emitLog(fmt.Sprintf("[GEDownload] Failed to download tile %s: %v", job.tile.Path, err))
					resultChan <- tileResult{tile: job.tile, index: job.index, success: false, err: err}
					continue
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
			if err := d.saveTile(tilesDir, "google_earth", timestamp, zoom, result.tile, result.data); err != nil {
				log.Printf("Failed to save tile: %v", err)
			}
		}

		// Decode and stitch for GeoTIFF
		if format == "geotiff" || format == "both" {
			if err := d.stitchTile(outputImg, result.tile, result.data, bounds); err != nil {
				d.emitLog(fmt.Sprintf("[GEDownload] Failed to decode tile %s: %v", result.tile.Path, err))
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
		"source":  "google_earth",
		"zoom":    zoom,
		"total":   total,
		"success": successCount,
		"failed":  total - successCount,
		"format":  format,
	})

	// Save GeoTIFF if requested
	if format == "geotiff" || format == "both" {
		if err := d.saveGeoTIFF(outputImg, bbox, zoom, bounds, timestamp, outputWidth, outputHeight); err != nil {
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

// saveTile saves an individual tile to disk in OGC structure
func (d *Downloader) saveTile(tilesDir, source, date string, zoom int, tile *googleearth.Tile, data []byte) error {
	// Create directory structure: source/date/z/x/
	sourceDir := filepath.Join(tilesDir, source, date)
	zDir := filepath.Join(sourceDir, fmt.Sprintf("%d", zoom))
	xDir := filepath.Join(zDir, fmt.Sprintf("%d", tile.Column))

	if err := os.MkdirAll(xDir, 0755); err != nil {
		return fmt.Errorf("failed to create tile directories: %w", err)
	}

	tilePath := filepath.Join(xDir, fmt.Sprintf("%d.jpg", tile.Row))
	if err := os.WriteFile(tilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tile file: %w", err)
	}

	return nil
}

// stitchTile decodes a tile and draws it onto the output image
func (d *Downloader) stitchTile(outputImg *image.RGBA, tile *googleearth.Tile, data []byte, bounds TileBounds) error {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to decode tile: %w", err)
	}

	// Calculate position in output image
	// GE rows increase from south to north, but image Y=0 is at top
	// So we need to invert: higher row numbers go to lower Y positions
	xOff := (tile.Column - bounds.MinCol) * downloads.TileSize
	yOff := (bounds.MaxRow - tile.Row) * downloads.TileSize

	// Draw tile onto output image
	draw.Draw(outputImg, image.Rect(xOff, yOff, xOff+downloads.TileSize, yOff+downloads.TileSize), img, image.Point{0, 0}, draw.Src)

	return nil
}

// saveGeoTIFF saves the stitched image as a GeoTIFF with metadata
func (d *Downloader) saveGeoTIFF(outputImg *image.RGBA, bbox downloads.BoundingBox, zoom int, bounds TileBounds, timestamp string, outputWidth, outputHeight int) error {
	// Calculate georeferencing in Web Mercator (EPSG:3857)
	// After Y-inversion, image top-left corresponds to (bounds.MinCol, bounds.MaxRow+1) in GE coords
	// Image bottom-right corresponds to (bounds.MaxCol+1, bounds.MinRow)
	originX, originY := googleearth.TileToWebMercator(bounds.MaxRow+1, bounds.MinCol, zoom)
	endX, endY := googleearth.TileToWebMercator(bounds.MinRow, bounds.MaxCol+1, zoom)
	pixelWidth := (endX - originX) / float64(outputWidth)
	pixelHeight := (endY - originY) / float64(outputHeight) // Will be negative (Y decreases going down)

	// Generate GeoTIFF filename
	tifPath := filepath.Join(d.downloadPath, naming.GenerateGeoTIFFFilename("ge", timestamp, bbox.South, bbox.West, bbox.North, bbox.East, zoom))

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
		"Google Earth",
		timestamp,
		"", // appVersion - not available in downloader context
	); err != nil {
		return fmt.Errorf("failed to save GeoTIFF: %w", err)
	}

	d.emitLog(fmt.Sprintf("Saved: %s", tifPath))

	// Save PNG copy for video export compatibility
	pngPath := tifPath[:len(tifPath)-4] + ".png"
	if err := savePNGCopy(outputImg, pngPath); err != nil {
		log.Printf("Warning: Failed to save PNG copy: %v", err)
	}

	return nil
}

// savePNGCopy saves a PNG copy of the image for video export
func savePNGCopy(img *image.RGBA, path string) error {
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
