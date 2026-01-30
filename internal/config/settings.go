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
	CacheMaxSizeMB int `json:"cacheMaxSizeMB"`
	CacheTTLDays   int `json:"cacheTTLDays"`

	// Default map settings
	DefaultZoom      int     `json:"defaultZoom"`
	DefaultSource    string  `json:"defaultSource"` // "esri", "google", or custom source name
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
}

// DefaultSettings returns default user settings
func DefaultSettings() *UserSettings {
	homeDir, _ := os.UserHomeDir()
	downloadPath := filepath.Join(homeDir, "Downloads", "imagery")

	return &UserSettings{
		DownloadPath:         downloadPath,
		CacheMaxSizeMB:       250,
		CacheTTLDays:         30,
		DefaultZoom:          10,
		DefaultSource:        "esri",
		DefaultCenterLat:     30.0444, // Cairo, Egypt
		DefaultCenterLon:     31.2357,
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
