import * as React from "react";
import { useState } from "react";
import { X, Download, FolderOpen, Film, Settings2 } from "lucide-react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/services/api";
import type { BoundingBox, GEDateInfo } from "@/types";
import { main } from "@/../wailsjs/go/models";

export interface ExportOptions {
  tiles: boolean;
  geotiff: boolean;
  both: boolean;
  mp4: boolean;
  gif: boolean;
}

export interface ExportDialogProps {
  isOpen: boolean;
  onClose: () => void;
  bbox: BoundingBox | null;
  zoom: number;
  source: "esri" | "google";

  // Single date export
  singleDate?: string;
  singleHexDate?: string;
  singleEpoch?: number;

  // Date range export
  dateRange?: Array<{ date: string; hexDate?: string; epoch?: number }>;
}

// Video presets with dimensions
const VIDEO_PRESETS = [
  { id: "youtube", label: "YouTube (1920×1080)", width: 1920, height: 1080 },
  { id: "instagram_square", label: "Instagram Square (1080×1080)", width: 1080, height: 1080 },
  { id: "instagram_portrait", label: "Instagram Portrait (1080×1350)", width: 1080, height: 1350 },
  { id: "instagram_reel", label: "Instagram Reel (1080×1920)", width: 1080, height: 1920 },
  { id: "tiktok", label: "TikTok (1080×1920)", width: 1080, height: 1920 },
  { id: "youtube_shorts", label: "YouTube Shorts (1080×1920)", width: 1080, height: 1920 },
  { id: "twitter", label: "Twitter/X (1280×720)", width: 1280, height: 720 },
  { id: "facebook", label: "Facebook (1280×720)", width: 1280, height: 720 },
];

const DATE_POSITIONS = [
  { id: "bottom-right", label: "Bottom Right" },
  { id: "bottom-left", label: "Bottom Left" },
  { id: "top-right", label: "Top Right" },
  { id: "top-left", label: "Top Left" },
  { id: "center", label: "Center" },
];

export function ExportDialog({
  isOpen,
  onClose,
  bbox,
  zoom,
  source,
  singleDate,
  singleHexDate,
  singleEpoch,
  dateRange,
}: ExportDialogProps) {
  const isRangeMode = !!dateRange && dateRange.length > 1;

  const [format, setFormat] = useState<"tiles" | "geotiff" | "both">("geotiff");
  const [includeVideo, setIncludeVideo] = useState(false);
  const [videoFormat, setVideoFormat] = useState<"mp4" | "gif">("mp4");

  // Video settings
  const [videoPreset, setVideoPreset] = useState("youtube");
  const [frameDelay, setFrameDelay] = useState(0.5); // seconds per frame
  const [showDateOverlay, setShowDateOverlay] = useState(true);
  const [datePosition, setDatePosition] = useState("bottom-right");
  const [videoQuality, setVideoQuality] = useState(90);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Crop position (0-1, where 0.5 is center)
  const [cropX, setCropX] = useState(0.5);
  const [cropY, setCropY] = useState(0.5);

  const [progress, setProgress] = useState<{
    downloaded: number;
    total: number;
    percent: number;
    status: string;
    currentDate?: number;
    totalDates?: number;
  } | null>(null);

  const [isExporting, setIsExporting] = useState(false);
  const [exportComplete, setExportComplete] = useState(false);
  const [exportZoom, setExportZoom] = useState(zoom);

  // Initialize zoom based on settings
  React.useEffect(() => {
    if (isOpen) {
      if ((window as any).go?.main?.App?.GetSettings) {
        (window as any).go.main.App.GetSettings().then((settings: any) => {
          if (settings.downloadZoomStrategy === "fixed" && settings.downloadFixedZoom) {
            setExportZoom(settings.downloadFixedZoom);
          } else {
            setExportZoom(zoom);
          }
        }).catch((err: any) => {
          console.error("Failed to load settings:", err);
          setExportZoom(zoom);
        });
      }
    }
  }, [isOpen, zoom]);

  React.useEffect(() => {
    if (!isOpen) {
      // Reset state when dialog closes
      setProgress(null);
      setIsExporting(false);
      setExportComplete(false);
    }
  }, [isOpen]);

  React.useEffect(() => {
    // Listen for download progress events
    const unsubscribe = api.onDownloadProgress((progressData: any) => {
      setProgress(progressData);
      if (progressData.percent === 100 && progressData.status === "Complete") {
        setIsExporting(false);
        setExportComplete(true);
      }
    });

    return () => {
      // Note: Wails EventsOff not directly exposed, but cleanup on unmount
    };
  }, []);

  // Get current preset dimensions
  const currentPreset = VIDEO_PRESETS.find(p => p.id === videoPreset) || VIDEO_PRESETS[0];

  const handleExport = async () => {
    if (!bbox) return;

    setIsExporting(true);
    setExportComplete(false);
    setProgress({ downloaded: 0, total: 1, percent: 0, status: "Starting export..." });

    try {
      if (isRangeMode && dateRange) {
        // Multi-date export
        if (source === "esri") {
          const dates = dateRange.map(d => d.date);
          await api.downloadEsriImageryRange(bbox, exportZoom, dates, format);
        } else {
          // Google Earth range
          const geDates: GEDateInfo[] = dateRange.map(d => ({
            date: d.date,
            hexDate: d.hexDate || "",
            epoch: d.epoch || 0,
          }));
          await api.downloadGoogleEarthHistoricalImageryRange(bbox, exportZoom, geDates, format);
        }

        // Video export if requested (after GeoTIFFs are downloaded)
        console.log("[ExportDialog] Video export check:", {
          includeVideo,
          format,
          willExport: includeVideo && format !== "tiles",
          dateRangeLength: dateRange?.length
        });

        if (includeVideo && format !== "tiles") {
          try {
            console.log("[ExportDialog] Starting video export...");
            setProgress({
              downloaded: 0,
              total: 1,
              percent: 0,
              status: "Starting video export..."
            });

            // Prepare video export options
            const videoOpts = new main.VideoExportOptions({
              width: currentPreset.width,
              height: currentPreset.height,
              preset: videoPreset,
              cropX: cropX,
              cropY: cropY,
              spotlightEnabled: false,
              spotlightCenterLat: 0,
              spotlightCenterLon: 0,
              spotlightRadiusKm: 1.0,
              overlayOpacity: 0.6,
              showDateOverlay: showDateOverlay,
              dateFontSize: 48,
              datePosition: datePosition,
              frameDelay: frameDelay,
              outputFormat: videoFormat,
              quality: videoQuality,
            });

            // Convert dateRange to proper GEDateInfo array
            const geDatesForVideo: GEDateInfo[] = dateRange.map(d => ({
              date: d.date,
              hexDate: d.hexDate || "",
              epoch: d.epoch || 0,
            }));

            console.log("[ExportDialog] Calling exportTimelapseVideo with:", {
              bbox,
              zoom: exportZoom,
              dateCount: geDatesForVideo.length,
              source: source === "esri" ? "esri" : "ge_historical",
              videoOpts: {
                preset: videoPreset,
                dimensions: `${currentPreset.width}x${currentPreset.height}`,
                frameDelay,
                showDateOverlay,
                datePosition,
              },
              dates: geDatesForVideo.map(d => d.date)
            });

            const result = await api.exportTimelapseVideo(
              bbox,
              exportZoom,
              geDatesForVideo,
              source === "esri" ? "esri" : "ge_historical",
              videoOpts
            );

            console.log("[ExportDialog] Video export completed successfully, result:", result);
          } catch (videoError) {
            console.error("[ExportDialog] Video export failed:", videoError);
            console.error("[ExportDialog] Video error stack:", videoError instanceof Error ? videoError.stack : "no stack");
            // Don't throw - let the main export succeed even if video fails
            setProgress({
              downloaded: 0,
              total: 1,
              percent: 0,
              status: `Video export failed: ${videoError}`
            });
          }
        } else {
          console.log("[ExportDialog] Skipping video export because:", {
            includeVideo,
            format,
            reason: !includeVideo ? "includeVideo is false" : "format is tiles"
          });
        }
      } else {
        // Single date export
        if (source === "esri" && singleDate) {
          await api.downloadEsriImagery(bbox, exportZoom, singleDate, format);
        } else if (source === "google" && singleHexDate !== undefined && singleEpoch !== undefined && singleDate) {
          await api.downloadGoogleEarthHistoricalImagery(
            bbox,
            exportZoom,
            singleHexDate,
            singleEpoch,
            singleDate,
            format
          );
        }
      }
    } catch (error) {
      console.error("Export failed:", error);
      setProgress({
        downloaded: 0,
        total: 1,
        percent: 0,
        status: `Error: ${error}`,
      });
      setIsExporting(false);
    }
  };

  const handleOpenFolder = async () => {
    try {
      await api.openDownloadFolder();
    } catch (error) {
      console.error("Failed to open download folder:", error);
    }
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <Card className="w-full max-w-2xl mx-4 max-h-[90vh] overflow-y-auto">
        <CardHeader className="flex flex-row items-center justify-between border-b sticky top-0 bg-card z-10">
          <div>
            <h2 className="text-xl font-semibold">
              Export {isRangeMode ? `${dateRange.length} Dates` : "Imagery"}
            </h2>
            <p className="text-sm text-muted-foreground mt-1">
              {source === "esri" ? "Esri Wayback" : "Google Earth"}
            </p>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={isExporting}>
            <X className="h-4 w-4" />
          </Button>
        </CardHeader>

        <CardContent className="p-6 space-y-6">
          {/* Format Selection */}
          <div className="space-y-3">
            <label className="text-sm font-medium">Export Format</label>
            <div className="flex gap-3">
              <Button
                variant={format === "tiles" ? "default" : "outline"}
                onClick={() => setFormat("tiles")}
                disabled={isExporting}
                className="flex-1"
              >
                Tiles Only
              </Button>
              <Button
                variant={format === "geotiff" ? "default" : "outline"}
                onClick={() => setFormat("geotiff")}
                disabled={isExporting}
                className="flex-1"
              >
                GeoTIFF
              </Button>
              <Button
                variant={format === "both" ? "default" : "outline"}
                onClick={() => setFormat("both")}
                disabled={isExporting}
                className="flex-1"
              >
                Both
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              • <strong>Tiles</strong>: Individual JPEG tiles{" "}
              • <strong>GeoTIFF</strong>: Merged, georeferenced TIFF (EPSG:3857){" "}
              • <strong>Both</strong>: Save tiles and GeoTIFF
            </p>
          </div>

          {/* Zoom Level Selection */}
          <div className="space-y-3 border-t pt-4">
            <div className="flex justify-between items-center">
              <label className="text-sm font-medium">Export Zoom Level</label>
              <span className="text-sm font-mono bg-muted px-2 py-0.5 rounded">{exportZoom}</span>
            </div>
            <div className="flex items-center gap-4">
              <span className="text-xs text-muted-foreground w-8">Low</span>
              <input
                type="range"
                min="1"
                max="20"
                step="1"
                value={exportZoom}
                onChange={(e) => setExportZoom(parseInt(e.target.value))}
                disabled={isExporting}
                className="flex-1 h-2 bg-secondary rounded-lg appearance-none cursor-pointer accent-primary"
              />
              <span className="text-xs text-muted-foreground w-8">High</span>
            </div>
            <p className="text-xs text-muted-foreground">
              Current Map Zoom: {zoom} • Download Zoom: {exportZoom}
            </p>
          </div>

          {/* Video Export Options (Range Mode Only) */}
          {isRangeMode && (
            <div className="space-y-4 border-t pt-4">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={includeVideo}
                  onChange={(e) => setIncludeVideo(e.target.checked)}
                  disabled={isExporting}
                  className="w-4 h-4 rounded border-border accent-primary"
                />
                <Film className="h-4 w-4" />
                <span className="text-sm font-medium">Create Timelapse Video</span>
              </label>

              {includeVideo && (
                <div className="space-y-4 ml-6 p-4 bg-muted/50 rounded-lg">
                  {/* Video Format */}
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Video Format</label>
                    <div className="flex gap-2">
                      <Button
                        variant={videoFormat === "mp4" ? "default" : "outline"}
                        size="sm"
                        onClick={() => setVideoFormat("mp4")}
                        disabled={isExporting}
                      >
                        MP4 (H.264)
                      </Button>
                      <Button
                        variant={videoFormat === "gif" ? "default" : "outline"}
                        size="sm"
                        onClick={() => setVideoFormat("gif")}
                        disabled={isExporting}
                      >
                        GIF
                      </Button>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      MP4 uses H.264 codec for best quality and compatibility
                    </p>
                  </div>

                  {/* Video Preset */}
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Dimensions</label>
                    <select
                      value={videoPreset}
                      onChange={(e) => setVideoPreset(e.target.value)}
                      disabled={isExporting}
                      className="w-full h-9 px-3 rounded-md border border-input bg-background text-sm"
                    >
                      {VIDEO_PRESETS.map(preset => (
                        <option key={preset.id} value={preset.id}>
                          {preset.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  {/* Frame Delay */}
                  <div className="space-y-2">
                    <div className="flex justify-between items-center">
                      <label className="text-sm font-medium">Frame Duration</label>
                      <span className="text-sm font-mono bg-background px-2 py-0.5 rounded">
                        {frameDelay.toFixed(1)}s
                      </span>
                    </div>
                    <input
                      type="range"
                      min="0.1"
                      max="3"
                      step="0.1"
                      value={frameDelay}
                      onChange={(e) => setFrameDelay(parseFloat(e.target.value))}
                      disabled={isExporting}
                      className="w-full h-2 bg-background rounded-lg appearance-none cursor-pointer accent-primary"
                    />
                    <p className="text-xs text-muted-foreground">
                      How long each frame shows ({(1/frameDelay).toFixed(1)} frames per second)
                    </p>
                  </div>

                  {/* Date Overlay Toggle */}
                  <div className="flex items-center justify-between">
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={showDateOverlay}
                        onChange={(e) => setShowDateOverlay(e.target.checked)}
                        disabled={isExporting}
                        className="w-4 h-4 rounded border-border accent-primary"
                      />
                      <span className="text-sm">Show Date Overlay</span>
                    </label>
                    {showDateOverlay && (
                      <select
                        value={datePosition}
                        onChange={(e) => setDatePosition(e.target.value)}
                        disabled={isExporting}
                        className="h-8 px-2 rounded-md border border-input bg-background text-xs"
                      >
                        {DATE_POSITIONS.map(pos => (
                          <option key={pos.id} value={pos.id}>
                            {pos.label}
                          </option>
                        ))}
                      </select>
                    )}
                  </div>

                  {/* Advanced Settings Toggle */}
                  <button
                    type="button"
                    onClick={() => setShowAdvanced(!showAdvanced)}
                    className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    <Settings2 className="h-3 w-3" />
                    {showAdvanced ? "Hide" : "Show"} Advanced Settings
                  </button>

                  {showAdvanced && (
                    <div className="space-y-3 pt-2 border-t border-border/50">
                      {/* Quality Slider */}
                      <div className="space-y-2">
                        <div className="flex justify-between items-center">
                          <label className="text-sm font-medium">Quality</label>
                          <span className="text-sm font-mono bg-background px-2 py-0.5 rounded">
                            {videoQuality}%
                          </span>
                        </div>
                        <input
                          type="range"
                          min="50"
                          max="100"
                          step="5"
                          value={videoQuality}
                          onChange={(e) => setVideoQuality(parseInt(e.target.value))}
                          disabled={isExporting}
                          className="w-full h-2 bg-background rounded-lg appearance-none cursor-pointer accent-primary"
                        />
                      </div>

                      {/* Crop Position Controls */}
                      <div className="space-y-3 pt-2">
                        <label className="text-sm font-medium">Crop Position</label>
                        <p className="text-xs text-muted-foreground">
                          The video will crop from the source imagery. Adjust where to crop from.
                        </p>

                        {/* Visual Crop Position Selector */}
                        <div className="relative w-full aspect-video bg-background rounded-lg border overflow-hidden">
                          {/* Grid lines */}
                          <div className="absolute inset-0 grid grid-cols-3 grid-rows-3">
                            {[...Array(9)].map((_, i) => (
                              <div
                                key={i}
                                className="border border-border/30"
                              />
                            ))}
                          </div>

                          {/* Crop indicator */}
                          <div
                            className="absolute w-12 h-12 border-2 border-primary rounded bg-primary/20 transform -translate-x-1/2 -translate-y-1/2 transition-all duration-150 cursor-move"
                            style={{
                              left: `${cropX * 100}%`,
                              top: `${cropY * 100}%`,
                            }}
                          />

                          {/* Click to position */}
                          <div
                            className="absolute inset-0 cursor-crosshair"
                            onClick={(e) => {
                              if (isExporting) return;
                              const rect = e.currentTarget.getBoundingClientRect();
                              const x = (e.clientX - rect.left) / rect.width;
                              const y = (e.clientY - rect.top) / rect.height;
                              setCropX(Math.max(0, Math.min(1, x)));
                              setCropY(Math.max(0, Math.min(1, y)));
                            }}
                          />
                        </div>

                        {/* Preset position buttons */}
                        <div className="flex gap-1 flex-wrap">
                          {[
                            { label: "↖", x: 0, y: 0 },
                            { label: "↑", x: 0.5, y: 0 },
                            { label: "↗", x: 1, y: 0 },
                            { label: "←", x: 0, y: 0.5 },
                            { label: "•", x: 0.5, y: 0.5 },
                            { label: "→", x: 1, y: 0.5 },
                            { label: "↙", x: 0, y: 1 },
                            { label: "↓", x: 0.5, y: 1 },
                            { label: "↘", x: 1, y: 1 },
                          ].map(({ label, x, y }) => (
                            <button
                              key={label}
                              type="button"
                              onClick={() => {
                                setCropX(x);
                                setCropY(y);
                              }}
                              disabled={isExporting}
                              className={`w-8 h-8 text-xs rounded border transition-colors ${
                                cropX === x && cropY === y
                                  ? "bg-primary text-primary-foreground border-primary"
                                  : "bg-background border-border hover:bg-muted"
                              }`}
                            >
                              {label}
                            </button>
                          ))}
                        </div>

                        <p className="text-xs text-muted-foreground">
                          Position: X={Math.round(cropX * 100)}%, Y={Math.round(cropY * 100)}%
                        </p>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {!includeVideo && (
                <p className="text-xs text-muted-foreground ml-6">
                  Create a timelapse video showing changes over time
                </p>
              )}
            </div>
          )}

          {/* Progress Bar */}
          {progress && (
            <div className="space-y-3 border-t pt-4">
              {/* Date Range Progress (only shown for multi-date downloads) */}
              {progress.totalDates && progress.totalDates > 1 && (
                <div className="flex items-center justify-between bg-muted/50 rounded-lg px-3 py-2">
                  <span className="text-sm font-medium">
                    Downloading Date {progress.currentDate} of {progress.totalDates}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {Math.round((progress.currentDate || 0) / progress.totalDates * 100)}% overall
                  </span>
                </div>
              )}

              {/* Current Task Status */}
              <div className="flex justify-between text-sm">
                <span className="font-medium">{progress.status}</span>
                <span className="text-muted-foreground">{progress.percent}%</span>
              </div>

              {/* Tile Progress Bar */}
              <div className="w-full bg-secondary rounded-full h-2 overflow-hidden">
                <div
                  className="bg-primary h-full transition-all duration-300 ease-out"
                  style={{ width: `${progress.percent}%` }}
                />
              </div>

              {progress.total > 0 && (
                <p className="text-xs text-muted-foreground">
                  {progress.downloaded} / {progress.total} tiles
                </p>
              )}
            </div>
          )}

          {/* Action Buttons */}
          <div className="flex gap-3 border-t pt-4">
            {exportComplete ? (
              <>
                <Button
                  onClick={handleOpenFolder}
                  className="flex-1"
                  variant="outline"
                >
                  <FolderOpen className="h-4 w-4 mr-2" />
                  Open Folder
                </Button>
                <Button onClick={onClose} className="flex-1">
                  Done
                </Button>
              </>
            ) : (
              <>
                <Button
                  onClick={onClose}
                  variant="outline"
                  className="flex-1"
                  disabled={isExporting}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleExport}
                  className="flex-1"
                  disabled={isExporting || !bbox}
                >
                  <Download className="h-4 w-4 mr-2" />
                  {isExporting ? "Exporting..." : "Export"}
                </Button>
              </>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
