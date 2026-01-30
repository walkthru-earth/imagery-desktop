package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
)

// Config represents cache configuration
type Config struct {
	MaxSizeMB int `json:"maxSizeMB"`
	TTLDays   int `json:"ttlDays"`
}

// DefaultConfig returns default cache configuration
func DefaultConfig() *Config {
	return &Config{
		MaxSizeMB: 250,  // 250 MB default
		TTLDays:   30,   // 30 days default
	}
}

// LoadConfig loads cache configuration from file or returns defaults
func LoadConfig(configPath string) (*Config, error) {
	config := DefaultConfig()

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return config, nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return config, err
	}

	// Parse JSON
	var fileConfig struct {
		Cache *Config `json:"cache"`
	}
	if err := json.Unmarshal(data, &fileConfig); err != nil {
		return config, err
	}

	// Merge with defaults
	if fileConfig.Cache != nil {
		if fileConfig.Cache.MaxSizeMB > 0 {
			config.MaxSizeMB = fileConfig.Cache.MaxSizeMB
		}
		if fileConfig.Cache.TTLDays > 0 {
			config.TTLDays = fileConfig.Cache.TTLDays
		}
	}

	return config, nil
}

// GetCacheDir returns the OS-specific cache directory
func GetCacheDir() string {
	homeDir, _ := os.UserHomeDir()

	switch goruntime.GOOS {
	case "darwin": // macOS
		return filepath.Join(homeDir, "Library", "Caches", "imagery-desktop", "tiles")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		return filepath.Join(appData, "imagery-desktop", "cache", "tiles")
	default: // Linux and others
		cacheHome := os.Getenv("XDG_CACHE_HOME")
		if cacheHome == "" {
			cacheHome = filepath.Join(homeDir, ".cache")
		}
		return filepath.Join(cacheHome, "imagery-desktop", "tiles")
	}
}
