import * as React from "react";
import { useState } from "react";
import { X, Download, FolderOpen } from "lucide-react";
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

  const [progress, setProgress] = useState<{
    downloaded: number;
    total: number;
    percent: number;
    status: string;
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
        console.log("[ExportDialog] Video export check:", { includeVideo, format, willExport: includeVideo && format !== "tiles" });

        if (includeVideo && format !== "tiles") {
          console.log("[ExportDialog] Starting video export...");
          setProgress({
            downloaded: 0,
            total: 1,
            percent: 0,
            status: "Starting video export..."
          });

          // Prepare video export options with sensible defaults
          const videoOpts = new main.VideoExportOptions({
            width: 1920,
            height: 1080,
            preset: "youtube", // Default preset
            spotlightEnabled: false,
            spotlightCenterLat: 0,
            spotlightCenterLon: 0,
            spotlightRadiusKm: 1.0,
            overlayOpacity: 0.6,
            showDateOverlay: true,
            dateFontSize: 48,
            datePosition: "bottom-right",
            frameDelay: 0.5, // 2 images per second
            outputFormat: videoFormat,
            quality: 90,
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
            videoOpts
          });

          await api.exportTimelapseVideo(
            bbox,
            exportZoom,
            geDatesForVideo,
            source === "esri" ? "esri" : "ge_historical",
            videoOpts
          );

          console.log("[ExportDialog] Video export completed successfully");
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
      <Card className="w-full max-w-2xl mx-4">
        <CardHeader className="flex flex-row items-center justify-between border-b">
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
            <div className="space-y-3 border-t pt-4">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={includeVideo}
                  onChange={(e) => setIncludeVideo(e.target.checked)}
                  disabled={isExporting}
                  className="w-4 h-4 rounded border-border accent-primary"
                />
                <span className="text-sm font-medium">Create Timelapse Video</span>
              </label>

              {includeVideo && (
                <div className="flex gap-3 ml-6">
                  <Button
                    variant={videoFormat === "mp4" ? "default" : "outline"}
                    size="sm"
                    onClick={() => setVideoFormat("mp4")}
                    disabled={isExporting}
                  >
                    MP4
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
              )}

              <p className="text-xs text-muted-foreground ml-6">
                Timelapse video showing changes over time
              </p>
            </div>
          )}

          {/* Progress Bar */}
          {progress && (
            <div className="space-y-2 border-t pt-4">
              <div className="flex justify-between text-sm">
                <span className="font-medium">{progress.status}</span>
                <span className="text-muted-foreground">{progress.percent}%</span>
              </div>
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
