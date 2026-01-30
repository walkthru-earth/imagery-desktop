package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// isDevMode detects if running in development mode
// Production builds will have embedded assets, dev mode uses live server
func isDevMode() bool {
	// Check if running with `wails dev` by looking for common dev indicators
	// In dev mode, Wails serves from localhost:34115 (or similar)
	return os.Getenv("WAILS_DEV_SERVER") != "" || os.Getenv("FRONTEND_DEVSERVER_URL") != ""
}

func main() {
	// Setup log file in user home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("Failed to get user home directory:", err)
	}

	// Create app-specific hidden directory: ~/.imagery-desktop (cross-platform)
	appDir := filepath.Join(homeDir, ".imagery-desktop")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		log.Fatal("Failed to create app directory:", err)
	}

	// Create log file in app directory
	logPath := filepath.Join(appDir, "debug.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	defer logFile.Close()

	// Set log output to file with flags for timestamp and file location
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("=== Imagery Desktop Started ===")
	log.Printf("App directory: %s", appDir)
	log.Printf("Log file: %s", logPath)

	// Also print to console for user awareness
	println("Debug logs:", logPath)

	// Create an instance of the app structure
	app := NewApp()

	// Enable dev mode based on environment or debug detection
	// Set DEV_MODE=1 environment variable when running in development
	app.devMode = os.Getenv("DEV_MODE") == "1" || isDevMode()

	// Create application with options
	if err := wails.Run(&options.App{
		Title:  "Imagery Desktop",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		log.Fatal("Error starting application:", err)
	}
}
