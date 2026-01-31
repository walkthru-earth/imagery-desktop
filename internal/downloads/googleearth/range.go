package googleearth

import (
	"fmt"
	"log"

	"imagery-desktop/internal/downloads"
	"imagery-desktop/internal/googleearth"
)

// Type alias for downloads package type (used for Google Earth range downloads)
type GEDateInfo = downloads.GEDateInfo

// DownloadHistoricalImageryRange downloads multiple historical Google Earth imagery dates
// This is a bulk download operation that processes multiple dates sequentially.
// Each date is downloaded using DownloadHistoricalImagery which implements:
//   - 3-layer epoch fallback strategy
//   - Zoom fallback with quadrant extraction
//   - Concurrent tile downloads with semaphore control
//
// Parameters:
//   - bbox: Geographic bounding box
//   - zoom: Zoom level (10-21 for Google Earth)
//   - dates: List of dates to download (each with date, hexDate, and epoch)
//   - format: "tiles", "geotiff", or "both"
//   - rangeTracker: Optional progress tracker for range downloads (can be nil)
func (d *Downloader) DownloadHistoricalImageryRange(
	bbox downloads.BoundingBox,
	zoom int,
	dates []GEDateInfo,
	format string,
	rangeTracker *downloads.RangeTracker,
) error {
	if len(dates) == 0 {
		return fmt.Errorf("no dates provided")
	}

	d.emitLog(fmt.Sprintf("Starting bulk download for %d Google Earth dates", len(dates)))

	// Validate the request once before processing all dates
	if err := d.validateDownloadRequest(bbox, zoom, format); err != nil {
		return fmt.Errorf("invalid download request: %w", err)
	}

	// Track successful and failed downloads
	var successfulDates []string
	var failedDates []string
	errors := make([]error, 0)

	total := len(dates)
	for i, dateInfo := range dates {
		currentIndex := i + 1

		// Update range tracker if provided
		if rangeTracker != nil {
			rangeTracker.SetCurrentDate(currentIndex)
		}

		d.emitLog(fmt.Sprintf("Downloading date %d/%d: %s", currentIndex, total, dateInfo.Date))

		// Download the historical imagery for this date
		// This will use the tile server's epoch fallback logic and zoom fallback
		err := d.DownloadHistoricalImagery(
			bbox,
			zoom,
			dateInfo.HexDate,
			dateInfo.Epoch,
			dateInfo.Date,
			format,
		)

		if err != nil {
			d.emitLog(fmt.Sprintf("Failed to download %s: %v", dateInfo.Date, err))
			failedDates = append(failedDates, dateInfo.Date)
			errors = append(errors, fmt.Errorf("%s: %w", dateInfo.Date, err))
			continue
		}

		successfulDates = append(successfulDates, dateInfo.Date)
		d.emitLog(fmt.Sprintf("Successfully downloaded %s", dateInfo.Date))
	}

	// Emit final progress
	d.emitProgress(downloads.DownloadProgress{
		Downloaded: total,
		Total:      total,
		Percent:    100,
		Status:     fmt.Sprintf("Downloaded %d/%d dates", len(successfulDates), total),
	})

	// Log summary
	d.emitLog(fmt.Sprintf("Range download complete: %d successful, %d failed", len(successfulDates), len(failedDates)))
	if len(failedDates) > 0 {
		d.emitLog(fmt.Sprintf("Failed dates: %v", failedDates))
	}

	// Track the range download completion
	d.trackEvent("range_download_complete", map[string]interface{}{
		"source":     "google_earth_historical",
		"total_dates": total,
		"successful": len(successfulDates),
		"failed":     len(failedDates),
		"zoom":       zoom,
		"format":     format,
	})

	// Return error if all downloads failed
	if len(successfulDates) == 0 {
		return fmt.Errorf("all %d date downloads failed", total)
	}

	// Return error if more than 50% failed
	if len(failedDates) > total/2 {
		return fmt.Errorf("range download partially failed: %d/%d dates failed", len(failedDates), total)
	}

	return nil
}

// DownloadHistoricalImageryRangeWithProgress downloads multiple dates with unified progress reporting
// This variant provides more granular progress updates across the entire range
func (d *Downloader) DownloadHistoricalImageryRangeWithProgress(
	bbox downloads.BoundingBox,
	zoom int,
	dates []GEDateInfo,
	format string,
) error {
	// Create a range tracker for unified progress
	rangeTracker := downloads.NewRangeTracker(len(dates))

	// Wrap the progress callback to include range information
	originalCallback := d.progressCallback
	if originalCallback != nil {
		d.progressCallback = func(progress downloads.DownloadProgress) {
			currentDate, totalDates := rangeTracker.GetProgress()
			progress.CurrentDate = currentDate
			progress.TotalDates = totalDates
			originalCallback(progress)
		}
		defer func() {
			d.progressCallback = originalCallback
		}()
	}

	return d.DownloadHistoricalImageryRange(bbox, zoom, dates, format, rangeTracker)
}

// ValidateDateRange validates a list of dates for download
func ValidateDateRange(dates []GEDateInfo) error {
	if len(dates) == 0 {
		return fmt.Errorf("no dates provided")
	}

	// Check for duplicates
	seen := make(map[string]bool)
	for i, dateInfo := range dates {
		if dateInfo.Date == "" {
			return fmt.Errorf("date %d: empty date string", i)
		}
		if dateInfo.HexDate == "" {
			return fmt.Errorf("date %d (%s): empty hexDate", i, dateInfo.Date)
		}
		if dateInfo.Epoch <= 0 {
			log.Printf("Warning: date %d (%s) has invalid epoch %d, will use fallback", i, dateInfo.Date, dateInfo.Epoch)
		}

		if seen[dateInfo.Date] {
			return fmt.Errorf("duplicate date: %s", dateInfo.Date)
		}
		seen[dateInfo.Date] = true
	}

	return nil
}

// EstimateRangeDownloadSize estimates the total download size for a range of dates
func EstimateRangeDownloadSize(bbox downloads.BoundingBox, zoom int, dateCount int) (int, float64, error) {
	// Validate coordinates
	if err := downloads.ValidateCoordinates(bbox, zoom); err != nil {
		return 0, 0, fmt.Errorf("invalid coordinates: %w", err)
	}

	// Get tiles for one date to estimate
	tiles, err := googleearth.GetTilesInBounds(bbox.South, bbox.West, bbox.North, bbox.East, zoom)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to calculate tiles: %w", err)
	}

	tilesPerDate := len(tiles)
	totalTiles := tilesPerDate * dateCount

	// Estimate size (average JPEG tile is ~15-25 KB, use 20 KB)
	avgTileKB := 20.0
	estimatedMB := (float64(totalTiles) * avgTileKB) / 1024.0

	return totalTiles, estimatedMB, nil
}
