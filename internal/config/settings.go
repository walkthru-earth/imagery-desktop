package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CustomSource represents a user-added imagery source
type CustomSource struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "wmts", "wms", "xyz", "tms"
	URL         string `json:"url"`
	Attribution string `json:"attribution,omitempty"`
	MaxZoom     int    `json:"maxZoom,omitempty"`
	MinZoom     int    `json:"minZoom,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// DateFilterPattern represents a regex pattern for filtering dates
type DateFilterPattern struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"` // Regex pattern for date matching
	Enabled bool   `json:"enabled"`
}

// UserSettings represents persistent user preferences
type UserSettings struct {
	// Download settings
	DownloadPath string `json:"downloadPath"`

	// Cache settings
	CachePath      string `json:"cachePath"` // Custom cache location (empty = default)
	CacheMaxSizeMB int    `json:"cacheMaxSizeMB"`
	CacheTTLDays   int    `json:"cacheTTLDays"`

	// Rate limit handling
	AutoRetryOnRateLimit bool `json:"autoRetryOnRateLimit"` // Enable automatic retry on rate limits

	// Default map settings
	DefaultZoom      int     `json:"defaultZoom"`
	DefaultSource    string  `json:"defaultSource"` // "esri_wayback", "google_earth", or custom source name
	DefaultCenterLat float64 `json:"defaultCenterLat"`
	DefaultCenterLon float64 `json:"defaultCenterLon"`

	// Download settings
	DownloadZoomStrategy string `json:"downloadZoomStrategy"` // "current" or "fixed"
	DownloadFixedZoom    int    `json:"downloadFixedZoom"`

	// Custom imagery sources
	CustomSources []CustomSource `json:"customSources"`

	// Date filtering
	DateFilterPatterns []DateFilterPattern `json:"dateFilterPatterns"`
	DefaultDatePattern string              `json:"defaultDatePattern"` // Name of default pattern to apply

	// UI preferences
	Theme               string `json:"theme"` // "light", "dark", "system"
	ShowTileGrid        bool   `json:"showTileGrid"`
	ShowCoordinates     bool   `json:"showCoordinates"`
	AutoOpenDownloadDir bool   `json:"autoOpenDownloadDir"`
	CheckForUpdates     bool   `json:"checkForUpdates"` // Check for updates on startup

	// Task queue settings
	MaxConcurrentTasks int  `json:"maxConcurrentTasks"` // 1-5, default 1
	TaskPanelOpen      bool `json:"taskPanelOpen"`      // Whether task panel is expanded

	// Last session map state (auto-saved on app close)
	LastCenterLat float64 `json:"lastCenterLat"`
	LastCenterLon float64 `json:"lastCenterLon"`
	LastZoom      float64 `json:"lastZoom"`
}

// DefaultSettings returns default user settings
func DefaultSettings() *UserSettings {
	homeDir, _ := os.UserHomeDir()
	downloadPath := filepath.Join(homeDir, "Downloads", "imagery")

	return &UserSettings{
		DownloadPath:          downloadPath,
		CachePath:             "", // Empty = use default app data location
		CacheMaxSizeMB:        500, // Increased default: 500MB
		CacheTTLDays:          90,  // Increased default: 90 days
		AutoRetryOnRateLimit:  true,
		DefaultZoom:          15,
		DefaultSource:        "esri_wayback",
		DefaultCenterLat:     30.0621, // Zamalek, Cairo, Egypt
		DefaultCenterLon:     31.2219,
		DownloadZoomStrategy: "fixed",
		DownloadFixedZoom:    19,
		CustomSources:        []CustomSource{},
		DateFilterPatterns: []DateFilterPattern{
			{
				Name:    "Recent (Last 5 Years)",
				Pattern: `^20(2[0-9]|1[5-9])-`,
				Enabled: false,
			},
			{
				Name:    "2020s Only",
				Pattern: `^202[0-9]-`,
				Enabled: false,
			},
			{
				Name:    "Summer Months (June-August)",
				Pattern: `^[0-9]{4}-(06|07|08)-`,
				Enabled: false,
			},
		},
		DefaultDatePattern:  "",
		Theme:               "system",
		ShowTileGrid:        false,
		ShowCoordinates:     false,
		AutoOpenDownloadDir: true,
		CheckForUpdates:     true, // Check for updates on startup by default
		MaxConcurrentTasks:  1,
		TaskPanelOpen:       false,
		LastCenterLat:       30.0621, // Zamalek, Cairo (same as DefaultCenterLat)
		LastCenterLon:       31.2219, // Zamalek, Cairo (same as DefaultCenterLon)
		LastZoom:            15,
	}
}

// GetSettingsPath returns the OS-specific settings file path
func GetSettingsPath() string {
	homeDir, _ := os.UserHomeDir()

	// Use unified directory structure: ~/.walkthru-earth/imagery-desktop/settings/
	baseDir := filepath.Join(homeDir, ".walkthru-earth", "imagery-desktop", "settings")

	// Ensure directory exists
	os.MkdirAll(baseDir, 0755)

	return filepath.Join(baseDir, "settings.json")
}

// GetDefaultCachePath returns the default cache directory path
// Uses OS-specific app data locations with OGC ZXY structure
func GetDefaultCachePath() string {
	homeDir, _ := os.UserHomeDir()

	// Use unified directory structure: ~/.walkthru-earth/imagery-desktop/cache/
	cachePath := filepath.Join(homeDir, ".walkthru-earth", "imagery-desktop", "cache")

	return cachePath
}

// GetCachePath returns the cache path from settings, or default if not set
func GetCachePath(settings *UserSettings) string {
	if settings.CachePath != "" {
		return settings.CachePath
	}
	return GetDefaultCachePath()
}

// LoadSettings loads user settings from disk
func LoadSettings() (*UserSettings, error) {
	settingsPath := GetSettingsPath()

	// If file doesn't exist, return defaults
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		return DefaultSettings(), nil
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings UserSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings: %w", err)
	}

	// Merge with defaults for any missing fields
	defaults := DefaultSettings()
	if settings.DownloadPath == "" {
		settings.DownloadPath = defaults.DownloadPath
	}
	if settings.CacheMaxSizeMB == 0 {
		settings.CacheMaxSizeMB = defaults.CacheMaxSizeMB
	}
	if settings.CacheTTLDays == 0 {
		settings.CacheTTLDays = defaults.CacheTTLDays
	}
	// CachePath can be empty (means use default), so don't override it
	if settings.DefaultZoom == 0 {
		settings.DefaultZoom = defaults.DefaultZoom
	}
	if settings.DefaultSource == "" {
		settings.DefaultSource = defaults.DefaultSource
	}
	if settings.Theme == "" {
		settings.Theme = defaults.Theme
	}
	if settings.DownloadZoomStrategy == "" {
		settings.DownloadZoomStrategy = defaults.DownloadZoomStrategy
	}
	if settings.DownloadFixedZoom == 0 {
		settings.DownloadFixedZoom = defaults.DownloadFixedZoom
	}
	if settings.MaxConcurrentTasks == 0 {
		settings.MaxConcurrentTasks = defaults.MaxConcurrentTasks
	}
	// Clamp MaxConcurrentTasks to valid range
	if settings.MaxConcurrentTasks < 1 {
		settings.MaxConcurrentTasks = 1
	}
	if settings.MaxConcurrentTasks > 5 {
		settings.MaxConcurrentTasks = 5
	}
	// Default last position to Cairo if not set (0 values indicate unset)
	if settings.LastCenterLat == 0 && settings.LastCenterLon == 0 {
		settings.LastCenterLat = defaults.LastCenterLat
		settings.LastCenterLon = defaults.LastCenterLon
	}
	if settings.LastZoom == 0 {
		settings.LastZoom = defaults.LastZoom
	}

	return &settings, nil
}

// SaveSettings saves user settings to disk
func SaveSettings(settings *UserSettings) error {
	settingsPath := GetSettingsPath()

	// Ensure directory exists
	dir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}

	return nil
}

// ValidateCustomSource validates a custom source configuration
func ValidateCustomSource(source *CustomSource) error {
	if source.Name == "" {
		return fmt.Errorf("source name is required")
	}
	if source.URL == "" {
		return fmt.Errorf("source URL is required")
	}
	if source.Type == "" {
		return fmt.Errorf("source type is required")
	}

	// Validate type
	validTypes := map[string]bool{
		"wmts": true,
		"wms":  true,
		"xyz":  true,
		"tms":  true,
	}
	if !validTypes[source.Type] {
		return fmt.Errorf("invalid source type: %s (must be wmts, wms, xyz, or tms)", source.Type)
	}

	return nil
}
