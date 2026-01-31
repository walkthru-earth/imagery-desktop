package main

import (
	"imagery-desktop/internal/ratelimit"
)

// Rate Limit Management Functions (Wails-exported)

// ManualRetryRateLimit allows user to manually trigger a retry for a rate-limited provider
func (a *App) ManualRetryRateLimit(provider string) {
	if a.rateLimitHandler != nil {
		a.rateLimitHandler.ManualRetry(provider)
	}
}

// GetRateLimitStatus returns the current rate limit state for a provider
func (a *App) GetRateLimitStatus(provider string) *ratelimit.RateLimitEvent {
	if a.rateLimitHandler != nil {
		return a.rateLimitHandler.GetCurrentState(provider)
	}
	return nil
}

// IsRateLimited checks if a provider is currently rate limited
func (a *App) IsRateLimited(provider string) bool {
	if a.rateLimitHandler != nil {
		return a.rateLimitHandler.IsRateLimited(provider)
	}
	return false
}

// SetAutoRetryRateLimit enables or disables automatic rate limit retries
func (a *App) SetAutoRetryRateLimit(enabled bool) {
	if a.rateLimitHandler != nil {
		a.rateLimitHandler.SetAutoRetry(enabled)
	}

	// Update settings
	if a.settings != nil {
		a.settings.AutoRetryOnRateLimit = enabled
		// Note: Settings will be saved when app closes via shutdown() hook
	}
}

// Cache Management Functions (Wails-exported)

// CacheStats represents cache statistics for frontend
type CacheStats struct {
	Entries   int     `json:"entries"`
	SizeBytes int64   `json:"sizeBytes"`
	MaxBytes  int64   `json:"maxBytes"`
	SizeMB    float64 `json:"sizeMB"`
	MaxMB     float64 `json:"maxMB"`
	CachePath string  `json:"cachePath"`
}

// GetCacheStats returns current cache statistics
func (a *App) GetCacheStats() CacheStats {
	if a.tileCache == nil {
		return CacheStats{}
	}

	entries, sizeBytes, maxBytes := a.tileCache.Stats()

	return CacheStats{
		Entries:   entries,
		SizeBytes: sizeBytes,
		MaxBytes:  maxBytes,
		SizeMB:    float64(sizeBytes) / 1024 / 1024,
		MaxMB:     float64(maxBytes) / 1024 / 1024,
		CachePath: a.tileCache.GetCachePath(),
	}
}

// ClearCache removes all cached tiles
func (a *App) ClearCache() error {
	if a.tileCache != nil {
		return a.tileCache.Clear()
	}
	return nil
}
