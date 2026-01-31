package esri

import (
	"context"
	"fmt"
	"sort"

	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/esri"
)

// DownloadImageryRange downloads Esri Wayback imagery for multiple dates (bulk download)
// format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
// This function deduplicates by checking the center tile - dates with identical imagery are skipped
func (d *Downloader) DownloadImageryRange(ctx context.Context, bbox downloads.BoundingBox, zoom int, dates []string, format string) error {
	if len(dates) == 0 {
		return fmt.Errorf("no dates provided")
	}

	// Validate coordinates
	if err := downloads.ValidateCoordinates(bbox, zoom); err != nil {
		return fmt.Errorf("invalid coordinates: %w", err)
	}

	d.emitLog(fmt.Sprintf("Starting bulk download for %d dates (with deduplication)", len(dates)))

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
	d.SetRangeDownloadState(true, 0, len(dates))
	defer func() {
		d.SetRangeDownloadState(false, 0, 0)
	}()

	total := len(dates)
	for i, date := range dates {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		d.SetRangeDownloadState(true, i+1, total)

		// Find layer for this date
		layer, err := d.findLayerForDate(date)
		if err != nil {
			d.emitLog(fmt.Sprintf("Skipping %s: %v", date, err))
			skippedCount++
			continue
		}

		// Fetch center tile to check for duplicates
		tileData, err := d.esriClient.FetchTile(layer, centerTile)
		if err != nil || len(tileData) == 0 {
			d.emitLog(fmt.Sprintf("Skipping %s: no tile data available", date))
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
			d.emitLog(fmt.Sprintf("Skipping %s: identical to %s", date, firstDate))
			skippedCount++
			continue
		}
		seenHashes[hashKey] = date

		// Download this unique date
		if err := d.DownloadImagery(ctx, bbox, zoom, date, format); err != nil {
			d.emitLog(fmt.Sprintf("Failed to download %s: %v", date, err))
		} else {
			downloadedCount++
		}
	}

	// Emit completion
	d.emitProgress(downloads.DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     fmt.Sprintf("Downloaded %d unique dates (skipped %d duplicates)", downloadedCount, skippedCount),
	})

	d.emitLog(fmt.Sprintf("Bulk download complete: %d unique, %d skipped", downloadedCount, skippedCount))

	return nil
}
