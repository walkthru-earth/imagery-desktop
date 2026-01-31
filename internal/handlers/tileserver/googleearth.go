package tileserver

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"imagery-desktop/internal/common"
	"imagery-desktop/internal/googleearth"
)

const TileSize = 256

// Helper function for max of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// handleGoogleEarthTile handles requests for Google Earth tiles
// URL format: /google-earth/{date}/{z}/{x}/{y}
// date format: YYYY-MM-DD (must be exact date from GetGoogleEarthDatesForArea)
// This handler reprojects GE tiles (Plate Carrée) to Web Mercator for MapLibre
func (s *Server) handleGoogleEarthTile(w http.ResponseWriter, r *http.Request) {
	// Parse path components
	// Expected: /google-earth/date/z/x/y
	path := r.URL.Path
	if len(path) < 14 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	parts := strings.Split(path[14:], "/") // Remove /google-earth/ prefix
	if len(parts) < 4 {
		http.Error(w, "Invalid path format, expected /google-earth/{date}/{z}/{x}/{y}", http.StatusBadRequest)
		return
	}

	dateStr := parts[0]
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

			// Use date from URL for caching
			var data []byte

			// Check cache first
			if s.tileCache != nil {
				cacheKey := fmt.Sprintf("%s:%d:%d:%d:%s", common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, dateStr)
				if cachedData, found := s.tileCache.Get(cacheKey); found {
					data = cachedData
					if s.devMode {
						log.Printf("[Cache HIT] Google Earth tile z=%d x=%d y=%d (date: %s)", tile.Level, tile.Column, tile.Row, dateStr)
					}
				}
			}

			// Fetch from source if not cached
			if data == nil {
				data, err = s.geClient.FetchTile(tile)
				if err != nil {
					continue
				}

				if s.devMode {
					log.Printf("[Cache MISS] Google Earth tile z=%d x=%d y=%d (date: %s) - fetched from network", tile.Level, tile.Column, tile.Row, dateStr)
				}

				// Cache the result
				if s.tileCache != nil {
					s.tileCache.Set(common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, dateStr, data)
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

// handleGoogleEarthHistoricalTile handles requests for historical Google Earth tiles
// URL format: /google-earth-historical/{date}_{hexDate}/{z}/{x}/{y}
// date format: YYYY-MM-DD (for human-readable cache), hexDate: hex string (for tile fetching)
// This handler reprojects GE tiles (Plate Carrée) to Web Mercator for MapLibre
func (s *Server) handleGoogleEarthHistoricalTile(w http.ResponseWriter, r *http.Request) {
	// Parse path components
	// Expected: /google-earth-historical/{date}_{hexDate}/{z}/{x}/{y}
	path := r.URL.Path
	prefix := "/google-earth-historical/"
	if !strings.HasPrefix(path, prefix) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	parts := strings.Split(path[len(prefix):], "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path format, expected /google-earth-historical/{date}_{hexDate}/{z}/{x}/{y}", http.StatusBadRequest)
		return
	}

	// Split date_hexDate into date and hexDate
	dateHexParts := strings.Split(parts[0], "_")
	if len(dateHexParts) != 2 {
		http.Error(w, "Invalid date format, expected {date}_{hexDate}", http.StatusBadRequest)
		return
	}

	date := dateHexParts[0]    // Human-readable date (YYYY-MM-DD)
	hexDate := dateHexParts[1] // Hex date for tile fetching
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
			var data []byte

			if s.tileCache != nil {
				// Build cache key using human-readable date for organized cache structure
				cacheKey := fmt.Sprintf("%s:%d:%d:%d:%s", common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, date)
				if cachedData, found := s.tileCache.Get(cacheKey); found {
					data = cachedData
					successCount++
				}
			}

			// Fetch from source if not cached (with full epoch fallback)
			if data == nil {
				data, err = s.fetchHistoricalGETile(tile, date, hexDate)
				if err != nil {
					log.Printf("[GEHistorical] Tile %s at zoom %d failed: %v", tile.Path, tryZoom, err)
					continue
				}

				// Cache the successful result using OGC ZXY structure with human-readable date
				if s.tileCache != nil {
					s.tileCache.Set(common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, date, data)
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
		s.serveTransparentTile(w)
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

// fetchHistoricalGETile fetches a historical tile for the given GE tile coordinates and hexDate
// It handles epoch lookup and fallback to nearest date
// date: human-readable date (YYYY-MM-DD) for cache storage
// hexDate: hex date for Google API tile fetching
func (s *Server) fetchHistoricalGETile(tile *googleearth.Tile, date, hexDate string) ([]byte, error) {
	// Check cache first
	if s.tileCache != nil {
		cacheKey := fmt.Sprintf("%s:%d:%d:%d:%s", common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, date)
		if cachedData, found := s.tileCache.Get(cacheKey); found {
			if s.devMode {
				log.Printf("[Cache HIT] Historical tile %s (date: %s)", tile.Path, date)
			}
			return cachedData, nil
		}
	}

	// Get available dates for this specific tile to find the correct epoch
	dates, err := s.geClient.GetAvailableDates(tile)
	if err != nil {
		return nil, fmt.Errorf("GetAvailableDates failed: %w", err)
	}

	if len(dates) == 0 {
		return nil, fmt.Errorf("no dates available for tile")
	}

	// Find the epoch for the requested hexDate
	var epoch int
	var foundHexDate string
	found := false
	for _, dt := range dates {
		if dt.HexDate == hexDate {
			epoch = dt.Epoch
			foundHexDate = hexDate
			found = true
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
		if s.devMode {
			log.Printf("[fetchHistoricalGETile] Using nearest date: %s (requested: %s)", foundHexDate, hexDate)
		}
	}

	// Try fetching with the protobuf-reported epoch first
	data, err := s.geClient.FetchHistoricalTile(tile, epoch, foundHexDate)
	if err == nil {
		// Cache the result using human-readable date for OGC compliance
		if s.tileCache != nil {
			s.tileCache.Set(common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, date, data)
		}
		return data, nil
	}

	// If the primary epoch fails (404), try with older epochs from the same tile
	// This mimics Google Earth Pro's behavior which uses older, stable epochs

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
		data, err := s.geClient.FetchHistoricalTile(tile, ef.epoch, foundHexDate)
		if err == nil {
			// Cache the result using human-readable date for OGC compliance
			if s.tileCache != nil {
				s.tileCache.Set(common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, date, data)
			}
			return data, nil
		}
	}

	// Last resort: Try known-good epochs for recent dates
	// These epochs may not be in the protobuf but are known to work from testing
	// Epochs are ordered newest-first (more likely to have tiles for recent dates):
	// - 365, 361, 360: 2025+ dates at high zoom levels (17-21)
	// - 358, 357, 356, 354, 352: 2024 dates
	// - 321: 2023 dates
	// - 296, 273: 2020-2022 dates
	knownGoodEpochs := []int{365, 361, 360, 358, 357, 356, 354, 352, 321, 296, 273}
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
		data, err := s.geClient.FetchHistoricalTile(tile, knownEpoch, foundHexDate)
		if err == nil {
			// Cache the result using human-readable date for OGC compliance
			if s.tileCache != nil {
				s.tileCache.Set(common.ProviderGoogleEarth, tile.Level, tile.Column, tile.Row, date, data)
			}
			return data, nil
		}
	}

	return nil, fmt.Errorf("tile not available with any known epoch (tried %d epochs)", len(epochList)+1+len(knownGoodEpochs))
}

// FetchHistoricalGETileWithZoomFallback attempts to fetch a historical tile with automatic zoom fallback
// If the tile doesn't exist at the requested zoom, it tries lower zoom levels (z-1, z-2, etc.)
// When using a lower zoom tile, it extracts and upscales the correct portion to match the original tile
// Returns the tile data and the zoom level that succeeded, or error if all attempts fail
func (s *Server) FetchHistoricalGETileWithZoomFallback(tile *googleearth.Tile, date, hexDate string, maxFallbackLevels int) ([]byte, int, error) {
	// Try the requested zoom first
	data, err := s.fetchHistoricalGETile(tile, date, hexDate)
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
		data, err := s.fetchHistoricalGETile(lowerTile, date, hexDate)
		if err == nil {
			log.Printf("[ZoomFallback] SUCCESS at zoom %d, extracting quadrant for original tile", lowerZoom)

			// Extract and upscale the correct portion of the lower zoom tile
			// to match the original requested tile
			croppedData, err := s.extractQuadrantFromFallbackTile(data, originalRow, originalCol, originalZoom, lowerTile.Row, lowerTile.Column, lowerZoom)
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
func (s *Server) extractQuadrantFromFallbackTile(data []byte, origRow, origCol, origZoom, fallbackRow, fallbackCol, fallbackZoom int) ([]byte, error) {
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
