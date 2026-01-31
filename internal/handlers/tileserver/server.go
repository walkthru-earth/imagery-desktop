package tileserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	"imagery-desktop/internal/cache"
	"imagery-desktop/internal/esri"
	"imagery-desktop/internal/googleearth"
)

// Server manages the tile server HTTP server
type Server struct {
	ctx           context.Context
	geClient      *googleearth.Client
	esriClient    *esri.Client
	esriLayers    []*esri.Layer
	tileCache     *cache.PersistentTileCache
	tileServerURL string
	devMode       bool
}

// NewServer creates a new tile server instance
func NewServer(ctx context.Context, geClient *googleearth.Client, esriClient *esri.Client, esriLayers []*esri.Layer, tileCache *cache.PersistentTileCache, devMode bool) *Server {
	return &Server{
		ctx:        ctx,
		geClient:   geClient,
		esriClient: esriClient,
		esriLayers: esriLayers,
		tileCache:  tileCache,
		devMode:    devMode,
	}
}

// GetTileServerURL returns the tile server URL
func (s *Server) GetTileServerURL() string {
	return s.tileServerURL
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

// Start starts a local HTTP server to serve decrypted Google Earth tiles
func (s *Server) Start() error {
	// Create a new mux to avoid global state conflicts
	mux := http.NewServeMux()
	mux.HandleFunc("/google-earth/", s.handleGoogleEarthTile)
	mux.HandleFunc("/google-earth-historical/", s.handleGoogleEarthHistoricalTile)
	mux.HandleFunc("/esri-wayback/", s.handleEsriTile)

	// Listen on a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start tile server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	s.tileServerURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	log.Printf("Tile server started on %s", s.tileServerURL)

	// Wrap mux with CORS middleware
	server := &http.Server{
		Handler: corsMiddleware(mux),
	}

	// Start server in goroutine
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Printf("Tile server stopped: %v", err)
		}
	}()

	return nil
}
