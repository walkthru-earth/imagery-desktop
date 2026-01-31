package video

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/icza/mjpeg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// SocialMediaPreset defines common social media video dimensions
type SocialMediaPreset string

const (
	PresetCustom            SocialMediaPreset = "custom"
	PresetInstagramSquare   SocialMediaPreset = "instagram_square"   // 1080x1080
	PresetInstagramPortrait SocialMediaPreset = "instagram_portrait" // 1080x1350
	PresetInstagramStory    SocialMediaPreset = "instagram_story"    // 1080x1920
	PresetInstagramReel     SocialMediaPreset = "instagram_reel"     // 1080x1920
	PresetTikTok            SocialMediaPreset = "tiktok"             // 1080x1920
	PresetYouTube           SocialMediaPreset = "youtube"            // 1920x1080
	PresetYouTubeShorts     SocialMediaPreset = "youtube_shorts"     // 1080x1920
	PresetTwitter           SocialMediaPreset = "twitter"            // 1280x720
	PresetFacebook          SocialMediaPreset = "facebook"           // 1280x720
)

// GetPresetDimensions returns width and height for a preset
func GetPresetDimensions(preset SocialMediaPreset) (int, int) {
	switch preset {
	case PresetInstagramSquare:
		return 1080, 1080
	case PresetInstagramPortrait:
		return 1080, 1350
	case PresetInstagramStory, PresetInstagramReel, PresetTikTok, PresetYouTubeShorts:
		return 1080, 1920
	case PresetYouTube:
		return 1920, 1080
	case PresetTwitter, PresetFacebook:
		return 1280, 720
	default:
		return 1920, 1080 // Default to YouTube
	}
}

// GetPresetLabel returns a human-readable label for a preset
func GetPresetLabel(preset SocialMediaPreset) string {
	switch preset {
	case PresetInstagramSquare:
		return "Instagram Square (1080×1080)"
	case PresetInstagramPortrait:
		return "Instagram Portrait (1080×1350)"
	case PresetInstagramStory:
		return "Instagram Story (1080×1920)"
	case PresetInstagramReel:
		return "Instagram Reel (1080×1920)"
	case PresetTikTok:
		return "TikTok (1080×1920)"
	case PresetYouTube:
		return "YouTube (1920×1080)"
	case PresetYouTubeShorts:
		return "YouTube Shorts (1080×1920)"
	case PresetTwitter:
		return "Twitter/X (1280×720)"
	case PresetFacebook:
		return "Facebook (1280×720)"
	case PresetCustom:
		return "Custom"
	default:
		return "YouTube (1920×1080)"
	}
}

// ExportOptions contains all options for video export
type ExportOptions struct {
	// Dimensions
	Width  int
	Height int
	Preset SocialMediaPreset

	// Crop position (0.0-1.0, where 0.5 is center)
	// CropX controls horizontal position: 0=left edge, 0.5=center, 1.0=right edge
	// CropY controls vertical position: 0=top, 0.5=center, 1.0=bottom
	CropX float64
	CropY float64

	// Spotlight area (pixel coordinates in source image) - for grayout effect
	SpotlightX      int
	SpotlightY      int
	SpotlightWidth  int
	SpotlightHeight int
	UseSpotlight    bool

	// Overlay
	OverlayOpacity float64 // 0.0 to 1.0 (0 = transparent, 1 = opaque)
	OverlayColor   color.RGBA

	// Date overlay
	ShowDateOverlay bool
	DateFontSize    float64
	DatePosition    string // "top-left", "top-right", "bottom-left", "bottom-right", "center"
	DateColor       color.RGBA
	DateShadow      bool
	DateFormat      string // e.g., "2006-01-02", "Jan 02, 2006"
	DateFontPath    string // Path to font file

	// Logo overlay
	ShowLogo     bool
	LogoPosition string // "top-left", "top-right", "bottom-left", "bottom-right"
	LogoImage    image.Image
	LogoScale    float64 // Scale factor for logo (default 1.0)

	// Video settings
	FrameRate    int     // FPS (e.g., 30, 24, 15)
	FrameDelay   float64 // Seconds between frames (e.g., 0.5 = 2 images per second)
	OutputFormat string  // "mp4", "gif", "avi"
	Quality      int     // 0-100 (for lossy formats)
	UseH264      bool    // Try to use H.264 encoding via FFmpeg

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
		CropX:           0.5, // Center horizontally
		CropY:           0.5, // Center vertically
		UseSpotlight:    false,
		OverlayOpacity:  0.6,
		OverlayColor:    color.RGBA{0, 0, 0, 255},
		ShowDateOverlay: true,
		DateFontSize:    48,
		DatePosition:    "bottom-right",
		DateColor:       color.RGBA{255, 255, 255, 255},
		DateShadow:      true,
		DateFormat:      "Jan 02, 2006",
		ShowLogo:        true,
		LogoPosition:    "bottom-left",
		LogoScale:       1.0,
		FrameRate:       30,
		FrameDelay:      0.5,
		OutputFormat:    "mp4",
		Quality:         90,
		UseH264:         true,
	}
}

// Frame represents a single frame in the timelapse
type Frame struct {
	Image *image.RGBA
	Date  time.Time
}

// Exporter handles video export operations
type Exporter struct {
	options    *ExportOptions
	font       font.Face
	ffmpegPath string
}

// CheckFFmpeg checks if FFmpeg is available - first checks bundled, then system
func CheckFFmpeg() (string, bool) {
	// First, check for bundled FFmpeg relative to executable
	bundledPath := getBundledFFmpegPath()
	if bundledPath != "" {
		if _, err := os.Stat(bundledPath); err == nil {
			return bundledPath, true
		}
	}

	// Then try system PATH
	names := []string{"ffmpeg"}
	if runtime.GOOS == "windows" {
		names = []string{"ffmpeg.exe", "ffmpeg"}
	}

	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, true
		}
	}

	// Check common installation directories
	commonPaths := []string{}
	switch runtime.GOOS {
	case "darwin":
		commonPaths = []string{
			"/usr/local/bin/ffmpeg",
			"/opt/homebrew/bin/ffmpeg",
			"/opt/local/bin/ffmpeg",
		}
	case "linux":
		commonPaths = []string{
			"/usr/bin/ffmpeg",
			"/usr/local/bin/ffmpeg",
		}
	case "windows":
		commonPaths = []string{
			"C:\\ffmpeg\\bin\\ffmpeg.exe",
			"C:\\Program Files\\ffmpeg\\bin\\ffmpeg.exe",
		}
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}

	return "", false
}

// getBundledFFmpegPath returns the path to bundled FFmpeg based on OS and executable location
func getBundledFFmpegPath() string {
	// Get the executable path
	execPath, err := os.Executable()
	if err != nil {
		return ""
	}
	execDir := filepath.Dir(execPath)

	switch runtime.GOOS {
	case "darwin":
		// On macOS, the app bundle structure is:
		// MyApp.app/Contents/MacOS/MyApp (executable)
		// MyApp.app/Contents/Resources/ffmpeg (bundled ffmpeg)
		// Also check for development mode where FFmpeg is in project root
		possiblePaths := []string{
			filepath.Join(execDir, "..", "Resources", "ffmpeg"),
			filepath.Join(execDir, "ffmpeg"),
			filepath.Join(execDir, "..", "..", "..", "FFmpeg", "ffmpeg"), // Dev mode
		}
		for _, p := range possiblePaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "windows":
		// On Windows, ffmpeg.exe is next to the executable
		possiblePaths := []string{
			filepath.Join(execDir, "ffmpeg.exe"),
			filepath.Join(execDir, "FFmpeg", "ffmpeg.exe"),
		}
		for _, p := range possiblePaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "linux":
		// On Linux, ffmpeg is next to the executable or in lib folder
		possiblePaths := []string{
			filepath.Join(execDir, "ffmpeg"),
			filepath.Join(execDir, "lib", "ffmpeg"),
			filepath.Join(execDir, "FFmpeg", "ffmpeg"),
		}
		for _, p := range possiblePaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	return ""
}

// NewExporter creates a new video exporter
func NewExporter(opts *ExportOptions) (*Exporter, error) {
	e := &Exporter{
		options: opts,
	}

	// Check for FFmpeg if H.264 is requested
	if opts.UseH264 {
		path, found := CheckFFmpeg()
		if found {
			e.ffmpegPath = path
			log.Printf("[VideoExport] FFmpeg found at: %s", path)
		} else {
			log.Printf("[VideoExport] FFmpeg not found, will use fallback encoder")
		}
	}

	// Load font if date overlay is enabled
	if opts.ShowDateOverlay && opts.DateFontPath != "" {
		if err := e.loadFont(); err != nil {
			log.Printf("[VideoExport] Warning: failed to load font: %v", err)
			// Don't fail - continue without date overlay
		}
	}

	return e, nil
}

// HasFFmpeg returns true if FFmpeg is available
func (e *Exporter) HasFFmpeg() bool {
	return e.ffmpegPath != ""
}

// loadFont loads the font for date overlay
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

	// Step 3: Add logo overlay if enabled
	if opts.ShowLogo && opts.LogoImage != nil {
		e.drawLogoOverlay(output)
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

// drawLogoOverlay draws the logo on the frame
func (e *Exporter) drawLogoOverlay(dst *image.RGBA) {
	if e.options.LogoImage == nil {
		return
	}

	logoImg := e.options.LogoImage
	logoBounds := logoImg.Bounds()
	logoWidth := logoBounds.Dx()
	logoHeight := logoBounds.Dy()

	// Scale logo to reasonable size (max 10% of video height)
	maxHeight := e.options.Height / 10
	scale := e.options.LogoScale
	if logoHeight > maxHeight {
		scale = float64(maxHeight) / float64(logoHeight)
	}

	scaledWidth := int(float64(logoWidth) * scale)
	scaledHeight := int(float64(logoHeight) * scale)

	// Calculate position
	var x, y int
	padding := 20

	switch e.options.LogoPosition {
	case "top-left":
		x = padding
		y = padding
	case "top-right":
		x = e.options.Width - scaledWidth - padding
		y = padding
	case "bottom-left":
		x = padding
		y = e.options.Height - scaledHeight - padding
	case "bottom-right":
		x = e.options.Width - scaledWidth - padding
		y = e.options.Height - scaledHeight - padding
	case "center":
		x = (e.options.Width - scaledWidth) / 2
		y = (e.options.Height - scaledHeight) / 2
	default:
		x = padding
		y = e.options.Height - scaledHeight - padding
	}

	// Draw scaled logo with alpha blending
	for dy := 0; dy < scaledHeight; dy++ {
		for dx := 0; dx < scaledWidth; dx++ {
			// Source pixel (scaled)
			sx := int(float64(dx) / scale)
			sy := int(float64(dy) / scale)
			if sx >= logoWidth {
				sx = logoWidth - 1
			}
			if sy >= logoHeight {
				sy = logoHeight - 1
			}

			srcColor := logoImg.At(logoBounds.Min.X+sx, logoBounds.Min.Y+sy)
			sr, sg, sb, sa := srcColor.RGBA()

			// Skip fully transparent pixels
			if sa == 0 {
				continue
			}

			dstX := x + dx
			dstY := y + dy
			if dstX < 0 || dstX >= e.options.Width || dstY < 0 || dstY >= e.options.Height {
				continue
			}

			// Alpha blending
			if sa == 65535 {
				// Fully opaque - just copy
				dst.Set(dstX, dstY, srcColor)
			} else {
				// Blend with destination
				dstColor := dst.At(dstX, dstY)
				dr, dg, db, da := dstColor.RGBA()

				// Standard alpha blending
				outA := sa + (da * (65535 - sa) / 65535)
				if outA > 0 {
					outR := (sr*sa + dr*(65535-sa)) / 65535
					outG := (sg*sa + dg*(65535-sa)) / 65535
					outB := (sb*sa + db*(65535-sa)) / 65535
					dst.Set(dstX, dstY, color.RGBA64{
						R: uint16(outR),
						G: uint16(outG),
						B: uint16(outB),
						A: uint16(outA),
					})
				}
			}
		}
	}
}

// ExportVideo creates a video from processed frames
func (e *Exporter) ExportVideo(frames []Frame, outputPath string) error {
	opts := e.options

	switch opts.OutputFormat {
	case "mp4":
		if e.ffmpegPath != "" && opts.UseH264 {
			return e.exportH264(frames, outputPath)
		}
		// Fallback to MJPEG AVI
		aviPath := strings.TrimSuffix(outputPath, ".mp4") + ".avi"
		log.Printf("[VideoExport] FFmpeg not available, falling back to MJPEG AVI: %s", aviPath)
		return e.exportMotionJPEG(frames, aviPath)
	case "avi":
		return e.exportMotionJPEG(frames, outputPath)
	case "gif":
		return e.exportGIF(frames, outputPath)
	default:
		return fmt.Errorf("unsupported output format: %s (supported: mp4, avi, gif)", opts.OutputFormat)
	}
}

// exportH264 creates an MP4 file with H.264 codec using FFmpeg
// It uses FFmpeg's scale and crop filters to properly handle aspect ratio
func (e *Exporter) exportH264(frames []Frame, outputPath string) error {
	if len(frames) == 0 {
		return fmt.Errorf("no frames to export")
	}

	log.Printf("[VideoExport] Exporting H.264 video with %d frames to %dx%d", len(frames), e.options.Width, e.options.Height)

	// Create temporary directory for frames
	tempDir, err := os.MkdirTemp("", "timelapse_frames_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	log.Printf("[VideoExport] Temp directory created: %s", tempDir)

	// Calculate how many times to duplicate each frame based on frame delay
	duplicateCount := int(e.options.FrameDelay * float64(e.options.FrameRate))
	if duplicateCount < 1 {
		duplicateCount = 1
	}

	log.Printf("[VideoExport] Frame duplication count: %d (frameDelay=%.2f, frameRate=%d)",
		duplicateCount, e.options.FrameDelay, e.options.FrameRate)

	// Process and save frames as PNG with date/logo overlays
	// ProcessFrame handles resizing, cropping, and adding overlays
	frameIndex := 0
	for i, frame := range frames {
		log.Printf("[VideoExport] Processing frame %d/%d", i+1, len(frames))

		// Process frame to add date/logo overlays and resize to target dimensions
		processedFrame, err := e.ProcessFrame(frame.Image, frame.Date)
		if err != nil {
			return fmt.Errorf("failed to process frame %d: %w", i, err)
		}

		// Duplicate frame for proper timing
		for d := 0; d < duplicateCount; d++ {
			framePath := filepath.Join(tempDir, fmt.Sprintf("frame_%05d.png", frameIndex))
			f, err := os.Create(framePath)
			if err != nil {
				return fmt.Errorf("failed to create frame file: %w", err)
			}

			if err := png.Encode(f, processedFrame); err != nil {
				f.Close()
				return fmt.Errorf("failed to encode frame %d: %w", i, err)
			}
			f.Close()
			frameIndex++
		}
	}

	log.Printf("[VideoExport] Saved %d processed frames to temp directory", frameIndex)

	// Verify frames exist
	files, err := filepath.Glob(filepath.Join(tempDir, "frame_*.png"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no frame files found in temp directory after processing")
	}
	log.Printf("[VideoExport] Verified %d frame files exist", len(files))

	// Calculate CRF (quality): 0-51, lower is better
	// Map quality 0-100 to CRF 51-0
	crf := 51 - (e.options.Quality * 51 / 100)
	if crf < 0 {
		crf = 0
	}
	if crf > 51 {
		crf = 51
	}

	// Build FFmpeg command
	// Frames are already processed to target dimensions with overlays
	inputPattern := filepath.Join(tempDir, "frame_%05d.png")
	args := []string{
		"-y",                    // Overwrite output
		"-framerate", fmt.Sprintf("%d", e.options.FrameRate),
		"-i", inputPattern,
		"-c:v", "libx264",       // H.264 codec
		"-preset", "medium",     // Encoding speed/quality tradeoff
		"-crf", fmt.Sprintf("%d", crf),
		"-pix_fmt", "yuv420p",   // Pixel format for compatibility
		"-movflags", "+faststart", // Enable streaming
		outputPath,
	}

	log.Printf("[VideoExport] Running FFmpeg: %s %v", e.ffmpegPath, args)

	cmd := exec.Command(e.ffmpegPath, args...)

	// Capture both stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start the command
	log.Printf("[VideoExport] Starting FFmpeg process...")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	// Wait for completion with a timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 5 minute timeout for video encoding
	timeout := time.After(5 * time.Minute)

	select {
	case err := <-done:
		if err != nil {
			log.Printf("[VideoExport] FFmpeg stderr: %s", stderr.String())
			return fmt.Errorf("FFmpeg encoding failed: %w\nStderr: %s", err, stderr.String())
		}
	case <-timeout:
		// Kill the process if it times out
		cmd.Process.Kill()
		log.Printf("[VideoExport] FFmpeg timed out after 5 minutes")
		log.Printf("[VideoExport] FFmpeg stderr so far: %s", stderr.String())
		return fmt.Errorf("FFmpeg encoding timed out after 5 minutes")
	}

	// Verify output file exists and has content
	if info, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not created: %w", err)
	} else if info.Size() == 0 {
		return fmt.Errorf("output file is empty")
	} else {
		log.Printf("[VideoExport] Output file size: %d bytes", info.Size())
	}

	log.Printf("[VideoExport] H.264 video exported successfully: %s", outputPath)
	return nil
}

// exportMotionJPEG creates an AVI file with Motion JPEG codec (compatible, plays everywhere)
func (e *Exporter) exportMotionJPEG(frames []Frame, outputPath string) error {
	if len(frames) == 0 {
		return fmt.Errorf("no frames to export")
	}

	// Ensure output has .avi extension
	if !strings.HasSuffix(strings.ToLower(outputPath), ".avi") {
		outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".avi"
	}

	// Calculate effective frame rate based on frame delay
	// Each frame should show for frameDelay seconds
	// So effective FPS = 1 / frameDelay
	effectiveFPS := int(1.0 / e.options.FrameDelay)
	if effectiveFPS < 1 {
		effectiveFPS = 1
	}
	if effectiveFPS > 30 {
		effectiveFPS = 30
	}

	// Create MJPEG writer
	writer, err := mjpeg.New(outputPath, int32(e.options.Width), int32(e.options.Height), int32(effectiveFPS))
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

	log.Printf("[VideoExport] MJPEG video exported: %s", outputPath)
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

// Ensure io is used (for potential future streaming support)
var _ = io.Discard
