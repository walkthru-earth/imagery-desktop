package main

import (
	"fmt"
	"log"

	"imagery-desktop/internal/config"
	"imagery-desktop/internal/wmts"
)

// ===================
// Settings Management
// ===================

// GetSettings returns current user settings
func (a *App) GetSettings() (*config.UserSettings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return a copy to prevent external modifications
	settingsCopy := *a.settings
	return &settingsCopy, nil
}

// SaveSettings saves user settings to disk and updates app state
func (a *App) SaveSettings(settings *config.UserSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Validate settings
	if settings.DownloadPath == "" {
		return fmt.Errorf("download path cannot be empty")
	}
	if settings.CacheMaxSizeMB <= 0 {
		return fmt.Errorf("cache size must be positive")
	}
	if settings.CacheTTLDays <= 0 {
		return fmt.Errorf("cache TTL must be positive")
	}

	// Save to disk
	if err := config.SaveSettings(settings); err != nil {
		return err
	}

	// Update app state
	a.settings = settings
	a.downloadPath = settings.DownloadPath

	// Note: Cache settings require app restart to take effect
	log.Printf("Settings saved. Cache settings will apply on next restart.")

	return nil
}

// GetSettingsPath returns the OS-specific settings file path
func (a *App) GetSettingsPath() string {
	return config.GetSettingsPath()
}

// SaveMapPosition saves the current map position for session persistence
// Called on app close or periodically to remember the last viewed location
func (a *App) SaveMapPosition(lat, lon, zoom float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.settings.LastCenterLat = lat
	a.settings.LastCenterLon = lon
	a.settings.LastZoom = zoom

	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Saved map position: lat=%.6f, lon=%.6f, zoom=%.1f", lat, lon, zoom)
	return nil
}

// ===================
// Custom Sources
// ===================

// AddCustomSource adds a new custom imagery source
func (a *App) AddCustomSource(source config.CustomSource) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Validate source
	if err := config.ValidateCustomSource(&source); err != nil {
		return err
	}

	// Check for duplicate names
	for _, existing := range a.settings.CustomSources {
		if existing.Name == source.Name {
			return fmt.Errorf("source with name '%s' already exists", source.Name)
		}
	}

	// Add to settings
	a.settings.CustomSources = append(a.settings.CustomSources, source)

	// Save settings
	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Added custom source: %s (%s)", source.Name, source.Type)
	return nil
}

// RemoveCustomSource removes a custom imagery source by name
func (a *App) RemoveCustomSource(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Find and remove source
	found := false
	newSources := make([]config.CustomSource, 0)
	for _, source := range a.settings.CustomSources {
		if source.Name != name {
			newSources = append(newSources, source)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("source '%s' not found", name)
	}

	a.settings.CustomSources = newSources

	// Save settings
	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Removed custom source: %s", name)
	return nil
}

// UpdateCustomSource updates an existing custom source
func (a *App) UpdateCustomSource(name string, source config.CustomSource) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Validate source
	if err := config.ValidateCustomSource(&source); err != nil {
		return err
	}

	// Find and update source
	found := false
	for i, existing := range a.settings.CustomSources {
		if existing.Name == name {
			a.settings.CustomSources[i] = source
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("source '%s' not found", name)
	}

	// Save settings
	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Updated custom source: %s", name)
	return nil
}

// ===================
// WMTS Integration
// ===================

// ValidateWMTSURL validates a WMTS capabilities URL
func (a *App) ValidateWMTSURL(url string) (bool, error) {
	valid, err := wmts.ValidateWMTSURL(url)
	if err != nil {
		return false, err
	}
	return valid, nil
}

// FetchWMTSLayers fetches available layers from a WMTS service
func (a *App) FetchWMTSLayers(url string) ([]wmts.LayerInfo, error) {
	caps, err := wmts.FetchCapabilities(url)
	if err != nil {
		return nil, err
	}

	layers := wmts.GetLayers(caps)
	log.Printf("Fetched %d layers from WMTS service", len(layers))

	return layers, nil
}

// CreateSourceFromWMTSLayer creates a custom source from a WMTS layer
func (a *App) CreateSourceFromWMTSLayer(layer wmts.LayerInfo, attribution string) config.CustomSource {
	return config.CustomSource{
		Name:        layer.Title,
		Type:        "wmts",
		URL:         wmts.ConvertTemplateToXYZ(layer.TemplateURL),
		Attribution: attribution,
		MaxZoom:     18, // Default, can be adjusted
		MinZoom:     0,
		Enabled:     true,
	}
}

// ===================
// Date Filter Patterns
// ===================

// AddDateFilterPattern adds a new date filter pattern
func (a *App) AddDateFilterPattern(pattern config.DateFilterPattern) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check for duplicate names
	for _, existing := range a.settings.DateFilterPatterns {
		if existing.Name == pattern.Name {
			return fmt.Errorf("pattern with name '%s' already exists", pattern.Name)
		}
	}

	a.settings.DateFilterPatterns = append(a.settings.DateFilterPatterns, pattern)

	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Added date filter pattern: %s", pattern.Name)
	return nil
}

// RemoveDateFilterPattern removes a date filter pattern
func (a *App) RemoveDateFilterPattern(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	found := false
	newPatterns := make([]config.DateFilterPattern, 0)
	for _, pattern := range a.settings.DateFilterPatterns {
		if pattern.Name != name {
			newPatterns = append(newPatterns, pattern)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("pattern '%s' not found", name)
	}

	a.settings.DateFilterPatterns = newPatterns

	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Removed date filter pattern: %s", name)
	return nil
}

// SetDefaultDatePattern sets the default date filter pattern
func (a *App) SetDefaultDatePattern(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Verify pattern exists
	found := false
	for _, pattern := range a.settings.DateFilterPatterns {
		if pattern.Name == name {
			found = true
			break
		}
	}

	if !found && name != "" {
		return fmt.Errorf("pattern '%s' not found", name)
	}

	a.settings.DefaultDatePattern = name

	if err := config.SaveSettings(a.settings); err != nil {
		return err
	}

	log.Printf("Set default date pattern: %s", name)
	return nil
}
