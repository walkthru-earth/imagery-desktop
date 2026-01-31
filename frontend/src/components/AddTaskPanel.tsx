import * as React from "react";
import { useState, useEffect, useCallback } from "react";
import {
  X,
  Film,
  Settings2,
  ListPlus,
  ChevronLeft,
} from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/services/api";
import type { BoundingBox, GEDateInfo, CropPreview } from "@/types";
import { main } from "@/../wailsjs/go/models";
import { cn } from "@/lib/utils";

export interface AddTaskPanelProps {
  isOpen: boolean;
  onClose: () => void;
  bbox: BoundingBox | null;
  zoom: number;
  source: "esri" | "google";

  // Single date task
  singleDate?: string;
  singleHexDate?: string;
  singleEpoch?: number;

  // Date range task
  dateRange?: Array<{ date: string; hexDate?: string; epoch?: number }>;

  // Crop overlay callback
  onCropChange?: (crop: CropPreview | null) => void;
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

export function AddTaskPanel({
  isOpen,
  onClose,
  bbox,
  zoom,
  source,
  singleDate,
  singleHexDate,
  singleEpoch,
  dateRange,
  onCropChange,
}: AddTaskPanelProps) {
  const isRangeMode = !!dateRange && dateRange.length > 1;

  const [format, setFormat] = useState<"tiles" | "geotiff" | "both">("geotiff");
  const [includeVideo, setIncludeVideo] = useState(false);
  const [videoFormat, setVideoFormat] = useState<"mp4" | "gif">("mp4");

  // Video settings
  const [videoPreset, setVideoPreset] = useState("youtube");
  const [frameDelay, setFrameDelay] = useState(0.5);
  const [showDateOverlay, setShowDateOverlay] = useState(true);
  const [datePosition, setDatePosition] = useState("bottom-right");
  const [videoQuality, setVideoQuality] = useState(90);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Crop position (0-1, where 0.5 is center)
  const [cropX, setCropX] = useState(0.5);
  const [cropY, setCropY] = useState(0.5);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [exportZoom, setExportZoom] = useState(zoom);

  // Get current preset dimensions
  const currentPreset = VIDEO_PRESETS.find(p => p.id === videoPreset) || VIDEO_PRESETS[0];

  // Calculate crop preview based on preset aspect ratio
  const calculateCropPreview = useCallback((): CropPreview => {
    const aspectRatio = currentPreset.width / currentPreset.height;

    // Assume container aspect ratio is 16:9 (typical map view)
    const containerAspectRatio = 16 / 9;

    let cropWidth: number;
    let cropHeight: number;

    if (aspectRatio > containerAspectRatio) {
      // Preset is wider - fit to width
      cropWidth = 0.8;
      cropHeight = cropWidth / aspectRatio * containerAspectRatio;
    } else {
      // Preset is taller - fit to height
      cropHeight = 0.8;
      cropWidth = cropHeight * aspectRatio / containerAspectRatio;
    }

    // Center the crop based on cropX, cropY (where 0.5 = center)
    const x = cropX * (1 - cropWidth);
    const y = cropY * (1 - cropHeight);

    return { x, y, width: cropWidth, height: cropHeight };
  }, [currentPreset, cropX, cropY]);

  // Update crop overlay when video settings change
  useEffect(() => {
    if (isOpen && includeVideo && onCropChange) {
      onCropChange(calculateCropPreview());
    } else if (onCropChange) {
      onCropChange(null);
    }
  }, [isOpen, includeVideo, calculateCropPreview, onCropChange]);

  // Initialize zoom based on settings
  useEffect(() => {
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

  // Clear crop overlay on close
  useEffect(() => {
    if (!isOpen && onCropChange) {
      onCropChange(null);
    }
  }, [isOpen, onCropChange]);

  const handleAddToQueue = async () => {
    if (!bbox) return;

    setIsSubmitting(true);

    try {
      // Build dates array
      let dates: GEDateInfo[];
      if (isRangeMode && dateRange) {
        dates = dateRange.map(d => ({
          date: d.date,
          hexDate: d.hexDate || "",
          epoch: d.epoch || 0,
        }));
      } else {
        dates = [{
          date: singleDate || "",
          hexDate: singleHexDate || "",
          epoch: singleEpoch || 0,
        }];
      }

      // Build task name
      const taskName = isRangeMode
        ? `${source === "esri" ? "Esri" : "Google Earth"} ${dates.length} dates (Z${exportZoom})`
        : `${source === "esri" ? "Esri" : "Google Earth"} ${singleDate} (Z${exportZoom})`;

      // Build video options if needed
      let videoOpts: main.VideoExportOptions | undefined;
      if (includeVideo && isRangeMode && format !== "tiles") {
        videoOpts = new main.VideoExportOptions({
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
      }

      // Create task
      const task = new main.TaskQueueExportTask({
        id: "", // Will be assigned by backend
        name: taskName,
        status: "pending",
        priority: 0,
        createdAt: new Date().toISOString(),
        source: source === "esri" ? "esri" : "google",
        bbox: bbox,
        zoom: exportZoom,
        format: format,
        dates: dates,
        videoExport: includeVideo && isRangeMode && format !== "tiles",
        videoOpts: videoOpts,
        progress: {
          currentPhase: "",
          totalDates: dates.length,
          currentDate: 0,
          tilesTotal: 0,
          tilesCompleted: 0,
          percent: 0,
        },
      });

      await api.addExportTask(task);

      // Close panel - task is now in queue
      onClose();

      // Auto-start queue if not running
      const status = await api.getTaskQueueStatus();
      if (!status.isRunning) {
        await api.startTaskQueue();
      }
    } catch (error) {
      console.error("Failed to add task to queue:", error);
      alert("Failed to add task to queue: " + error);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div
      className={cn(
        "fixed left-0 top-14 bottom-0 z-40",
        "w-96 bg-background border-r shadow-lg",
        "flex flex-col transition-transform duration-300 ease-in-out",
        isOpen ? "translate-x-0" : "-translate-x-full"
      )}
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b bg-card">
        <div>
          <h2 className="text-lg font-semibold">
            Add Task: {isRangeMode ? `${dateRange?.length} Dates` : "Single Date"}
          </h2>
          <p className="text-sm text-muted-foreground">
            {source === "esri" ? "Esri Wayback" : "Google Earth"}
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} disabled={isSubmitting}>
          <ChevronLeft className="h-5 w-5" />
        </Button>
      </div>

      {/* Scrollable Content */}
      <div className="flex-1 overflow-y-auto p-4 space-y-5">
        {/* Format Selection */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Export Format</label>
          <div className="flex gap-2">
            <Button
              variant={format === "tiles" ? "default" : "outline"}
              onClick={() => setFormat("tiles")}
              disabled={isSubmitting}
              size="sm"
              className="flex-1"
            >
              Tiles
            </Button>
            <Button
              variant={format === "geotiff" ? "default" : "outline"}
              onClick={() => setFormat("geotiff")}
              disabled={isSubmitting}
              size="sm"
              className="flex-1"
            >
              GeoTIFF
            </Button>
            <Button
              variant={format === "both" ? "default" : "outline"}
              onClick={() => setFormat("both")}
              disabled={isSubmitting}
              size="sm"
              className="flex-1"
            >
              Both
            </Button>
          </div>
        </div>

        {/* Zoom Level Selection */}
        <div className="space-y-2">
          <div className="flex justify-between items-center">
            <label className="text-sm font-medium">Export Zoom Level</label>
            <span className="text-sm font-mono bg-muted px-2 py-0.5 rounded">{exportZoom}</span>
          </div>
          <input
            type="range"
            min="1"
            max="20"
            step="1"
            value={exportZoom}
            onChange={(e) => setExportZoom(parseInt(e.target.value))}
            disabled={isSubmitting}
            className="w-full h-2 bg-secondary rounded-lg appearance-none cursor-pointer accent-primary"
          />
          <p className="text-xs text-muted-foreground">
            Map: {zoom} | Export: {exportZoom}
          </p>
        </div>

        {/* Video Export Options (Range Mode Only) */}
        {isRangeMode && (
          <div className="space-y-3 border-t pt-4">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={includeVideo}
                onChange={(e) => setIncludeVideo(e.target.checked)}
                disabled={isSubmitting}
                className="w-4 h-4 rounded border-border accent-primary"
              />
              <Film className="h-4 w-4" />
              <span className="text-sm font-medium">Create Timelapse Video</span>
            </label>

            {includeVideo && (
              <div className="space-y-3 ml-6 p-3 bg-muted/50 rounded-lg">
                {/* Video Format */}
                <div className="space-y-1.5">
                  <label className="text-xs font-medium">Video Format</label>
                  <div className="flex gap-2">
                    <Button
                      variant={videoFormat === "mp4" ? "default" : "outline"}
                      size="sm"
                      onClick={() => setVideoFormat("mp4")}
                      disabled={isSubmitting}
                      className="flex-1 h-8 text-xs"
                    >
                      MP4
                    </Button>
                    <Button
                      variant={videoFormat === "gif" ? "default" : "outline"}
                      size="sm"
                      onClick={() => setVideoFormat("gif")}
                      disabled={isSubmitting}
                      className="flex-1 h-8 text-xs"
                    >
                      GIF
                    </Button>
                  </div>
                </div>

                {/* Video Preset */}
                <div className="space-y-1.5">
                  <label className="text-xs font-medium">Dimensions</label>
                  <select
                    value={videoPreset}
                    onChange={(e) => setVideoPreset(e.target.value)}
                    disabled={isSubmitting}
                    className="w-full h-8 px-2 rounded-md border border-input bg-background text-xs"
                  >
                    {VIDEO_PRESETS.map(preset => (
                      <option key={preset.id} value={preset.id}>
                        {preset.label}
                      </option>
                    ))}
                  </select>
                  <p className="text-xs text-muted-foreground">
                    Pan the map to position your export area
                  </p>
                </div>

                {/* Frame Delay */}
                <div className="space-y-1.5">
                  <div className="flex justify-between items-center">
                    <label className="text-xs font-medium">Frame Duration</label>
                    <span className="text-xs font-mono bg-background px-1.5 py-0.5 rounded">
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
                    disabled={isSubmitting}
                    className="w-full h-1.5 bg-background rounded-lg appearance-none cursor-pointer accent-primary"
                  />
                </div>

                {/* Date Overlay Toggle */}
                <div className="flex items-center justify-between">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={showDateOverlay}
                      onChange={(e) => setShowDateOverlay(e.target.checked)}
                      disabled={isSubmitting}
                      className="w-3.5 h-3.5 rounded border-border accent-primary"
                    />
                    <span className="text-xs">Date Overlay</span>
                  </label>
                  {showDateOverlay && (
                    <select
                      value={datePosition}
                      onChange={(e) => setDatePosition(e.target.value)}
                      disabled={isSubmitting}
                      className="h-7 px-1.5 rounded-md border border-input bg-background text-xs"
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
                  {showAdvanced ? "Hide" : "Show"} Advanced
                </button>

                {showAdvanced && (
                  <div className="space-y-3 pt-2 border-t border-border/50">
                    {/* Quality Slider */}
                    <div className="space-y-1.5">
                      <div className="flex justify-between items-center">
                        <label className="text-xs font-medium">Quality</label>
                        <span className="text-xs font-mono bg-background px-1.5 py-0.5 rounded">
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
                        disabled={isSubmitting}
                        className="w-full h-1.5 bg-background rounded-lg appearance-none cursor-pointer accent-primary"
                      />
                    </div>

                    {/* Crop Position Controls */}
                    <div className="space-y-2">
                      <label className="text-xs font-medium">Crop Position</label>
                      <div className="grid grid-cols-3 gap-1">
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
                            disabled={isSubmitting}
                            className={cn(
                              "w-full h-8 text-xs rounded border transition-colors",
                              cropX === x && cropY === y
                                ? "bg-primary text-primary-foreground border-primary"
                                : "bg-background border-border hover:bg-muted"
                            )}
                          >
                            {label}
                          </button>
                        ))}
                      </div>
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
      </div>

      {/* Footer - Fixed at bottom */}
      <div className="p-4 border-t bg-card space-y-3">
        <Button
          onClick={handleAddToQueue}
          className="w-full"
          disabled={isSubmitting || !bbox}
        >
          <ListPlus className="h-4 w-4 mr-2" />
          {isSubmitting ? "Adding..." : "Add to Queue"}
        </Button>
        <p className="text-xs text-muted-foreground text-center">
          Task will appear in the Export Queue panel
        </p>
      </div>
    </div>
  );
}
