package taskqueue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TaskStatus represents the current status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// BoundingBox represents a geographic bounding box (matches app.go definition)
type BoundingBox struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

// GEDateInfo contains date info for Google Earth (matches app.go definition)
type GEDateInfo struct {
	Date    string `json:"date"`
	HexDate string `json:"hexDate"`
	Epoch   int    `json:"epoch"`
}

// VideoExportOptions contains video export settings (matches app.go definition)
type VideoExportOptions struct {
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Preset           string  `json:"preset"`
	CropX            float64 `json:"cropX"`
	CropY            float64 `json:"cropY"`
	SpotlightEnabled bool    `json:"spotlightEnabled"`
	SpotlightCenterLat float64 `json:"spotlightCenterLat"`
	SpotlightCenterLon float64 `json:"spotlightCenterLon"`
	SpotlightRadiusKm  float64 `json:"spotlightRadiusKm"`
	OverlayOpacity   float64 `json:"overlayOpacity"`
	ShowDateOverlay  bool    `json:"showDateOverlay"`
	DateFontSize     float64 `json:"dateFontSize"`
	DatePosition     string  `json:"datePosition"`
	FrameDelay       float64 `json:"frameDelay"`
	OutputFormat     string  `json:"outputFormat"`
	Quality          int     `json:"quality"`
}

// CropPreview represents crop area for map preview (relative 0-1 coords)
type CropPreview struct {
	X      float64 `json:"x"`      // Left position (0-1)
	Y      float64 `json:"y"`      // Top position (0-1)
	Width  float64 `json:"width"`  // Width (0-1)
	Height float64 `json:"height"` // Height (0-1)
}

// TaskProgress represents detailed progress information
type TaskProgress struct {
	CurrentPhase   string `json:"currentPhase"`   // "downloading", "merging", "encoding"
	TotalDates     int    `json:"totalDates"`
	CurrentDate    int    `json:"currentDate"`
	TilesTotal     int    `json:"tilesTotal"`
	TilesCompleted int    `json:"tilesCompleted"`
	Percent        int    `json:"percent"`
}

// ExportTask represents a single export task in the queue
type ExportTask struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      TaskStatus `json:"status"`
	Priority    int        `json:"priority"`    // Higher = more urgent (default 0)
	CreatedAt   string     `json:"createdAt"`   // ISO 8601 format
	StartedAt   string     `json:"startedAt,omitempty"`
	CompletedAt string     `json:"completedAt,omitempty"`

	// Export settings
	Source string      `json:"source"` // "esri" or "google"
	BBox   BoundingBox `json:"bbox"`
	Zoom   int         `json:"zoom"`
	Format string      `json:"format"` // "tiles", "geotiff", "both"

	// Date range
	Dates []GEDateInfo `json:"dates"`

	// Video options (optional)
	VideoExport bool                `json:"videoExport"`
	VideoOpts   *VideoExportOptions `json:"videoOpts,omitempty"`

	// Crop area for map preview
	CropPreview *CropPreview `json:"cropPreview,omitempty"`

	// Progress tracking
	Progress TaskProgress `json:"progress"`

	// Error message if failed
	Error string `json:"error,omitempty"`

	// Output path for completed exports
	OutputPath string `json:"outputPath,omitempty"`
}

// NewExportTask creates a new export task with default values
func NewExportTask(name string, source string, bbox BoundingBox, zoom int, dates []GEDateInfo) *ExportTask {
	return &ExportTask{
		ID:        generateTaskID(),
		Name:      name,
		Status:    TaskStatusPending,
		Priority:  0,
		CreatedAt: time.Now().Format(time.RFC3339),
		Source:    source,
		BBox:      bbox,
		Zoom:      zoom,
		Dates:     dates,
		Format:    "geotiff",
		Progress: TaskProgress{
			CurrentPhase: "",
			TotalDates:   len(dates),
		},
	}
}

// generateTaskID creates a unique task ID
func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

// SaveToFile persists the task to a JSON file
func (t *ExportTask) SaveToFile(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create task directory: %w", err)
	}

	path := filepath.Join(dir, t.ID+".json")
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write task file: %w", err)
	}

	return nil
}

// LoadFromFile loads a task from a JSON file
func LoadFromFile(path string) (*ExportTask, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read task file: %w", err)
	}

	var task ExportTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}

// DeleteFile removes the task file from disk
func (t *ExportTask) DeleteFile(dir string) error {
	path := filepath.Join(dir, t.ID+".json")
	return os.Remove(path)
}

// UpdateProgress updates the task's progress
func (t *ExportTask) UpdateProgress(phase string, currentDate, totalDates, tilesCompleted, tilesTotal int) {
	t.Progress.CurrentPhase = phase
	t.Progress.CurrentDate = currentDate
	t.Progress.TotalDates = totalDates
	t.Progress.TilesCompleted = tilesCompleted
	t.Progress.TilesTotal = tilesTotal

	// Calculate overall percent
	if totalDates > 0 && tilesTotal > 0 {
		dateProgress := float64(currentDate-1) / float64(totalDates)
		tileProgress := float64(tilesCompleted) / float64(tilesTotal)
		currentDateContribution := tileProgress / float64(totalDates)
		t.Progress.Percent = int((dateProgress + currentDateContribution) * 100)
	} else if totalDates > 0 {
		t.Progress.Percent = (currentDate * 100) / totalDates
	}

	if t.Progress.Percent > 100 {
		t.Progress.Percent = 100
	}
}

// MarkStarted marks the task as started
func (t *ExportTask) MarkStarted() {
	t.StartedAt = time.Now().Format(time.RFC3339)
	t.Status = TaskStatusRunning
}

// MarkCompleted marks the task as completed
func (t *ExportTask) MarkCompleted(outputPath string) {
	t.CompletedAt = time.Now().Format(time.RFC3339)
	t.Status = TaskStatusCompleted
	t.OutputPath = outputPath
	t.Progress.Percent = 100
}

// MarkFailed marks the task as failed with an error
func (t *ExportTask) MarkFailed(err error) {
	t.CompletedAt = time.Now().Format(time.RFC3339)
	t.Status = TaskStatusFailed
	if err != nil {
		t.Error = err.Error()
	}
}

// MarkCancelled marks the task as cancelled
func (t *ExportTask) MarkCancelled() {
	t.CompletedAt = time.Now().Format(time.RFC3339)
	t.Status = TaskStatusCancelled
}
