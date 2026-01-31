import * as React from "react";
import { useState, useEffect, useCallback } from "react";
import { Film, Settings2, ListPlus, ChevronLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
  singleDate?: string;
  singleHexDate?: string;
  singleEpoch?: number;
  dateRange?: Array<{ date: string; hexDate?: string; epoch?: number }>;
  onCropChange?: (crop: CropPreview | null) => void;
  onTaskAdded?: () => void;
}

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
  onTaskAdded,
}: AddTaskPanelProps) {
  const isRangeMode = !!dateRange && dateRange.length > 1;

  const [format, setFormat] = useState<"tiles" | "geotiff" | "both">("geotiff");
  const [includeVideo, setIncludeVideo] = useState(false);
  const [videoFormat, setVideoFormat] = useState<"mp4" | "gif">("mp4");
  const [videoPreset, setVideoPreset] = useState("youtube");
  const [frameDelay, setFrameDelay] = useState(0.5);
  const [showDateOverlay, setShowDateOverlay] = useState(true);
  const [datePosition, setDatePosition] = useState("bottom-right");
  const [videoQuality, setVideoQuality] = useState(90);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [cropX, setCropX] = useState(0.5);
  const [cropY, setCropY] = useState(0.5);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [exportZoom, setExportZoom] = useState(zoom);

  const currentPreset = VIDEO_PRESETS.find(p => p.id === videoPreset) || VIDEO_PRESETS[0];

  const calculateCropPreview = useCallback((): CropPreview => {
    const aspectRatio = currentPreset.width / currentPreset.height;
    const containerAspectRatio = 16 / 9;

    let cropWidth: number;
    let cropHeight: number;

    if (aspectRatio > containerAspectRatio) {
      cropWidth = 0.8;
      cropHeight = cropWidth / aspectRatio * containerAspectRatio;
    } else {
      cropHeight = 0.8;
      cropWidth = cropHeight * aspectRatio / containerAspectRatio;
    }

    const x = cropX * (1 - cropWidth);
    const y = cropY * (1 - cropHeight);

    return { x, y, width: cropWidth, height: cropHeight };
  }, [currentPreset, cropX, cropY]);

  useEffect(() => {
    if (isOpen && includeVideo && onCropChange) {
      onCropChange(calculateCropPreview());
    } else if (onCropChange) {
      onCropChange(null);
    }
  }, [isOpen, includeVideo, calculateCropPreview, onCropChange]);

  useEffect(() => {
    if (isOpen) {
      if ((window as any).go?.main?.App?.GetSettings) {
        (window as any).go.main.App.GetSettings().then((settings: any) => {
          if (settings.downloadZoomStrategy === "fixed" && settings.downloadFixedZoom) {
            setExportZoom(settings.downloadFixedZoom);
          } else {
            setExportZoom(zoom);
          }
        }).catch(() => setExportZoom(zoom));
      }
    }
  }, [isOpen, zoom]);

  useEffect(() => {
    if (!isOpen && onCropChange) {
      onCropChange(null);
    }
  }, [isOpen, onCropChange]);

  const handleAddToQueue = async () => {
    if (!bbox) return;
    setIsSubmitting(true);

    try {
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

      const taskName = isRangeMode
        ? `${source === "esri" ? "Esri" : "Google Earth"} ${dates.length} dates (Z${exportZoom})`
        : `${source === "esri" ? "Esri" : "Google Earth"} ${singleDate} (Z${exportZoom})`;

      let videoOpts: main.VideoExportOptions | undefined;
      if (includeVideo && isRangeMode && format !== "tiles") {
        videoOpts = new main.VideoExportOptions({
          width: currentPreset.width,
          height: currentPreset.height,
          preset: videoPreset,
          cropX,
          cropY,
          spotlightEnabled: false,
          spotlightCenterLat: 0,
          spotlightCenterLon: 0,
          spotlightRadiusKm: 1.0,
          overlayOpacity: 0.6,
          showDateOverlay,
          dateFontSize: 48,
          datePosition,
          frameDelay,
          outputFormat: videoFormat,
          quality: videoQuality,
        });
      }

      const task = new main.TaskQueueExportTask({
        id: "",
        name: taskName,
        status: "pending",
        priority: 0,
        createdAt: new Date().toISOString(),
        source: source === "esri" ? "esri" : "google",
        bbox,
        zoom: exportZoom,
        format,
        dates,
        videoExport: includeVideo && isRangeMode && format !== "tiles",
        videoOpts,
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
      onTaskAdded?.();
      onClose();

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
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-5">
          {/* Format Selection */}
          <div className="space-y-2">
            <Label>Export Format</Label>
            <div className="flex gap-2">
              {(["tiles", "geotiff", "both"] as const).map((f) => (
                <Button
                  key={f}
                  variant={format === f ? "default" : "outline"}
                  onClick={() => setFormat(f)}
                  disabled={isSubmitting}
                  size="sm"
                  className="flex-1 capitalize"
                >
                  {f === "geotiff" ? "GeoTIFF" : f === "both" ? "Both" : "Tiles"}
                </Button>
              ))}
            </div>
          </div>

          {/* Zoom Level Selection */}
          <div className="space-y-3">
            <div className="flex justify-between items-center">
              <Label>Export Zoom Level</Label>
              <span className="text-sm font-mono bg-muted px-2 py-0.5 rounded">{exportZoom}</span>
            </div>
            <Slider
              min={1}
              max={20}
              step={1}
              value={[exportZoom]}
              onValueChange={(v) => setExportZoom(v[0])}
              disabled={isSubmitting}
            />
            <p className="text-xs text-muted-foreground">
              Map: {zoom} | Export: {exportZoom}
            </p>
          </div>

          {/* Video Export Options */}
          {isRangeMode && (
            <>
              <Separator />
              <div className="space-y-3">
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="include-video"
                    checked={includeVideo}
                    onCheckedChange={(checked) => setIncludeVideo(checked === true)}
                    disabled={isSubmitting}
                  />
                  <Label htmlFor="include-video" className="flex items-center gap-2 cursor-pointer">
                    <Film className="h-4 w-4" />
                    Create Timelapse Video
                  </Label>
                </div>

                {includeVideo && (
                  <div className="space-y-4 ml-6 p-3 bg-muted/50 rounded-lg">
                    {/* Video Format */}
                    <div className="space-y-2">
                      <Label className="text-xs">Video Format</Label>
                      <div className="flex gap-2">
                        {(["mp4", "gif"] as const).map((f) => (
                          <Button
                            key={f}
                            variant={videoFormat === f ? "default" : "outline"}
                            size="sm"
                            onClick={() => setVideoFormat(f)}
                            disabled={isSubmitting}
                            className="flex-1 h-8 text-xs uppercase"
                          >
                            {f}
                          </Button>
                        ))}
                      </div>
                    </div>

                    {/* Video Preset */}
                    <div className="space-y-2">
                      <Label className="text-xs">Dimensions</Label>
                      <Select value={videoPreset} onValueChange={setVideoPreset} disabled={isSubmitting}>
                        <SelectTrigger size="sm" className="w-full text-xs">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {VIDEO_PRESETS.map(preset => (
                            <SelectItem key={preset.id} value={preset.id} className="text-xs">
                              {preset.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">
                        Pan the map to position your export area
                      </p>
                    </div>

                    {/* Frame Delay */}
                    <div className="space-y-2">
                      <div className="flex justify-between items-center">
                        <Label className="text-xs">Frame Duration</Label>
                        <span className="text-xs font-mono bg-background px-1.5 py-0.5 rounded">
                          {frameDelay.toFixed(1)}s
                        </span>
                      </div>
                      <Slider
                        min={0.1}
                        max={3}
                        step={0.1}
                        value={[frameDelay]}
                        onValueChange={(v) => setFrameDelay(v[0])}
                        disabled={isSubmitting}
                        className="h-1.5"
                      />
                    </div>

                    {/* Date Overlay */}
                    <div className="flex items-center justify-between">
                      <div className="flex items-center space-x-2">
                        <Checkbox
                          id="date-overlay"
                          checked={showDateOverlay}
                          onCheckedChange={(checked) => setShowDateOverlay(checked === true)}
                          disabled={isSubmitting}
                        />
                        <Label htmlFor="date-overlay" className="text-xs cursor-pointer">
                          Date Overlay
                        </Label>
                      </div>
                      {showDateOverlay && (
                        <Select value={datePosition} onValueChange={setDatePosition} disabled={isSubmitting}>
                          <SelectTrigger size="sm" className="h-7 w-auto text-xs">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {DATE_POSITIONS.map(pos => (
                              <SelectItem key={pos.id} value={pos.id} className="text-xs">
                                {pos.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
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
                      <div className="space-y-4 pt-2 border-t border-border/50">
                        {/* Quality */}
                        <div className="space-y-2">
                          <div className="flex justify-between items-center">
                            <Label className="text-xs">Quality</Label>
                            <span className="text-xs font-mono bg-background px-1.5 py-0.5 rounded">
                              {videoQuality}%
                            </span>
                          </div>
                          <Slider
                            min={50}
                            max={100}
                            step={5}
                            value={[videoQuality]}
                            onValueChange={(v) => setVideoQuality(v[0])}
                            disabled={isSubmitting}
                            className="h-1.5"
                          />
                        </div>

                        {/* Crop Position */}
                        <div className="space-y-2">
                          <Label className="text-xs">Crop Position</Label>
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
                              <Button
                                key={label}
                                variant={cropX === x && cropY === y ? "default" : "outline"}
                                size="sm"
                                onClick={() => { setCropX(x); setCropY(y); }}
                                disabled={isSubmitting}
                                className="h-8 text-xs"
                              >
                                {label}
                              </Button>
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
            </>
          )}
        </div>
      </ScrollArea>

      {/* Footer */}
      <div className="p-4 border-t bg-card space-y-3">
        <Button onClick={handleAddToQueue} className="w-full" disabled={isSubmitting || !bbox}>
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
