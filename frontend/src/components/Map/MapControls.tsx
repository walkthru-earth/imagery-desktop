import * as React from "react";
import { useState } from "react";
import { Download, Calendar } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { cn } from "@/lib/utils";
import {
  useImageryContext,
  getAvailableDates,
  type ViewMode,
  type ImagerySource,
} from "@/contexts/ImageryContext";
import { SourceSelector } from "./SourceSelector";

// Single View - Center Timeline Control
function SingleViewTimeline({ onExport }: { onExport?: () => void }) {
  const { state, dispatch } = useImageryContext();
  const mapState = state.maps.single;
  const dates = getAvailableDates(state, "single");

  // Range selection mode
  const [isRangeMode, setIsRangeMode] = useState(false);
  const [rangeStart, setRangeStart] = useState(0);
  const [rangeEnd, setRangeEnd] = useState(0);

  const handleExportRange = () => {
    // Export range of dates
    const selectedDates = dates.slice(
      Math.min(rangeStart, rangeEnd),
      Math.max(rangeStart, rangeEnd) + 1
    );
    console.log("[MapControls] Exporting date range:", selectedDates);
    // TODO: Open export dialog with date range
    if (onExport) {
      onExport();
    }
  };

  return (
    <Card className="bg-background/95 backdrop-blur-lg">
      <CardContent className="p-4 space-y-3">
        {/* Source Selector and Export */}
        <div className="flex gap-2 items-center">
          <SourceSelector
            value={mapState.source}
            onChange={(source) => {
              console.log("[MapControls] Single view - switching to:", source);
              dispatch({ type: "SET_MAP_SOURCE", map: "single", source: source as ImagerySource });
            }}
            size="sm"
            className="flex-1"
          />
          <Button
            size="sm"
            variant={isRangeMode ? "default" : "outline"}
            onClick={() => {
              setIsRangeMode(!isRangeMode);
              if (!isRangeMode) {
                // Initialize range to current date
                setRangeStart(mapState.dateIndex);
                setRangeEnd(Math.min(mapState.dateIndex + 5, dates.length - 1));
              }
            }}
            title="Toggle range selection"
          >
            <Calendar className="h-4 w-4" />
          </Button>
          {onExport && dates.length > 0 && (
            <Button
              size="sm"
              variant="outline"
              onClick={isRangeMode ? handleExportRange : onExport}
              title={isRangeMode ? "Export selected date range" : "Export current view"}
            >
              <Download className="h-4 w-4" />
              {isRangeMode && (
                <span className="ml-1 text-xs">
                  ({Math.abs(rangeEnd - rangeStart) + 1})
                </span>
              )}
            </Button>
          )}
        </div>

        {/* Date Slider */}
        {dates.length > 0 && (
          <div className="space-y-2">
            {isRangeMode ? (
              <>
                {/* Range Mode - Single Dual-Thumb Slider */}
                <div className="text-sm font-medium text-center">
                  {dates[rangeStart]?.date} → {dates[rangeEnd]?.date}
                  <span className="ml-2 text-xs text-muted-foreground">
                    ({Math.abs(rangeEnd - rangeStart) + 1} dates)
                  </span>
                </div>
                <div className="px-1">
                  <Slider
                    min={0}
                    max={Math.max(0, dates.length - 1)}
                    step={1}
                    value={[rangeStart, rangeEnd]}
                    onValueChange={(values) => {
                      setRangeStart(values[0]);
                      setRangeEnd(values[1]);
                    }}
                    className="w-full"
                  />
                </div>
                <div className="flex justify-between text-xs text-muted-foreground">
                  <span>{dates[0]?.date}</span>
                  <span>{dates[dates.length - 1]?.date}</span>
                </div>
              </>
            ) : (
              <>
                {/* Single Mode - One Slider */}
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
              </>
            )}
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

          {/* Source Selector */}
          <SourceSelector
            value={leftMapState.source}
            onChange={(source) => {
              console.log("[MapControls] Left map - switching to:", source);
              dispatch({ type: "SET_MAP_SOURCE", map: "left", source: source as ImagerySource });
            }}
            size="sm"
          />

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

          {/* Source Selector */}
          <SourceSelector
            value={rightMapState.source}
            onChange={(source) => {
              console.log("[MapControls] Right map - switching to:", source);
              dispatch({ type: "SET_MAP_SOURCE", map: "right", source: source as ImagerySource });
            }}
            size="sm"
          />

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
        state.viewMode === "single" ? "w-[600px]" : "w-[700px]",
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
