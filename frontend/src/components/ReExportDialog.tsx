import * as React from "react";
import { useState } from "react";
import { Film } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { api } from "@/services/api";
import type { ExportTask } from "@/types";

const VIDEO_PRESETS = [
  { id: "youtube", label: "YouTube (1920×1080)" },
  { id: "instagram_square", label: "Instagram Square (1080×1080)" },
  { id: "instagram_portrait", label: "Instagram Portrait (1080×1350)" },
  { id: "instagram_reel", label: "Instagram Reel (1080×1920)" },
  { id: "tiktok", label: "TikTok (1080×1920)" },
  { id: "youtube_shorts", label: "YouTube Shorts (1080×1920)" },
  { id: "twitter", label: "Twitter/X (1280×720)" },
  { id: "facebook", label: "Facebook (1280×720)" },
];

interface ReExportDialogProps {
  task: ExportTask | null;
  isOpen: boolean;
  onClose: () => void;
  onSuccess?: () => void;
}

export function ReExportDialog({ task, isOpen, onClose, onSuccess }: ReExportDialogProps) {
  const [selectedPresets, setSelectedPresets] = useState<string[]>(["youtube"]);
  const [videoFormat, setVideoFormat] = useState<"mp4" | "gif">("mp4");
  const [isExporting, setIsExporting] = useState(false);

  const handleReExport = async () => {
    if (!task || selectedPresets.length === 0) return;

    setIsExporting(true);
    try {
      await api.reExportVideo(task.id, selectedPresets, videoFormat);
      onSuccess?.();
      onClose();
    } catch (error) {
      console.error("Re-export failed:", error);
      alert("Re-export failed: " + error);
    } finally {
      setIsExporting(false);
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Film className="h-5 w-5" />
            Re-export Video
          </DialogTitle>
          <DialogDescription>
            Export new video versions with different sizes from the existing imagery.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {/* Task info */}
          {task && (
            <div className="text-sm text-muted-foreground bg-muted/50 rounded-md p-3">
              <p className="font-medium text-foreground">{task.name}</p>
              <p>{task.dates.length} dates • {task.source}</p>
            </div>
          )}

          {/* Video Format */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">Video Format</Label>
            <div className="flex gap-2">
              {(["mp4", "gif"] as const).map((f) => (
                <Button
                  key={f}
                  variant={videoFormat === f ? "default" : "outline"}
                  size="sm"
                  onClick={() => setVideoFormat(f)}
                  disabled={isExporting}
                  className="flex-1 uppercase"
                >
                  {f}
                </Button>
              ))}
            </div>
          </div>

          {/* Preset Selection */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">Output Sizes ({selectedPresets.length} selected)</Label>
            <div className="space-y-2 max-h-48 overflow-y-auto bg-muted/30 rounded-md p-3 border">
              {VIDEO_PRESETS.map((preset) => (
                <div key={preset.id} className="flex items-center space-x-2">
                  <Checkbox
                    id={`reexport-preset-${preset.id}`}
                    checked={selectedPresets.includes(preset.id)}
                    onCheckedChange={(checked) => {
                      if (checked) {
                        setSelectedPresets((prev) => [...prev, preset.id]);
                      } else {
                        if (selectedPresets.length > 1) {
                          setSelectedPresets((prev) => prev.filter((id) => id !== preset.id));
                        }
                      }
                    }}
                    disabled={isExporting}
                  />
                  <Label
                    htmlFor={`reexport-preset-${preset.id}`}
                    className="text-sm cursor-pointer flex-1"
                  >
                    {preset.label}
                  </Label>
                </div>
              ))}
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={isExporting}>
            Cancel
          </Button>
          <Button onClick={handleReExport} disabled={isExporting || selectedPresets.length === 0}>
            {isExporting ? "Exporting..." : `Export ${selectedPresets.length} Video${selectedPresets.length > 1 ? "s" : ""}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
