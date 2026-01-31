package tileserver

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"imagery-desktop/internal/common"
	"imagery-desktop/internal/esri"
)

// handleEsriTile serves Esri Wayback tiles with persistent caching
// URL format: /esri-wayback/{date}/{z}/{x}/{y}
// This provides the same caching benefits as Google Earth tile proxy
func (s *Server) handleEsriTile(w http.ResponseWriter, r *http.Request) {
	// Parse URL path: /esri-wayback/{date}/{z}/{x}/{y}
	path := strings.TrimPrefix(r.URL.Path, "/esri-wayback/")
	parts := strings.Split(path, "/")

	if len(parts) != 4 {
		http.Error(w, "Invalid URL format. Expected: /esri-wayback/{date}/{z}/{x}/{y}", http.StatusBadRequest)
		return
	}

	date := parts[0]
	z, err := strconv.Atoi(parts[1])
	if err != nil {
		http.Error(w, "Invalid zoom level", http.StatusBadRequest)
		return
	}

	x, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid X coordinate", http.StatusBadRequest)
		return
	}

	y, err := strconv.Atoi(parts[3])
	if err != nil {
		http.Error(w, "Invalid Y coordinate", http.StatusBadRequest)
		return
	}

	// Check cache first
	cacheKey := fmt.Sprintf("%s:%d:%d:%d:%s", common.ProviderEsriWayback, z, x, y, date)
	if cachedData, found := s.tileCache.Get(cacheKey); found {
		log.Printf("[EsriTileServer] Cache hit: %s", cacheKey)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year cache
		w.Header().Set("X-Cache-Status", "HIT")
		w.Write(cachedData)
		return
	}

	// Cache miss - fetch from Esri API
	log.Printf("[EsriTileServer] Cache miss, fetching: date=%s z=%d x=%d y=%d", date, z, x, y)

	// Find Esri layer for this date
	layer, err := s.findLayerForDate(date)
	if err != nil {
		log.Printf("[EsriTileServer] Failed to find layer for date %s: %v", date, err)
		http.Error(w, fmt.Sprintf("No Esri Wayback layer found for date %s", date), http.StatusNotFound)
		return
	}

	// Create Esri tile
	tile := &esri.EsriTile{
		Level:  z,
		Row:    y,
		Column: x,
	}

	// Fetch tile from Esri API
	tileData, err := s.esriClient.FetchTile(layer, tile)
	if err != nil {
		log.Printf("[EsriTileServer] Failed to fetch tile: %v", err)
		// Serve transparent tile on error
		s.serveTransparentTile(w)
		return
	}

	// Cache the tile
	s.tileCache.Set(common.ProviderEsriWayback, z, x, y, date, tileData)
	log.Printf("[EsriTileServer] Cached tile: %s", cacheKey)

	// Serve the tile
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year cache
	w.Header().Set("X-Cache-Status", "MISS")
	w.Write(tileData)
}

// findLayerForDate finds the Esri Wayback layer matching a specific date
// This is a helper method that uses cached layers for performance
func (s *Server) findLayerForDate(targetDate string) (*esri.Layer, error) {
	if len(s.esriLayers) == 0 {
		return nil, fmt.Errorf("Esri Wayback layers not loaded")
	}

	// Find exact match using date string comparison
	for _, layer := range s.esriLayers {
		if layer.Date.Format("2006-01-02") == targetDate {
			return layer, nil
		}
	}

	return nil, fmt.Errorf("no layer found for date: %s", targetDate)
}
