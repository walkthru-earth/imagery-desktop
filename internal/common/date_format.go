package common

import (
	"fmt"
	"time"
)

// Standard date format constants
const (
	// ISO8601Date is the standard date format used throughout the application
	// for cache keys, file naming, and API communication
	ISO8601Date = "2006-01-02"

	// DisplayDate is the human-readable format used for UI display
	DisplayDate = "Jan 02, 2006"

	// VideoOverlayDate is the format used in video frame overlays
	VideoOverlayDate = "January 2, 2006"
)

// ParseISO8601 parses a date string in ISO 8601 format (YYYY-MM-DD)
func ParseISO8601(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("date string is empty")
	}
	return time.Parse(ISO8601Date, dateStr)
}

// FormatISO8601 formats a time.Time to ISO 8601 date string (YYYY-MM-DD)
func FormatISO8601(t time.Time) string {
	return t.Format(ISO8601Date)
}

// FormatDisplay formats a time.Time to display format (Jan 02, 2006)
func FormatDisplay(t time.Time) string {
	return t.Format(DisplayDate)
}

// FormatVideoOverlay formats a time.Time for video overlay text (January 2, 2006)
func FormatVideoOverlay(t time.Time) string {
	return t.Format(VideoOverlayDate)
}

// CurrentDateISO8601 returns the current date in ISO 8601 format
func CurrentDateISO8601() string {
	return time.Now().Format(ISO8601Date)
}

// ValidateISO8601 checks if a date string is in valid ISO 8601 format
func ValidateISO8601(dateStr string) bool {
	_, err := ParseISO8601(dateStr)
	return err == nil
}
