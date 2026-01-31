package video

import (
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"imagery-desktop/internal/utils/naming"
)

// BoundingBox represents geographic bounds (using same structure as downloads package)
type BoundingBox struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

// DateInfo contains date information for timelapse frames
type DateInfo struct {
	Date    string `json:"date"`    // Human-readable date (YYYY-MM-DD)
	HexDate string `json:"hexDate"` // Hex date for Google API (optional)
	Epoch   int    `json:"epoch"`   // Primary epoch from protobuf (optional)
}

// TimelapseOptions contains all options for timelapse video export
type TimelapseOptions struct {
	// Dimensions
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Preset  string   `json:"preset"`            // "instagram_square", "tiktok", "youtube", etc.
	Presets []string `json:"presets,omitempty"` // Multiple presets for batch export

	// Crop position (0.0-1.0, where 0.5 is center)
	CropX float64 `json:"cropX"` // 0=left, 0.5=center, 1=right
	CropY float64 `json:"cropY"` // 0=top, 0.5=center, 1=bottom

	// Spotlight area (geographic coordinates)
	SpotlightEnabled   bool    `json:"spotlightEnabled"`
	SpotlightCenterLat float64 `json:"spotlightCenterLat"`
	SpotlightCenterLon float64 `json:"spotlightCenterLon"`
	SpotlightRadiusKm  float64 `json:"spotlightRadiusKm"`

	// Overlay
	OverlayOpacity float64 `json:"overlayOpacity"` // 0.0 to 1.0

	// Date overlay
	ShowDateOverlay bool    `json:"showDateOverlay"`
	DateFontSize    float64 `json:"dateFontSize"`
	DatePosition    string  `json:"datePosition"` // "top-left", "top-right", "bottom-left", "bottom-right"

	// Logo overlay
	ShowLogo     bool   `json:"showLogo"`
	LogoPosition string `json:"logoPosition"` // "top-left", "top-right", "bottom-left", "bottom-right"

	// Video settings
	FrameDelay   float64 `json:"frameDelay"`   // Seconds between frames
	OutputFormat string  `json:"outputFormat"` // "mp4", "gif"
	Quality      int     `json:"quality"`      // 0-100
}

// SpotlightPixels represents pixel coordinates for spotlight area
type SpotlightPixels struct {
	X      int
	Y      int
	Width  int
	Height int
}

// ProgressCallback is called during video export to report progress
type ProgressCallback func(current, total int, percent int, status string)

// LogCallback is called to emit log messages
type LogCallback func(message string)

// ImageLoader loads images from file paths (typically GeoTIFFs or PNGs)
type ImageLoader func(path string) (image.Image, error)

// LogoLoader loads the logo image
type LogoLoader func() (image.Image, error)

// SpotlightCalculator calculates spotlight pixel coordinates from geographic coordinates
type SpotlightCalculator func(bbox BoundingBox, zoom int, centerLat, centerLon, radiusKm float64, imageBounds image.Rectangle) SpotlightPixels

// Manager handles timelapse video export orchestration
type Manager struct {
	downloadPath         string
	dateFontData         []byte
	progressCallback     ProgressCallback
	logCallback          LogCallback
	imageLoader          ImageLoader
	logoLoader           LogoLoader
	spotlightCalculator  SpotlightCalculator
}

// Config holds configuration for the video Manager
type Config struct {
	DownloadPath        string
	DateFontData        []byte               // Embedded font data for date overlay
	ProgressCallback    ProgressCallback
	LogCallback         LogCallback
	ImageLoader         ImageLoader
	LogoLoader          LogoLoader
	SpotlightCalculator SpotlightCalculator
}

// NewManager creates a new video export manager
func NewManager(cfg Config) *Manager {
	return &Manager{
		downloadPath:        cfg.DownloadPath,
		dateFontData:        cfg.DateFontData,
		progressCallback:    cfg.ProgressCallback,
		logCallback:         cfg.LogCallback,
		imageLoader:         cfg.ImageLoader,
		logoLoader:          cfg.LogoLoader,
		spotlightCalculator: cfg.SpotlightCalculator,
	}
}

// SetDownloadPath updates the download path (for task-specific exports)
func (m *Manager) SetDownloadPath(path string) {
	m.downloadPath = path
}

// GetDownloadPath returns the current download path
func (m *Manager) GetDownloadPath() string {
	return m.downloadPath
}

// emitLog sends a log message via callback if available
func (m *Manager) emitLog(message string) {
	if m.logCallback != nil {
		m.logCallback(message)
	} else {
		log.Println(message)
	}
}

// emitProgress sends progress update via callback if available
func (m *Manager) emitProgress(current, total, percent int, status string) {
	if m.progressCallback != nil {
		m.progressCallback(current, total, percent, status)
	}
}

// ExportTimelapse exports a timelapse video from downloaded imagery
func (m *Manager) ExportTimelapse(bbox BoundingBox, zoom int, dates []DateInfo, source string, opts TimelapseOptions) error {
	return m.exportTimelapseInternal(bbox, zoom, dates, source, opts, true)
}

// ExportTimelapseNoOpen exports a timelapse video without opening the folder (for batch exports)
func (m *Manager) ExportTimelapseNoOpen(bbox BoundingBox, zoom int, dates []DateInfo, source string, opts TimelapseOptions) error {
	return m.exportTimelapseInternal(bbox, zoom, dates, source, opts, false)
}

// exportTimelapseInternal is the internal implementation with option to skip opening folder
func (m *Manager) exportTimelapseInternal(bbox BoundingBox, zoom int, dates []DateInfo, source string, opts TimelapseOptions, openFolder bool) error {
	log.Printf("=== ExportTimelapse CALLED ===")
	log.Printf("Parameters: bbox=%+v, zoom=%d, source=%s, dateCount=%d", bbox, zoom, source, len(dates))
	log.Printf("Options: %+v", opts)

	if len(dates) == 0 {
		log.Printf("ERROR: No dates provided to ExportTimelapse")
		return fmt.Errorf("no dates provided")
	}

	log.Printf("[VideoExport] Starting timelapse video export for %d dates", len(dates))
	log.Printf("[VideoExport] Source: %s, Zoom: %d", source, zoom)
	m.emitLog(fmt.Sprintf("Starting timelapse video export for %d dates", len(dates)))
	m.emitLog(fmt.Sprintf("Source: %s, Zoom: %d", source, zoom))

	// Get download directory
	downloadDir := m.downloadPath
	log.Printf("[VideoExport] Download directory: %s", downloadDir)
	m.emitLog(fmt.Sprintf("Download directory: %s", downloadDir))

	// Prepare video export options
	var preset SocialMediaPreset
	switch opts.Preset {
	case "instagram_square":
		preset = PresetInstagramSquare
	case "instagram_portrait":
		preset = PresetInstagramPortrait
	case "instagram_story":
		preset = PresetInstagramStory
	case "instagram_reel":
		preset = PresetInstagramReel
	case "tiktok":
		preset = PresetTikTok
	case "youtube":
		preset = PresetYouTube
	case "youtube_shorts":
		preset = PresetYouTubeShorts
	case "twitter":
		preset = PresetTwitter
	case "facebook":
		preset = PresetFacebook
	default:
		preset = PresetCustom
	}

	// Get dimensions from preset or custom
	width, height := opts.Width, opts.Height
	if preset != PresetCustom {
		width, height = GetPresetDimensions(preset)
	}

	// Default crop position to center if not specified
	cropX := opts.CropX
	cropY := opts.CropY
	if cropX == 0 && cropY == 0 {
		cropX = 0.5
		cropY = 0.5
	}

	exportOpts := &ExportOptions{
		Width:           width,
		Height:          height,
		Preset:          preset,
		CropX:           cropX,
		CropY:           cropY,
		UseSpotlight:    opts.SpotlightEnabled,
		OverlayOpacity:  opts.OverlayOpacity,
		OverlayColor:    DefaultExportOptions().OverlayColor, // Use default black
		ShowDateOverlay: opts.ShowDateOverlay,
		DateFontSize:    opts.DateFontSize,
		DatePosition:    opts.DatePosition,
		DateColor:       DefaultExportOptions().DateColor, // Use default white
		DateShadow:      true,
		DateFormat:      "Jan 02, 2006",
		DateFontData:    m.dateFontData, // Use embedded Arial Unicode font
		ShowLogo:        opts.ShowLogo,
		LogoPosition:    opts.LogoPosition,
		LogoScale:       0.6,
		FrameRate:       30,
		FrameDelay:      opts.FrameDelay,
		OutputFormat:    opts.OutputFormat,
		Quality:         opts.Quality,
		UseH264:         true, // Try to use H.264 if FFmpeg is available
	}

	// Load logo image if enabled
	if opts.ShowLogo && m.logoLoader != nil {
		logoImg, err := m.logoLoader()
		if err != nil {
			log.Printf("[VideoExport] Warning: Failed to load logo: %v", err)
		} else {
			exportOpts.LogoImage = logoImg
			log.Printf("[VideoExport] Logo image loaded")
		}
	}

	// If spotlight is enabled, calculate pixel coordinates from geographic coordinates
	if opts.SpotlightEnabled {
		m.emitLog("Spotlight mode enabled - will calculate coordinates from first frame")
	}

	// Create video exporter
	log.Printf("[VideoExport] Creating video exporter...")
	exporter, err := NewExporter(exportOpts)
	if err != nil {
		log.Printf("[VideoExport] ERROR: Failed to create video exporter: %v", err)
		return fmt.Errorf("failed to create video exporter: %w", err)
	}
	defer exporter.Close()
	log.Printf("[VideoExport] Video exporter created successfully")

	// Load frames from GeoTIFFs
	frames := make([]Frame, 0, len(dates))
	log.Printf("[VideoExport] Starting frame loading loop for %d dates", len(dates))

	for i, dateInfo := range dates {
		log.Printf("[VideoExport] Processing date %d/%d: %s", i+1, len(dates), dateInfo.Date)
		m.emitProgress(i, len(dates), (i*100)/len(dates), fmt.Sprintf("Loading frame %d/%d: %s", i+1, len(dates), dateInfo.Date))

		// Construct GeoTIFF path using same generateGeoTIFFFilename function as downloads
		// Provider constants now match filename prefixes directly
		filename := naming.GenerateGeoTIFFFilename(source, dateInfo.Date, bbox.South, bbox.West, bbox.North, bbox.East, zoom)
		basePath := filepath.Join(downloadDir, filename)

		// Try loading PNG first (created as sidecar for better compatibility)
		imagePath := strings.TrimSuffix(basePath, ".tif") + ".png"
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			// Fallback to GeoTIFF if PNG not found
			imagePath = basePath
		}

		log.Printf("[VideoExport] Looking for frame: %s", imagePath)
		m.emitLog(fmt.Sprintf("Looking for frame: %s", imagePath))

		// Check if file exists
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			log.Printf("[VideoExport] ❌ Frame not found for %s: %s", dateInfo.Date, imagePath)
			m.emitLog(fmt.Sprintf("❌ Frame not found for %s: %s", dateInfo.Date, imagePath))
			continue
		}

		log.Printf("[VideoExport] ✅ Found frame for %s", dateInfo.Date)
		m.emitLog(fmt.Sprintf("✅ Found frame for %s", dateInfo.Date))

		// Load image using provided loader
		log.Printf("[VideoExport] Attempting to load image from: %s", imagePath)
		var img image.Image
		if m.imageLoader != nil {
			img, err = m.imageLoader(imagePath)
		} else {
			// Fallback to direct file loading if no loader provided
			f, openErr := os.Open(imagePath)
			if openErr != nil {
				err = openErr
			} else {
				defer f.Close()
				img, _, err = image.Decode(f)
			}
		}

		if err != nil {
			log.Printf("[VideoExport] ❌ ERROR: Failed to load image for %s: %v", dateInfo.Date, err)
			m.emitLog(fmt.Sprintf("Failed to load image for %s: %v", dateInfo.Date, err))
			continue
		}
		log.Printf("[VideoExport] ✅ Successfully loaded image for %s", dateInfo.Date)

		// Convert to RGBA if needed
		var rgba *image.RGBA
		if rgbaImg, ok := img.(*image.RGBA); ok {
			rgba = rgbaImg
		} else {
			bounds := img.Bounds()
			rgba = image.NewRGBA(bounds)
			draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)
		}

		// Calculate spotlight coordinates from geographic coordinates on first frame
		if opts.SpotlightEnabled && i == 0 && m.spotlightCalculator != nil {
			spotlightPixels := m.spotlightCalculator(
				bbox, zoom,
				opts.SpotlightCenterLat, opts.SpotlightCenterLon,
				opts.SpotlightRadiusKm,
				rgba.Bounds(),
			)
			exportOpts.SpotlightX = spotlightPixels.X
			exportOpts.SpotlightY = spotlightPixels.Y
			exportOpts.SpotlightWidth = spotlightPixels.Width
			exportOpts.SpotlightHeight = spotlightPixels.Height
			m.emitLog(fmt.Sprintf("Spotlight area: x=%d y=%d w=%d h=%d",
				spotlightPixels.X, spotlightPixels.Y, spotlightPixels.Width, spotlightPixels.Height))
		}

		// Parse date
		parsedDate, err := time.Parse("2006-01-02", dateInfo.Date)
		if err != nil {
			m.emitLog(fmt.Sprintf("Failed to parse date %s: %v", dateInfo.Date, err))
			parsedDate = time.Now()
		}

		frames = append(frames, Frame{
			Image: rgba,
			Date:  parsedDate,
		})
	}

	log.Printf("[VideoExport] Total frames loaded: %d", len(frames))
	m.emitLog(fmt.Sprintf("Total frames loaded: %d", len(frames)))

	if len(frames) == 0 {
		log.Printf("[VideoExport] ❌ ERROR: No frames loaded - ensure GeoTIFFs are downloaded first")
		m.emitLog("❌ ERROR: No frames loaded - ensure GeoTIFFs are downloaded first")
		return fmt.Errorf("no frames loaded - ensure GeoTIFFs are downloaded first")
	}

	log.Printf("[VideoExport] ✅ Loaded %d frames successfully, starting video encoding...", len(frames))
	m.emitLog(fmt.Sprintf("✅ Loaded %d frames successfully, starting video encoding...", len(frames)))

	// Generate output filename
	outputFilename := fmt.Sprintf("%s_timelapse_%s_to_%s_%s.%s",
		source,
		dates[0].Date,
		dates[len(dates)-1].Date,
		opts.Preset,
		opts.OutputFormat,
	)
	outputPath := filepath.Join(downloadDir, "timelapse_exports", outputFilename)

	// Create output directory
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Export video
	m.emitProgress(len(frames), len(frames), 99, "Encoding video...")

	if err := exporter.ExportVideo(frames, outputPath); err != nil {
		return fmt.Errorf("failed to export video: %w", err)
	}

	m.emitLog(fmt.Sprintf("Video exported successfully: %s", outputPath))

	// Emit completion
	m.emitProgress(len(frames), len(frames), 100, fmt.Sprintf("Video export complete: %s", filepath.Base(outputPath)))

	return nil
}
