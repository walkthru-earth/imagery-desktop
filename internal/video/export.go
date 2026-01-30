package video

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"os"
	"time"

	"github.com/icza/mjpeg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// SocialMediaPreset defines common social media video dimensions
type SocialMediaPreset string

const (
	PresetCustom           SocialMediaPreset = "custom"
	PresetInstagramSquare  SocialMediaPreset = "instagram_square"   // 1080x1080
	PresetInstagramPortrait SocialMediaPreset = "instagram_portrait" // 1080x1350
	PresetInstagramStory   SocialMediaPreset = "instagram_story"    // 1080x1920
	PresetTikTok           SocialMediaPreset = "tiktok"             // 1080x1920
	PresetYouTube          SocialMediaPreset = "youtube"            // 1920x1080
	PresetYouTubeShorts    SocialMediaPreset = "youtube_shorts"     // 1080x1920
	PresetTwitter          SocialMediaPreset = "twitter"            // 1280x720
	PresetFacebook         SocialMediaPreset = "facebook"           // 1280x720
)

// GetPresetDimensions returns width and height for a preset
func GetPresetDimensions(preset SocialMediaPreset) (int, int) {
	switch preset {
	case PresetInstagramSquare:
		return 1080, 1080
	case PresetInstagramPortrait:
		return 1080, 1350
	case PresetInstagramStory, PresetTikTok, PresetYouTubeShorts:
		return 1080, 1920
	case PresetYouTube:
		return 1920, 1080
	case PresetTwitter, PresetFacebook:
		return 1280, 720
	default:
		return 1920, 1080 // Default to YouTube
	}
}

// ExportOptions contains all options for video export
type ExportOptions struct {
	// Dimensions
	Width  int
	Height int
	Preset SocialMediaPreset

	// Spotlight area (pixel coordinates in source image)
	SpotlightX      int
	SpotlightY      int
	SpotlightWidth  int
	SpotlightHeight int
	UseSpotlight    bool

	// Overlay
	OverlayOpacity float64 // 0.0 to 1.0 (0 = transparent, 1 = opaque)
	OverlayColor   color.RGBA

	// Date overlay
	ShowDateOverlay  bool
	DateFontSize     float64
	DatePosition     string // "top-left", "top-right", "bottom-left", "bottom-right", "center"
	DateColor        color.RGBA
	DateShadow       bool
	DateFormat       string // e.g., "2006-01-02", "Jan 02, 2006"
	DateFontPath     string // Path to Quicksand font

	// Video settings
	FrameRate    int    // FPS (e.g., 30, 24, 15)
	FrameDelay   float64 // Seconds between frames (e.g., 0.5 = 2 images per second)
	OutputFormat string // "mp4", "gif", "webm"
	Quality      int    // 0-100 (for lossy formats)

	// Metadata
	Title       string
	Description string
}

// DefaultExportOptions returns sensible defaults
func DefaultExportOptions() *ExportOptions {
	return &ExportOptions{
		Width:           1920,
		Height:          1080,
		Preset:          PresetYouTube,
		UseSpotlight:    false,
		OverlayOpacity:  0.6,
		OverlayColor:    color.RGBA{0, 0, 0, 255},
		ShowDateOverlay: true,
		DateFontSize:    48,
		DatePosition:    "bottom-right",
		DateColor:       color.RGBA{255, 255, 255, 255},
		DateShadow:      true,
		DateFormat:      "Jan 02, 2006",
		FrameRate:       30,
		FrameDelay:      0.5,
		OutputFormat:    "mp4",
		Quality:         90,
	}
}

// Frame represents a single frame in the timelapse
type Frame struct {
	Image *image.RGBA
	Date  time.Time
}

// Exporter handles video export operations
type Exporter struct {
	options *ExportOptions
	font    font.Face
}

// NewExporter creates a new video exporter
func NewExporter(opts *ExportOptions) (*Exporter, error) {
	e := &Exporter{
		options: opts,
	}

	// Load font if date overlay is enabled
	if opts.ShowDateOverlay && opts.DateFontPath != "" {
		if err := e.loadFont(); err != nil {
			return nil, fmt.Errorf("failed to load font: %w", err)
		}
	}

	return e, nil
}

// loadFont loads the Quicksand font for date overlay
func (e *Exporter) loadFont() error {
	fontBytes, err := os.ReadFile(e.options.DateFontPath)
	if err != nil {
		return fmt.Errorf("failed to read font file: %w", err)
	}

	f, err := opentype.Parse(fontBytes)
	if err != nil {
		return fmt.Errorf("failed to parse font: %w", err)
	}

	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    e.options.DateFontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return fmt.Errorf("failed to create font face: %w", err)
	}

	e.font = face
	return nil
}

// ProcessFrame processes a single frame: crops, applies spotlight, adds date
func (e *Exporter) ProcessFrame(sourceImage image.Image, date time.Time) (*image.RGBA, error) {
	opts := e.options

	// Create output image
	output := image.NewRGBA(image.Rect(0, 0, opts.Width, opts.Height))

	// Step 1: Draw the base image (cropped or full)
	if opts.UseSpotlight {
		// Draw grayed out full image first
		e.drawGrayedImage(output, sourceImage)

		// Then draw the spotlight area at full brightness
		e.drawSpotlightArea(output, sourceImage)
	} else {
		// Just resize/crop the source image to fit output dimensions
		e.resizeAndDrawImage(output, sourceImage)
	}

	// Step 2: Add date overlay if enabled
	if opts.ShowDateOverlay && e.font != nil {
		e.drawDateOverlay(output, date)
	}

	return output, nil
}

// drawGrayedImage draws the entire source image grayed out with overlay
func (e *Exporter) drawGrayedImage(dst *image.RGBA, src image.Image) {
	bounds := src.Bounds()
	dstBounds := dst.Bounds()

	// Calculate scaling to fit source into destination
	scaleX := float64(dstBounds.Dx()) / float64(bounds.Dx())
	scaleY := float64(dstBounds.Dy()) / float64(bounds.Dy())
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	// Draw scaled source image
	for dy := dstBounds.Min.Y; dy < dstBounds.Max.Y; dy++ {
		for dx := dstBounds.Min.X; dx < dstBounds.Max.X; dx++ {
			sx := int(float64(dx) / scale)
			sy := int(float64(dy) / scale)

			if sx >= bounds.Min.X && sx < bounds.Max.X && sy >= bounds.Min.Y && sy < bounds.Max.Y {
				c := src.At(sx, sy)
				r, g, b, a := c.RGBA()

				// Convert to grayscale
				gray := uint32(0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b))

				// Apply overlay
				opacity := uint32(e.options.OverlayOpacity * 65535)
				overlayR := uint32(e.options.OverlayColor.R) * 257
				overlayG := uint32(e.options.OverlayColor.G) * 257
				overlayB := uint32(e.options.OverlayColor.B) * 257

				finalR := (gray*(65535-opacity) + overlayR*opacity) / 65535
				finalG := (gray*(65535-opacity) + overlayG*opacity) / 65535
				finalB := (gray*(65535-opacity) + overlayB*opacity) / 65535

				dst.Set(dx, dy, color.RGBA64{
					R: uint16(finalR),
					G: uint16(finalG),
					B: uint16(finalB),
					A: uint16(a),
				})
			}
		}
	}
}

// drawSpotlightArea draws the spotlight area at full brightness
func (e *Exporter) drawSpotlightArea(dst *image.RGBA, src image.Image) {
	opts := e.options

	// Calculate destination rectangle for spotlight
	// Center the spotlight in the output
	dstX := (opts.Width - opts.SpotlightWidth) / 2
	dstY := (opts.Height - opts.SpotlightHeight) / 2

	// Draw spotlight region
	for dy := 0; dy < opts.SpotlightHeight; dy++ {
		for dx := 0; dx < opts.SpotlightWidth; dx++ {
			sx := opts.SpotlightX + dx
			sy := opts.SpotlightY + dy

			dstPx := dstX + dx
			dstPy := dstY + dy

			if dstPx >= 0 && dstPx < opts.Width && dstPy >= 0 && dstPy < opts.Height {
				c := src.At(sx, sy)
				dst.Set(dstPx, dstPy, c)
			}
		}
	}
}

// resizeAndDrawImage resizes source to fit destination
func (e *Exporter) resizeAndDrawImage(dst *image.RGBA, src image.Image) {
	bounds := src.Bounds()
	dstBounds := dst.Bounds()

	// Simple nearest-neighbor scaling (fast, good enough for video)
	scaleX := float64(bounds.Dx()) / float64(dstBounds.Dx())
	scaleY := float64(bounds.Dy()) / float64(dstBounds.Dy())

	for dy := dstBounds.Min.Y; dy < dstBounds.Max.Y; dy++ {
		for dx := dstBounds.Min.X; dx < dstBounds.Max.X; dx++ {
			sx := bounds.Min.X + int(float64(dx-dstBounds.Min.X)*scaleX)
			sy := bounds.Min.Y + int(float64(dy-dstBounds.Min.Y)*scaleY)

			if sx >= bounds.Min.X && sx < bounds.Max.X && sy >= bounds.Min.Y && sy < bounds.Max.Y {
				dst.Set(dx, dy, src.At(sx, sy))
			}
		}
	}
}

// drawDateOverlay draws the date text on the frame
func (e *Exporter) drawDateOverlay(dst *image.RGBA, date time.Time) {
	if e.font == nil {
		return
	}

	dateStr := date.Format(e.options.DateFormat)

	// Measure text
	drawer := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(e.options.DateColor),
		Face: e.font,
	}

	bounds, _ := drawer.BoundString(dateStr)
	textWidth := (bounds.Max.X - bounds.Min.X).Ceil()
	textHeight := (bounds.Max.Y - bounds.Min.Y).Ceil()

	// Calculate position
	var x, y int
	padding := 20

	switch e.options.DatePosition {
	case "top-left":
		x = padding
		y = padding + textHeight
	case "top-right":
		x = e.options.Width - textWidth - padding
		y = padding + textHeight
	case "bottom-left":
		x = padding
		y = e.options.Height - padding
	case "bottom-right":
		x = e.options.Width - textWidth - padding
		y = e.options.Height - padding
	case "center":
		x = (e.options.Width - textWidth) / 2
		y = (e.options.Height + textHeight) / 2
	default:
		x = e.options.Width - textWidth - padding
		y = e.options.Height - padding
	}

	// Draw shadow if enabled
	if e.options.DateShadow {
		shadowDrawer := &font.Drawer{
			Dst:  dst,
			Src:  image.NewUniform(color.RGBA{0, 0, 0, 180}),
			Face: e.font,
			Dot:  fixed.P(x+2, y+2),
		}
		shadowDrawer.DrawString(dateStr)
	}

	// Draw text
	drawer.Dot = fixed.P(x, y)
	drawer.DrawString(dateStr)
}

// ExportVideo creates a video from processed frames using native Go libraries
func (e *Exporter) ExportVideo(frames []Frame, outputPath string) error {
	opts := e.options

	switch opts.OutputFormat {
	case "mp4", "avi":
		return e.exportMotionJPEG(frames, outputPath)
	case "gif":
		return e.exportGIF(frames, outputPath)
	default:
		return fmt.Errorf("unsupported output format: %s (supported: mp4, avi, gif)", opts.OutputFormat)
	}
}

// exportMotionJPEG creates an AVI file with Motion JPEG codec (compatible, plays everywhere)
func (e *Exporter) exportMotionJPEG(frames []Frame, outputPath string) error {
	if len(frames) == 0 {
		return fmt.Errorf("no frames to export")
	}

	// Create MJPEG writer
	// Note: MJPEG AVI files are widely compatible and don't require external encoders
	writer, err := mjpeg.New(outputPath, int32(e.options.Width), int32(e.options.Height), int32(e.options.FrameRate))
	if err != nil {
		return fmt.Errorf("failed to create video writer: %w", err)
	}
	defer writer.Close()

	// Process and write each frame
	for i, frame := range frames {
		processedFrame, err := e.ProcessFrame(frame.Image, frame.Date)
		if err != nil {
			return fmt.Errorf("failed to process frame %d: %w", i, err)
		}

		// Encode frame as JPEG
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, processedFrame, &jpeg.Options{Quality: e.options.Quality}); err != nil {
			return fmt.Errorf("failed to encode frame %d as JPEG: %w", i, err)
		}

		// Add frame to video
		if err := writer.AddFrame(buf.Bytes()); err != nil {
			return fmt.Errorf("failed to add frame %d: %w", i, err)
		}
	}

	return nil
}

// exportGIF creates an animated GIF
func (e *Exporter) exportGIF(frames []Frame, outputPath string) error {
	if len(frames) == 0 {
		return fmt.Errorf("no frames to export")
	}

	// Process all frames
	palettedImages := make([]*image.Paletted, 0, len(frames))
	delays := make([]int, 0, len(frames))

	// Calculate delay in 100ths of a second
	delay := int(e.options.FrameDelay * 100)
	if delay < 1 {
		delay = 1
	}

	for i, frame := range frames {
		processedFrame, err := e.ProcessFrame(frame.Image, frame.Date)
		if err != nil {
			return fmt.Errorf("failed to process frame %d: %w", i, err)
		}

		// Convert to paletted image for GIF
		bounds := processedFrame.Bounds()
		palettedImg := image.NewPaletted(bounds, nil)

		// Use Floyd-Steinberg dithering for better quality
		draw.FloydSteinberg.Draw(palettedImg, bounds, processedFrame, image.Point{})

		palettedImages = append(palettedImages, palettedImg)
		delays = append(delays, delay)
	}

	// Create output file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	// Encode as animated GIF
	return gif.EncodeAll(f, &gif.GIF{
		Image: palettedImages,
		Delay: delays,
		Config: image.Config{
			Width:  e.options.Width,
			Height: e.options.Height,
		},
	})
}

// Close releases resources
func (e *Exporter) Close() error {
	if e.font != nil {
		return e.font.Close()
	}
	return nil
}
