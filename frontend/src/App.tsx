import React, { useEffect, useRef, useCallback, useState } from "react";
import maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";

// Hooks
import { useMapInstance } from "@/hooks/useMapInstance";
import { useGoogleEarthDates } from "@/hooks/useGoogleEarthDates";
import { useEsriDates } from "@/hooks/useEsriDates";
import { useImageryLayer } from "@/hooks/useImageryLayer";

// Context
import {
  useImageryContext,
  getCurrentDate,
  getAvailableDates,
} from "@/contexts/ImageryContext";

// Components
import { MainLayout } from "@/components/Layout";
import { Header } from "@/components/Layout/Header";
import { MapControls } from "@/components/Map/MapControls";
import { MapCompare } from "@/components/Map/MapCompare";
import { AddTaskPanel } from "@/components/AddTaskPanel";
import { SettingsDialog } from "@/components/SettingsDialog";
import { TileGridOverlay } from "@/components/Map/TileGridOverlay";
import { CoordinatesOverlay } from "@/components/Map/CoordinatesOverlay";
import { MapCropOverlay } from "@/components/Map/MapCropOverlay";
import { TaskPanel } from "@/components/TaskPanel";
import { ReExportDialog } from "@/components/ReExportDialog";
import { useTheme } from "@/components/ThemeProvider";

// API & Types
import { api, createBoundingBox } from "@/services/api";
import type { CropPreview, ExportTask } from "@/types";

// RTL text plugin URL for Arabic support
const RTL_PLUGIN_URL =
  "https://unpkg.com/@mapbox/mapbox-gl-rtl-text@0.3.0/dist/mapbox-gl-rtl-text.js";

function App() {
  const { theme, setTheme } = useTheme();
  const { state, dispatch } = useImageryContext();

  // Add Task panel state
  const [isAddTaskPanelOpen, setIsAddTaskPanelOpen] = useState(false);
  const [selectedDateRange, setSelectedDateRange] = useState<any[] | null>(null);
  const [cropPreview, setCropPreview] = useState<CropPreview | null>(null);

  // Settings dialog state
  const [isSettingsDialogOpen, setIsSettingsDialogOpen] = useState(false);

  // Task panel state
  const [isTaskPanelOpen, setIsTaskPanelOpen] = useState(false);
  const [taskPanelRefreshTrigger, setTaskPanelRefreshTrigger] = useState(0);

  // Re-export dialog state
  const [reExportTask, setReExportTask] = useState<ExportTask | null>(null);
  const [isReExportDialogOpen, setIsReExportDialogOpen] = useState(false);

  // Effective theme (resolve "system" to actual light/dark)
  const effectiveTheme =
    theme === "system"
      ? window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : theme;

  // Log state changes
  useEffect(() => {
    console.log("[App] State updated:", {
      viewMode: state.viewMode,
      esriDatesCount: state.esriDates.length,
      singleSource: state.maps.single.source,
      singleGeDatesCount: state.maps.single.geDates.length,
      singleDateIndex: state.maps.single.dateIndex,
      singleStyleLoaded: state.maps.single.styleLoaded,
      leftSource: state.maps.left.source,
      leftGeDatesCount: state.maps.left.geDates.length,
      leftDateIndex: state.maps.left.dateIndex,
      leftStyleLoaded: state.maps.left.styleLoaded,
      rightSource: state.maps.right.source,
      rightGeDatesCount: state.maps.right.geDates.length,
      rightDateIndex: state.maps.right.dateIndex,
      rightStyleLoaded: state.maps.right.styleLoaded,
    });
  }, [state]);

  // Map container refs
  const singleMapRef = useRef<HTMLDivElement>(null);
  const leftMapRef = useRef<HTMLDivElement>(null);
  const rightMapRef = useRef<HTMLDivElement>(null);

  // Settings state
  const [settings, setSettings] = useState<any>(null);

  const loadSettings = async () => {
    try {
      if ((window as any).go?.main?.App?.GetSettings) {
        const s = await (window as any).go.main.App.GetSettings();
        setSettings(s);
        // Sync theme from backend settings
        if (s.theme) {
          setTheme(s.theme);
        }
        // Sync task panel state from settings
        if (s.taskPanelOpen !== undefined) {
          setIsTaskPanelOpen(s.taskPanelOpen);
        }
        // Load last map position from settings
        if (s.lastCenterLat && s.lastCenterLon) {
          dispatch({
            type: "SET_MAP_POSITION",
            center: [s.lastCenterLon, s.lastCenterLat],
            zoom: s.lastZoom || 10,
          });
        }
      }
    } catch (err) {
      console.error("Failed to load settings:", err);
    }
  };

  // ===================
  // Initialization
  // ===================
  useEffect(() => {
    // Load settings
    loadSettings();

    // RTL Plugin
    if (maplibregl.getRTLTextPluginStatus() === "unavailable") {
      maplibregl.setRTLTextPlugin(RTL_PLUGIN_URL, true);
    }

    // Start tile server
    api.startTileServer().catch((error) => {
      console.error("Failed to start tile server:", error);
    });
  }, []);

  // ===================
  // Map Instances
  // ===================
  // Use useCallback to prevent recreating callbacks on every render
  const handleSingleStyleLoad = useCallback(() => {
    console.log("[App] Single map style loaded");
    dispatch({ type: "SET_STYLE_LOADED", map: "single", loaded: true });
  }, [dispatch]);

  const handleLeftStyleLoad = useCallback(() => {
    console.log("[App] Left map style loaded");
    dispatch({ type: "SET_STYLE_LOADED", map: "left", loaded: true });
  }, [dispatch]);

  const handleRightStyleLoad = useCallback(() => {
    console.log("[App] Right map style loaded");
    dispatch({ type: "SET_STYLE_LOADED", map: "right", loaded: true });
  }, [dispatch]);

  // Get initial position from context (loaded from settings)
  const initialCenter = state.mapPosition.isLoaded
    ? state.mapPosition.center
    : undefined;
  const initialZoom = state.mapPosition.isLoaded
    ? state.mapPosition.zoom
    : undefined;

  const singleMap = useMapInstance(
    singleMapRef,
    effectiveTheme,
    handleSingleStyleLoad,
    initialCenter,
    initialZoom
  );

  const leftMap = useMapInstance(
    leftMapRef,
    effectiveTheme,
    handleLeftStyleLoad,
    initialCenter,
    initialZoom,
    "bottom-left" // Attribution on left for left map
  );

  const rightMap = useMapInstance(
    rightMapRef,
    effectiveTheme,
    handleRightStyleLoad,
    initialCenter,
    initialZoom
  );

  // ===================
  // Map Position Sync
  // ===================
  // Track position changes on moveend
  useEffect(() => {
    const activeMap = state.viewMode === "single" ? singleMap : leftMap;
    if (!activeMap) return;

    const handleMoveEnd = () => {
      const center = activeMap.getCenter();
      const zoom = activeMap.getZoom();
      dispatch({
        type: "SET_MAP_POSITION",
        center: [center.lng, center.lat],
        zoom,
      });
    };

    activeMap.on("moveend", handleMoveEnd);
    return () => {
      activeMap.off("moveend", handleMoveEnd);
    };
  }, [singleMap, leftMap, state.viewMode, dispatch]);

  // Save position on app close
  useEffect(() => {
    const handleBeforeUnload = () => {
      const map = singleMap || leftMap;
      if (map && (window as any).go?.main?.App?.SaveMapPosition) {
        const center = map.getCenter();
        // Fire and forget - can't await in beforeunload
        (window as any).go.main.App.SaveMapPosition(center.lat, center.lng, map.getZoom());
      }
    };

    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload);
    };
  }, [singleMap, leftMap]);

  // Sync position when switching from split to single view only
  // Note: We don't sync when going TO split view because MapCompare handles the map sync
  const mapPositionRef = React.useRef(state.mapPosition);
  mapPositionRef.current = state.mapPosition;

  const prevViewModeRef = React.useRef(state.viewMode);

  useEffect(() => {
    const prevMode = prevViewModeRef.current;
    prevViewModeRef.current = state.viewMode;

    // Only sync when switching FROM split TO single (not the other way)
    if (prevMode === "split" && state.viewMode === "single") {
      const { center, zoom, isLoaded } = mapPositionRef.current;
      if (!isLoaded) return;

      // Small delay to ensure single map is ready
      const timer = setTimeout(() => {
        if (singleMap && singleMap.loaded()) {
          singleMap.jumpTo({ center, zoom });
        }
      }, 100);

      return () => clearTimeout(timer);
    }
  }, [state.viewMode, singleMap]);

  // ===================
  // Esri Dates (Per Map - with local changes detection)
  // ===================
  // Use viewport-based Esri dates that only show dates with actual imagery changes
  // No global fallback - wait for viewport-specific dates to load
  const { dates: singleEsriDates, isLoading: singleEsriLoading } = useEsriDates(
    singleMap,
    state.viewMode === "single" && state.maps.single.source === "esri"
  );

  const { dates: leftEsriDates, isLoading: leftEsriLoading } = useEsriDates(
    leftMap,
    state.viewMode === "split" && state.maps.left.source === "esri"
  );

  const { dates: rightEsriDates, isLoading: rightEsriLoading } = useEsriDates(
    rightMap,
    state.viewMode === "split" && state.maps.right.source === "esri"
  );

  // Track if any Esri dates are loading
  const esriDatesLoading =
    (state.viewMode === "single" && state.maps.single.source === "esri" && singleEsriLoading) ||
    (state.viewMode === "split" && state.maps.left.source === "esri" && leftEsriLoading) ||
    (state.viewMode === "split" && state.maps.right.source === "esri" && rightEsriLoading);

  // Dispatch loading state to context
  useEffect(() => {
    dispatch({ type: "SET_ESRI_DATES_LOADING", loading: esriDatesLoading });
  }, [esriDatesLoading, dispatch]);

  // Update context when Esri dates change (use active map's dates as shared state)
  // Dispatch whenever dates change AND source is esri (even if empty, to replace global dates)
  useEffect(() => {
    if (state.viewMode === "single" && state.maps.single.source === "esri") {
      console.log("[App] Single map Esri dates changed:", singleEsriDates.length);
      if (singleEsriDates.length > 0) {
        console.log("[App] Dispatching SET_ESRI_DATES from single map");
        dispatch({ type: "SET_ESRI_DATES", dates: singleEsriDates });
      }
    }
  }, [singleEsriDates, state.viewMode, state.maps.single.source, dispatch]);

  useEffect(() => {
    if (state.viewMode === "split" && state.maps.left.source === "esri") {
      console.log("[App] Left map Esri dates changed:", leftEsriDates.length);
      if (leftEsriDates.length > 0) {
        console.log("[App] Dispatching SET_ESRI_DATES from left map");
        dispatch({ type: "SET_ESRI_DATES", dates: leftEsriDates });
      }
    }
  }, [leftEsriDates, state.viewMode, state.maps.left.source, dispatch]);

  useEffect(() => {
    if (state.viewMode === "split" && state.maps.right.source === "esri") {
      console.log("[App] Right map Esri dates changed:", rightEsriDates.length);
      if (rightEsriDates.length > 0) {
        console.log("[App] Dispatching SET_ESRI_DATES from right map");
        dispatch({ type: "SET_ESRI_DATES", dates: rightEsriDates });
      }
    }
  }, [rightEsriDates, state.viewMode, state.maps.right.source, dispatch]);

  // ===================
  // Google Earth Dates (Per Map)
  // ===================
  const { dates: singleGeDates, isLoading: singleGeLoading } = useGoogleEarthDates(
    singleMap,
    state.viewMode === "single" && state.maps.single.source === "google"
  );

  const { dates: leftGeDates, isLoading: leftGeLoading } = useGoogleEarthDates(
    leftMap,
    state.viewMode === "split" && state.maps.left.source === "google"
  );

  const { dates: rightGeDates, isLoading: rightGeLoading } = useGoogleEarthDates(
    rightMap,
    state.viewMode === "split" && state.maps.right.source === "google"
  );

  // Track if any GE dates are loading
  const geDatesLoading =
    (state.viewMode === "single" && state.maps.single.source === "google" && singleGeLoading) ||
    (state.viewMode === "split" && state.maps.left.source === "google" && leftGeLoading) ||
    (state.viewMode === "split" && state.maps.right.source === "google" && rightGeLoading);

  // Dispatch GE loading state to context
  useEffect(() => {
    dispatch({ type: "SET_GE_DATES_LOADING", loading: geDatesLoading });
  }, [geDatesLoading, dispatch]);

  // Update context when dates change
  useEffect(() => {
    console.log("[App] Single map GE dates changed:", singleGeDates.length);
    if (singleGeDates.length > 0) {
      console.log("[App] Dispatching UPDATE_GE_DATES for single map");
      dispatch({ type: "UPDATE_GE_DATES", map: "single", dates: singleGeDates });
    }
  }, [singleGeDates, dispatch]);

  useEffect(() => {
    console.log("[App] Left map GE dates changed:", leftGeDates.length);
    if (leftGeDates.length > 0) {
      console.log("[App] Dispatching UPDATE_GE_DATES for left map");
      dispatch({ type: "UPDATE_GE_DATES", map: "left", dates: leftGeDates });
    }
  }, [leftGeDates, dispatch]);

  useEffect(() => {
    console.log("[App] Right map GE dates changed:", rightGeDates.length);
    if (rightGeDates.length > 0) {
      console.log("[App] Dispatching UPDATE_GE_DATES for right map");
      dispatch({ type: "UPDATE_GE_DATES", map: "right", dates: rightGeDates });
    }
  }, [rightGeDates, dispatch]);

  // ===================
  // Imagery Layers
  // ===================
  useImageryLayer(
    singleMap,
    state.maps.single.source,
    getCurrentDate(state, "single"),
    state.layers.imagery.opacity
  );

  useImageryLayer(
    leftMap,
    state.maps.left.source,
    getCurrentDate(state, "left"),
    state.layers.imagery.opacity
  );

  useImageryLayer(
    rightMap,
    state.maps.right.source,
    getCurrentDate(state, "right"),
    state.layers.imagery.opacity
  );



  // ===================
  // Bbox Layer (Single View)
  // ===================
  useEffect(() => {
    if (!singleMap || state.viewMode !== "single") return;

    // Add bbox source and layers if they don't exist
    if (!singleMap.getSource("bbox")) {
      singleMap.addSource("bbox", {
        type: "geojson",
        data: {
          type: "Feature",
          properties: {},
          geometry: {
            type: "Polygon",
            coordinates: [[]],
          },
        },
      });

      singleMap.addLayer({
        id: "bbox-fill",
        type: "fill",
        source: "bbox",
        paint: {
          "fill-color": "#3b82f6",
          "fill-opacity": state.layers.bbox.opacity,
        },
      });

      singleMap.addLayer({
        id: "bbox-line",
        type: "line",
        source: "bbox",
        paint: {
          "line-color": "#3b82f6",
          "line-width": 2,
        },
      });
    }

    // Update visibility
    if (singleMap.getLayer("bbox-fill")) {
      singleMap.setLayoutProperty(
        "bbox-fill",
        "visibility",
        state.layers.bbox.visible ? "visible" : "none"
      );
      singleMap.setPaintProperty(
        "bbox-fill",
        "fill-opacity",
        state.layers.bbox.opacity
      );
    }

    if (singleMap.getLayer("bbox-line")) {
      singleMap.setLayoutProperty(
        "bbox-line",
        "visibility",
        state.layers.bbox.visible ? "visible" : "none"
      );
    }
  }, [singleMap, state.viewMode, state.layers.bbox]);

  // ===================
  // Add Task Handler
  // ===================
  const handleAddTask = async (dateRange?: any[]) => {
    console.log("Add task triggered - opening panel", { dateRange });
    setSelectedDateRange(dateRange || null);
    setIsAddTaskPanelOpen(true);
  };

  // Helper to get current map's bbox
  const getCurrentBbox = () => {
    const currentMap = state.viewMode === "single" ? singleMap : leftMap;
    if (!currentMap) return null;

    const bounds = currentMap.getBounds();
    return createBoundingBox(
      bounds.getSouth(),
      bounds.getWest(),
      bounds.getNorth(),
      bounds.getEast()
    );
  };

  // Helper to get task data for current view
  const getTaskData = () => {
    if (state.viewMode !== "single") {
      return null; // Task creation only available in single view for now
    }

    const mapState = state.maps.single;
    const dates = getAvailableDates(state, "single");
    const currentDate = getCurrentDate(state, "single");
    const currentMap = singleMap;

    if (!currentMap || !currentDate) return null;

    const bbox = getCurrentBbox();
    const zoom = Math.round(currentMap.getZoom());

    if (mapState.source === "esri") {
      // Only pass dateRange if user explicitly selected a range
      const dateRange = selectedDateRange && selectedDateRange.length > 1
        ? selectedDateRange.map(d => ({ date: d.date }))
        : undefined;

      return {
        bbox,
        zoom,
        source: "esri" as const,
        singleDate: currentDate.date,
        dateRange,
      };
    } else {
      const geDate = currentDate as import("@/types").GEAvailableDate;

      // Only pass dateRange if user explicitly selected a range
      const dateRange = selectedDateRange && selectedDateRange.length > 1
        ? (selectedDateRange as import("@/types").GEAvailableDate[]).map(d => ({
            date: d.date,
            hexDate: d.hexDate,
            epoch: d.epoch,
          }))
        : undefined;

      return {
        bbox,
        zoom,
        source: "google" as const,
        singleDate: geDate.date,
        singleHexDate: geDate.hexDate,
        singleEpoch: geDate.epoch,
        dateRange,
      };
    }
  };

  // ===================
  // Render
  // ===================
  return (
    <MainLayout
      header={
        <Header
          viewMode={state.viewMode}
          onViewModeChange={(mode) => {
            console.log("[App] View mode changed to:", mode);
            dispatch({ type: "SET_VIEW_MODE", mode });
          }}
          onOpenSettings={() => setIsSettingsDialogOpen(true)}
        />
      }
    >
      <div className="relative w-full h-full">
        {/* Single map view */}
        <div
          ref={singleMapRef}
          className="absolute inset-0 overflow-hidden"
          style={{ display: state.viewMode === "single" ? "block" : "none" }}
        >
          {/* Crop overlay for video preview - only shown when video option enabled */}
          {cropPreview && state.viewMode === "single" && (
            <MapCropOverlay
              visible={true}
              crop={cropPreview}
              onChange={setCropPreview}
              containerRef={singleMapRef}
            />
          )}
        </div>

        {/* Split map view */}
        <div
          id="compare-container"
          style={{
            position: "absolute",
            inset: 0,
            display: state.viewMode === "split" ? "block" : "none",
          }}
        >
          <div
            id="before-map"
            ref={leftMapRef}
            style={{ position: "absolute", inset: 0, width: "100%" }}
          />
          <div
            id="after-map"
            ref={rightMapRef}
            style={{ position: "absolute", inset: 0, width: "100%" }}
          />
        </div>

        {state.viewMode === "split" && (
          <MapCompare
            leftMap={leftMap}
            rightMap={rightMap}
            orientation="vertical"
          />
        )}

        {/* Map Controls */}
        <MapControls onAddTask={handleAddTask} />

        {/* Add Task Panel */}
        <AddTaskPanel
          isOpen={isAddTaskPanelOpen}
          onClose={() => {
            setIsAddTaskPanelOpen(false);
            setCropPreview(null);
          }}
          onCropChange={setCropPreview}
          onTaskAdded={() => setTaskPanelRefreshTrigger(prev => prev + 1)}
          {...(getTaskData() || {
            bbox: null,
            zoom: 10,
            source: "esri",
          })}
        />

        {/* Settings Dialog */}
        <SettingsDialog
          isOpen={isSettingsDialogOpen}
          onClose={() => {
            setIsSettingsDialogOpen(false);
            // Refresh settings when dialog closes
            loadSettings();
          }}
        />

        {/* Coordinates Overlay */}
        {settings?.showCoordinates && (
          <CoordinatesOverlay maps={[singleMap, leftMap, rightMap]} />
        )}

        {/* Tile Grid Overlays */}
        <TileGridOverlay map={singleMap} visible={settings?.showTileGrid ?? false} />
        <TileGridOverlay map={leftMap} visible={settings?.showTileGrid ?? false} />
        <TileGridOverlay map={rightMap} visible={settings?.showTileGrid ?? false} />

        {/* Task Panel */}
        <TaskPanel
          isOpen={isTaskPanelOpen}
          onToggle={() => setIsTaskPanelOpen(!isTaskPanelOpen)}
          refreshTrigger={taskPanelRefreshTrigger}
          onTaskSelect={(task) => {
            // Fly to task's bbox location
            const map = state.viewMode === "single" ? singleMap : leftMap;
            if (map && task.bbox) {
              map.fitBounds(
                [
                  [task.bbox.west, task.bbox.south],
                  [task.bbox.east, task.bbox.north],
                ],
                { padding: 50, duration: 1000 }
              );
            }
          }}
          onReExport={(task) => {
            setReExportTask(task);
            setIsReExportDialogOpen(true);
          }}
        />

        {/* Re-export Dialog */}
        <ReExportDialog
          task={reExportTask}
          isOpen={isReExportDialogOpen}
          onClose={() => {
            setIsReExportDialogOpen(false);
            setReExportTask(null);
          }}
          onSuccess={() => setTaskPanelRefreshTrigger(prev => prev + 1)}
        />
      </div>
    </MainLayout>
  );
}

export default App;

