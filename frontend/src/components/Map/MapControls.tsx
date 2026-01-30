import * as React from "react";
import { Download } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  useImageryContext,
  getAvailableDates,
  type ViewMode,
  type ImagerySource,
} from "@/contexts/ImageryContext";

// Single View - Center Timeline Control
function SingleViewTimeline({ onExport }: { onExport?: () => void }) {
  const { state, dispatch } = useImageryContext();
  const mapState = state.maps.single;
  const dates = getAvailableDates(state, "single");

  return (
    <Card className="bg-background/95 backdrop-blur-lg">
      <CardContent className="p-4 space-y-3">
        {/* Source Toggle and Export */}
        <div className="flex gap-2">
          <Button
            size="sm"
            variant={mapState.source === "esri" ? "default" : "outline"}
            onClick={() => {
              console.log("[MapControls] Single view - switching to Esri");
              dispatch({ type: "SET_MAP_SOURCE", map: "single", source: "esri" });
            }}
          >
            Esri
          </Button>
          <Button
            size="sm"
            variant={mapState.source === "google" ? "default" : "outline"}
            onClick={() => {
              console.log("[MapControls] Single view - switching to Google Earth");
              dispatch({ type: "SET_MAP_SOURCE", map: "single", source: "google" });
            }}
          >
            Google Earth
          </Button>
          {onExport && dates.length > 0 && (
            <Button
              size="sm"
              variant="outline"
              onClick={onExport}
              className="ml-auto"
              title="Export current view"
            >
              <Download className="h-4 w-4" />
            </Button>
          )}
        </div>

        {/* Date Slider */}
        {dates.length > 0 && (
          <div className="space-y-2">
            <div className="text-sm font-medium text-center">
              {dates[mapState.dateIndex]?.date || "No date"}
            </div>
            <input
              type="range"
              min="0"
              max={Math.max(0, dates.length - 1)}
              value={mapState.dateIndex}
              onChange={(e) => {
                const newIndex = parseInt(e.target.value);
                console.log("[MapControls] Single view - date index changed to:", newIndex, dates[newIndex]?.date);
                dispatch({
                  type: "SET_DATE_INDEX",
                  map: "single",
                  index: newIndex,
                });
              }}
              className="w-full"
            />
            <div className="flex justify-between text-xs text-muted-foreground">
              <span>{dates[0]?.date}</span>
              <span>{dates[dates.length - 1]?.date}</span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// Split View - Left and Right Timeline Controls
function SplitViewTimeline() {
  const { state, dispatch } = useImageryContext();

  const leftDates = getAvailableDates(state, "left");
  const rightDates = getAvailableDates(state, "right");

  const leftMapState = state.maps.left;
  const rightMapState = state.maps.right;

  return (
    <>
      {/* Left Control */}
      <Card className="bg-background/95 backdrop-blur-lg">
        <CardContent className="p-3 space-y-2">
          <div className="text-xs font-semibold text-center">Left</div>

          {/* Source Toggle */}
          <div className="flex gap-1">
            <Button
              size="sm"
              variant={leftMapState.source === "esri" ? "default" : "outline"}
              onClick={() => {
                console.log("[MapControls] Left map - switching to Esri");
                dispatch({ type: "SET_MAP_SOURCE", map: "left", source: "esri" });
              }}
              className="text-xs px-2 h-7"
            >
              Esri
            </Button>
            <Button
              size="sm"
              variant={leftMapState.source === "google" ? "default" : "outline"}
              onClick={() => {
                console.log("[MapControls] Left map - switching to Google Earth");
                dispatch({ type: "SET_MAP_SOURCE", map: "left", source: "google" });
              }}
              className="text-xs px-2 h-7"
            >
              Google
            </Button>
          </div>

          {/* Date Slider */}
          {leftDates.length > 0 && (
            <div className="space-y-1">
              <div className="text-xs text-center">
                {leftDates[leftMapState.dateIndex]?.date}
              </div>
              <input
                type="range"
                min="0"
                max={Math.max(0, leftDates.length - 1)}
                value={leftMapState.dateIndex}
                onChange={(e) => {
                  const newIndex = parseInt(e.target.value);
                  console.log("[MapControls] Left map - date index changed to:", newIndex, leftDates[newIndex]?.date);
                  dispatch({
                    type: "SET_DATE_INDEX",
                    map: "left",
                    index: newIndex,
                  });
                }}
                className="w-full"
              />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Right Control */}
      <Card className="bg-background/95 backdrop-blur-lg">
        <CardContent className="p-3 space-y-2">
          <div className="text-xs font-semibold text-center">Right</div>

          {/* Source Toggle */}
          <div className="flex gap-1">
            <Button
              size="sm"
              variant={rightMapState.source === "esri" ? "default" : "outline"}
              onClick={() => {
                console.log("[MapControls] Right map - switching to Esri");
                dispatch({ type: "SET_MAP_SOURCE", map: "right", source: "esri" });
              }}
              className="text-xs px-2 h-7"
            >
              Esri
            </Button>
            <Button
              size="sm"
              variant={rightMapState.source === "google" ? "default" : "outline"}
              onClick={() => {
                console.log("[MapControls] Right map - switching to Google Earth");
                dispatch({ type: "SET_MAP_SOURCE", map: "right", source: "google" });
              }}
              className="text-xs px-2 h-7"
            >
              Google
            </Button>
          </div>

          {/* Date Slider */}
          {rightDates.length > 0 && (
            <div className="space-y-1">
              <div className="text-xs text-center">
                {rightDates[rightMapState.dateIndex]?.date}
              </div>
              <input
                type="range"
                min="0"
                max={Math.max(0, rightDates.length - 1)}
                value={rightMapState.dateIndex}
                onChange={(e) => {
                  const newIndex = parseInt(e.target.value);
                  console.log("[MapControls] Right map - date index changed to:", newIndex, rightDates[newIndex]?.date);
                  dispatch({
                    type: "SET_DATE_INDEX",
                    map: "right",
                    index: newIndex,
                  });
                }}
                className="w-full"
              />
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}

// Main MapControls Component
export function MapControls({
  className,
  onExport,
}: {
  className?: string;
  onExport?: () => void;
}) {
  const { state } = useImageryContext();

  return (
    <div
      className={cn(
        "absolute bottom-4 left-1/2 -translate-x-1/2 z-10 flex gap-4 items-end",
        className
      )}
    >
      {state.viewMode === "single" ? (
        <SingleViewTimeline onExport={onExport} />
      ) : (
        <SplitViewTimeline />
      )}
    </div>
  );
}
