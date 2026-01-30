package main

import (
	"embed"
	"os"

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
	// Create an instance of the app structure
	app := NewApp()

	// Enable dev mode based on environment or debug detection
	// Set DEV_MODE=1 environment variable when running in development
	app.devMode = os.Getenv("DEV_MODE") == "1" || isDevMode()

	// Create application with options
	err := wails.Run(&options.App{
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
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
