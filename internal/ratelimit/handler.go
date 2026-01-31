package ratelimit

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// RetryStrategy defines the backoff intervals for rate limit retries
type RetryStrategy struct {
	Intervals []time.Duration // e.g., [5min, 10min, 15min, 20min, 30min]
	MaxRetries int
}

// DefaultRetryStrategy returns the default exponential backoff strategy
func DefaultRetryStrategy() *RetryStrategy {
	return &RetryStrategy{
		Intervals: []time.Duration{
			5 * time.Minute,   // First retry after 5 mins
			10 * time.Minute,  // Second retry after 10 mins
			15 * time.Minute,  // Third retry after 15 mins
			20 * time.Minute,  // Fourth retry after 20 mins
			30 * time.Minute,  // Fifth+ retries after 30 mins
		},
		MaxRetries: 10, // Maximum number of automatic retries before giving up
	}
}

// RateLimitEvent represents a rate limit occurrence
type RateLimitEvent struct {
	Timestamp    time.Time `json:"timestamp" ts_type:"string"`
	Provider     string    `json:"provider"` // "google_earth" or "esri_wayback"
	StatusCode   int       `json:"statusCode"` // HTTP status code (403, 429, etc.)
	RetryAttempt int       `json:"retryAttempt"` // Current retry attempt (0 = first occurrence)
	NextRetryAt  time.Time `json:"nextRetryAt" ts_type:"string"`
	Message      string    `json:"message"` // User-friendly message
}

// Handler manages rate limit detection and retry logic
type Handler struct {
	mu               sync.RWMutex
	rateLimited      map[string]*RateLimitEvent // provider -> current rate limit state
	strategy         *RetryStrategy
	onRateLimit      func(event RateLimitEvent) // Callback for UI notification
	onRetry          func(event RateLimitEvent) // Callback for retry notification
	onRecovered      func(provider string)      // Callback when rate limit clears
	autoRetryEnabled bool
	ctx              context.Context
	cancel           context.CancelFunc
}

// NewHandler creates a new rate limit handler
func NewHandler(strategy *RetryStrategy) *Handler {
	if strategy == nil {
		strategy = DefaultRetryStrategy()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Handler{
		rateLimited:      make(map[string]*RateLimitEvent),
		strategy:         strategy,
		autoRetryEnabled: true,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// SetOnRateLimit sets the callback for rate limit events
func (h *Handler) SetOnRateLimit(callback func(event RateLimitEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onRateLimit = callback
}

// SetOnRetry sets the callback for retry attempts
func (h *Handler) SetOnRetry(callback func(event RateLimitEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onRetry = callback
}

// SetOnRecovered sets the callback for recovery from rate limit
func (h *Handler) SetOnRecovered(callback func(provider string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onRecovered = callback
}

// IsRateLimited checks if a provider is currently rate limited
func (h *Handler) IsRateLimited(provider string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, limited := h.rateLimited[provider]
	return limited
}

// CheckResponse analyzes an HTTP response for rate limit indicators
func (h *Handler) CheckResponse(provider string, resp *http.Response) bool {
	// Rate limit status codes
	isRateLimited := resp.StatusCode == 429 || // Too Many Requests
		resp.StatusCode == 403 || // Forbidden (Google uses this for rate limits)
		resp.StatusCode == 509 // Bandwidth Limit Exceeded

	if !isRateLimited {
		// Check if we were previously rate limited and have now recovered
		h.checkRecovery(provider)
		return false
	}

	// We're rate limited - record the event
	h.recordRateLimit(provider, resp.StatusCode)
	return true
}

// recordRateLimit records a rate limit event and schedules retry
func (h *Handler) recordRateLimit(provider string, statusCode int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if we already have a rate limit for this provider
	existing, exists := h.rateLimited[provider]

	retryAttempt := 0
	if exists {
		retryAttempt = existing.RetryAttempt + 1
	}

	// Calculate next retry time based on retry attempt
	var interval time.Duration
	if retryAttempt < len(h.strategy.Intervals) {
		interval = h.strategy.Intervals[retryAttempt]
	} else {
		// Use last interval for all subsequent retries
		interval = h.strategy.Intervals[len(h.strategy.Intervals)-1]
	}

	nextRetryAt := time.Now().Add(interval)

	event := RateLimitEvent{
		Timestamp:    time.Now(),
		Provider:     provider,
		StatusCode:   statusCode,
		RetryAttempt: retryAttempt,
		NextRetryAt:  nextRetryAt,
		Message:      h.buildMessage(provider, statusCode, retryAttempt, nextRetryAt),
	}

	h.rateLimited[provider] = &event

	log.Printf("[RateLimit] %s rate limited (attempt %d). Next retry at %s",
		provider, retryAttempt, nextRetryAt.Format(time.RFC3339))

	// Notify UI
	if h.onRateLimit != nil {
		go h.onRateLimit(event)
	}

	// Schedule auto-retry if enabled
	if h.autoRetryEnabled && retryAttempt < h.strategy.MaxRetries {
		go h.scheduleRetry(provider, event)
	}
}

// scheduleRetry schedules an automatic retry after the backoff interval
func (h *Handler) scheduleRetry(provider string, event RateLimitEvent) {
	waitDuration := time.Until(event.NextRetryAt)

	select {
	case <-time.After(waitDuration):
		// Time to retry
		h.mu.Lock()
		current, exists := h.rateLimited[provider]
		if !exists || current.Timestamp != event.Timestamp {
			// Rate limit was already cleared or replaced
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()

		log.Printf("[RateLimit] Auto-retrying %s after %s wait", provider, waitDuration)

		// Notify UI of retry attempt
		if h.onRetry != nil {
			go h.onRetry(event)
		}

		// The actual retry will happen when the next download is attempted
		// The download logic should check IsRateLimited() before proceeding

	case <-h.ctx.Done():
		// Handler was shut down
		return
	}
}

// checkRecovery checks if we've recovered from a rate limit
func (h *Handler) checkRecovery(provider string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.rateLimited[provider]; exists {
		delete(h.rateLimited, provider)
		log.Printf("[RateLimit] %s rate limit cleared - download resumed", provider)

		if h.onRecovered != nil {
			go h.onRecovered(provider)
		}
	}
}

// ManualRetry allows user to manually trigger a retry
func (h *Handler) ManualRetry(provider string) {
	h.mu.Lock()
	event, exists := h.rateLimited[provider]
	if !exists {
		h.mu.Unlock()
		return
	}

	log.Printf("[RateLimit] Manual retry requested for %s", provider)

	// Clear the rate limit to allow retry
	delete(h.rateLimited, provider)
	h.mu.Unlock()

	// Notify about retry
	if h.onRetry != nil {
		go h.onRetry(*event)
	}
}

// SetAutoRetry enables or disables automatic retries
func (h *Handler) SetAutoRetry(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.autoRetryEnabled = enabled
}

// GetCurrentState returns the current rate limit state for a provider
func (h *Handler) GetCurrentState(provider string) *RateLimitEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if event, exists := h.rateLimited[provider]; exists {
		// Return a copy
		eventCopy := *event
		return &eventCopy
	}
	return nil
}

// buildMessage creates a user-friendly message
func (h *Handler) buildMessage(provider string, statusCode int, retryAttempt int, nextRetryAt time.Time) string {
	providerName := "Google Earth"
	if provider == "esri_wayback" {
		providerName = "Esri"
	}

	waitDuration := time.Until(nextRetryAt)
	minutes := int(waitDuration.Minutes())

	var message string
	if retryAttempt == 0 {
		message = fmt.Sprintf(
			"%s rate limit detected (HTTP %d). Downloads paused.\n\n"+
				"This usually happens when downloading large areas. "+
				"You can:\n"+
				"1. Wait %d minutes for automatic retry\n"+
				"2. Change your IP address and click 'Retry Now'\n"+
				"3. Try again later (recommended: 30+ minutes)",
			providerName, statusCode, minutes)
	} else {
		message = fmt.Sprintf(
			"%s still rate limited (retry attempt %d).\n\n"+
				"Next automatic retry in %d minutes.\n\n"+
				"Consider waiting longer or changing your IP address.",
			providerName, retryAttempt+1, minutes)
	}

	return message
}

// Close shuts down the rate limit handler
func (h *Handler) Close() {
	h.cancel()
}
