import * as React from "react";
import { useState } from "react";
import { ListPlus, Calendar, ChevronLeft, ChevronRight, ChevronUp, ChevronDown, MoveVertical } from "lucide-react";
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
function SingleViewTimeline({ onAddTask }: { onAddTask?: (dateRange?: any[]) => void }) {
  const { state, dispatch } = useImageryContext();
  const mapState = state.maps.single;
  const dates = getAvailableDates(state, "single");

  // Range selection mode
  const [isRangeMode, setIsRangeMode] = useState(false);
  const [rangeStart, setRangeStart] = useState(0);
  const [rangeEnd, setRangeEnd] = useState(0);

  const handleAddTaskRange = () => {
    // Add task with range of dates
    const selectedDates = dates.slice(
      Math.min(rangeStart, rangeEnd),
      Math.max(rangeStart, rangeEnd) + 1
    );
    console.log("[MapControls] Adding task with date range:", selectedDates);
    // Pass selected date range to handler
    if (onAddTask) {
      onAddTask(selectedDates);
    }
  };

  return (
    <Card className="bg-background/95 backdrop-blur-lg shadow-lg">
      <CardContent className="p-2 space-y-1">
        {/* Source Selector, Add Task, and Range Toggle */}
        <div className="flex gap-2 items-center">
          <SourceSelector
            value={mapState.source}
            onChange={(source) => {
              console.log("[MapControls] Single view - switching to:", source);
              dispatch({ type: "SET_MAP_SOURCE", map: "single", source: source as ImagerySource });
            }}
            size="md"
            className="flex-1"
          />
          {onAddTask && dates.length > 0 && (
            <Button
              size="default"
              onClick={() => isRangeMode ? handleAddTaskRange() : onAddTask()}
              title={isRangeMode ? "Add task for date range" : "Add task for current view"}
              className="bg-orange-500 hover:bg-orange-600 text-white"
            >
              <ListPlus className="h-4 w-4 mr-1.5" />
              <span className="text-sm">
                {isRangeMode ? `Add ${Math.abs(rangeEnd - rangeStart) + 1}` : "Add to Queue"}
              </span>
            </Button>
          )}
          <Button
            size="default"
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
            <Calendar className="h-5 w-5" />
          </Button>
        </div>

        {/* Date Slider */}
        {dates.length > 0 && (
          <div className="space-y-1">
            {isRangeMode ? (
              <>
                {/* Range Mode - Single Dual-Thumb Slider */}
                <div className="flex items-center justify-center gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const newStart = Math.max(0, rangeStart - 1);
                      setRangeStart(newStart);
                      if (rangeEnd < newStart) setRangeEnd(newStart);
                    }}
                    disabled={rangeStart === 0}
                    className="h-8 w-8 p-0"
                  >
                    <ChevronLeft className="h-4 w-4" />
                  </Button>
                  <div className="text-base font-semibold text-center flex-1">
                    {dates[rangeStart]?.date} → {dates[rangeEnd]?.date}
                    <span className="ml-2 text-sm text-muted-foreground font-normal">
                      ({Math.abs(rangeEnd - rangeStart) + 1} dates)
                    </span>
                  </div>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const newEnd = Math.min(dates.length - 1, rangeEnd + 1);
                      setRangeEnd(newEnd);
                      if (rangeStart > newEnd) setRangeStart(newEnd);
                    }}
                    disabled={rangeEnd === dates.length - 1}
                    className="h-8 w-8 p-0"
                  >
                    <ChevronRight className="h-4 w-4" />
                  </Button>
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
                <div className="flex justify-between text-sm text-muted-foreground">
                  <span>{dates[0]?.date}</span>
                  <span>{dates[dates.length - 1]?.date}</span>
                </div>
              </>
            ) : (
              <>
                {/* Single Mode - One Slider */}
                <div className="flex items-center justify-center gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const newIndex = Math.max(0, mapState.dateIndex - 1);
                      dispatch({
                        type: "SET_DATE_INDEX",
                        map: "single",
                        index: newIndex,
                      });
                    }}
                    disabled={mapState.dateIndex === 0}
                    className="h-8 w-8 p-0"
                  >
                    <ChevronLeft className="h-4 w-4" />
                  </Button>
                  <div className="text-base font-semibold text-center flex-1">
                    {dates[mapState.dateIndex]?.date || "No date"}
                  </div>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const newIndex = Math.min(dates.length - 1, mapState.dateIndex + 1);
                      dispatch({
                        type: "SET_DATE_INDEX",
                        map: "single",
                        index: newIndex,
                      });
                    }}
                    disabled={mapState.dateIndex === dates.length - 1}
                    className="h-8 w-8 p-0"
                  >
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                </div>
                <Slider
                  min={0}
                  max={Math.max(0, dates.length - 1)}
                  step={1}
                  value={[mapState.dateIndex]}
                  onValueChange={(values) => {
                    const newIndex = values[0];
                    console.log("[MapControls] Single view - date index changed to:", newIndex, dates[newIndex]?.date);
                    dispatch({
                      type: "SET_DATE_INDEX",
                      map: "single",
                      index: newIndex,
                    });
                  }}
                  className="w-full"
                />
                <div className="flex justify-between text-xs text-muted-foreground px-1">
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


// Main MapControls Component
export function MapControls({
  className,
  onAddTask,
}: {
  className?: string;
  onAddTask?: (dateRange?: any[]) => void;
}) {
  const { state, dispatch } = useImageryContext();

  if (state.viewMode === "single") {
    // Single view - centered at bottom
    return (
      <div
        className={cn(
          "absolute bottom-8 left-1/2 -translate-x-1/2 z-10",
          "w-[800px]",
          className
        )}
      >
        <SingleViewTimeline onAddTask={onAddTask} />
      </div>
    );
  }

  // Split view - vertical controls at left and right center
  return (
    <>
      {/* Left Control - vertically centered on left */}
      <div
        className={cn(
          "absolute left-4 top-1/2 -translate-y-1/2 z-10",
          "w-[160px]",
          className
        )}
      >
        <Card className="bg-background/95 backdrop-blur-lg">
          <CardContent className="p-4 space-y-3 flex flex-col items-center">
            <div className="text-sm font-semibold">Left</div>

            {/* Source Selector */}
            <SourceSelector
              value={state.maps.left.source}
              onChange={(source) => {
                console.log("[MapControls] Left map - switching to:", source);
                dispatch({ type: "SET_MAP_SOURCE", map: "left", source: source as ImagerySource });
              }}
              size="sm"
              className="w-full"
            />

            {/* Date Slider - Vertical */}
            {getAvailableDates(state, "left").length > 0 && (
              <div className="flex flex-col items-center space-y-2 w-full">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const newIndex = Math.max(0, state.maps.left.dateIndex - 1);
                    dispatch({
                      type: "SET_DATE_INDEX",
                      map: "left",
                      index: newIndex,
                    });
                  }}
                  disabled={state.maps.left.dateIndex === 0}
                  className="h-8 w-8 p-0"
                >
                  <ChevronUp className="h-4 w-4" />
                </Button>
                <div className="text-sm font-medium text-center">
                  {getAvailableDates(state, "left")[state.maps.left.dateIndex]?.date}
                </div>
                <div className="flex items-center justify-center h-[300px]">
                  <Slider
                    min={0}
                    max={Math.max(0, getAvailableDates(state, "left").length - 1)}
                    step={1}
                    value={[state.maps.left.dateIndex]}
                    onValueChange={(values) => {
                      const newIndex = values[0];
                      const leftDates = getAvailableDates(state, "left");
                      console.log("[MapControls] Left map - date index changed to:", newIndex, leftDates[newIndex]?.date);
                      dispatch({
                        type: "SET_DATE_INDEX",
                        map: "left",
                        index: newIndex,
                      });
                    }}
                    orientation="vertical"
                    inverted
                    className="h-[300px]"
                  />
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const newIndex = Math.min(
                      getAvailableDates(state, "left").length - 1,
                      state.maps.left.dateIndex + 1
                    );
                    dispatch({
                      type: "SET_DATE_INDEX",
                      map: "left",
                      index: newIndex,
                    });
                  }}
                  disabled={state.maps.left.dateIndex === getAvailableDates(state, "left").length - 1}
                  className="h-8 w-8 p-0"
                >
                  <ChevronDown className="h-4 w-4" />
                </Button>
                <div className="flex flex-col items-center text-xs text-muted-foreground space-y-1">
                  <span>{getAvailableDates(state, "left")[0]?.date}</span>
                  <MoveVertical className="h-3 w-3 text-primary" />
                  <span>{getAvailableDates(state, "left")[getAvailableDates(state, "left").length - 1]?.date}</span>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Right Control - vertically centered on right */}
      <div
        className={cn(
          "absolute right-4 top-1/2 -translate-y-1/2 z-10",
          "w-[160px]",
          className
        )}
      >
        <Card className="bg-background/95 backdrop-blur-lg">
          <CardContent className="p-4 space-y-3 flex flex-col items-center">
            <div className="text-sm font-semibold">Right</div>

            {/* Source Selector */}
            <SourceSelector
              value={state.maps.right.source}
              onChange={(source) => {
                console.log("[MapControls] Right map - switching to:", source);
                dispatch({ type: "SET_MAP_SOURCE", map: "right", source: source as ImagerySource });
              }}
              size="sm"
              className="w-full"
            />

            {/* Date Slider - Vertical */}
            {getAvailableDates(state, "right").length > 0 && (
              <div className="flex flex-col items-center space-y-2 w-full">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const newIndex = Math.max(0, state.maps.right.dateIndex - 1);
                    dispatch({
                      type: "SET_DATE_INDEX",
                      map: "right",
                      index: newIndex,
                    });
                  }}
                  disabled={state.maps.right.dateIndex === 0}
                  className="h-8 w-8 p-0"
                >
                  <ChevronUp className="h-4 w-4" />
                </Button>
                <div className="text-sm font-medium text-center">
                  {getAvailableDates(state, "right")[state.maps.right.dateIndex]?.date}
                </div>
                <div className="flex items-center justify-center h-[300px]">
                  <Slider
                    min={0}
                    max={Math.max(0, getAvailableDates(state, "right").length - 1)}
                    step={1}
                    value={[state.maps.right.dateIndex]}
                    onValueChange={(values) => {
                      const newIndex = values[0];
                      const rightDates = getAvailableDates(state, "right");
                      console.log("[MapControls] Right map - date index changed to:", newIndex, rightDates[newIndex]?.date);
                      dispatch({
                        type: "SET_DATE_INDEX",
                        map: "right",
                        index: newIndex,
                      });
                    }}
                    orientation="vertical"
                    inverted
                    className="h-[300px]"
                  />
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const newIndex = Math.min(
                      getAvailableDates(state, "right").length - 1,
                      state.maps.right.dateIndex + 1
                    );
                    dispatch({
                      type: "SET_DATE_INDEX",
                      map: "right",
                      index: newIndex,
                    });
                  }}
                  disabled={state.maps.right.dateIndex === getAvailableDates(state, "right").length - 1}
                  className="h-8 w-8 p-0"
                >
                  <ChevronDown className="h-4 w-4" />
                </Button>
                <div className="flex flex-col items-center text-xs text-muted-foreground space-y-1">
                  <span>{getAvailableDates(state, "right")[0]?.date}</span>
                  <MoveVertical className="h-3 w-3 text-primary" />
                  <span>{getAvailableDates(state, "right")[getAvailableDates(state, "right").length - 1]?.date}</span>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  );
}
