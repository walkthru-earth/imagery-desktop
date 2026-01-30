import React, { useEffect, useRef, useCallback } from "react";
import maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";

// Hooks
import { useMapInstance } from "@/hooks/useMapInstance";
import { useGoogleEarthDates } from "@/hooks/useGoogleEarthDates";
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
import { ExportDialog } from "@/components/ExportDialog";
import { SettingsDialog } from "@/components/SettingsDialog";
import { useTheme } from "@/components/ThemeProvider";

// API & Types
import { api, createBoundingBox } from "@/services/api";
import type { DownloadProgress } from "@/types";
import { useState } from "react";

// RTL text plugin URL for Arabic support
const RTL_PLUGIN_URL =
  "https://unpkg.com/@mapbox/mapbox-gl-rtl-text@0.3.0/dist/mapbox-gl-rtl-text.js";

function App() {
  const { theme } = useTheme();
  const { state, dispatch } = useImageryContext();

  // Download progress
  const [downloadProgress, setDownloadProgress] =
    useState<DownloadProgress | null>(null);

  // Export dialog state
  const [isExportDialogOpen, setIsExportDialogOpen] = useState(false);

  // Settings dialog state
  const [isSettingsDialogOpen, setIsSettingsDialogOpen] = useState(false);

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

  // ===================
  // Initialization
  // ===================
  useEffect(() => {
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

  const singleMap = useMapInstance(
    singleMapRef,
    effectiveTheme,
    handleSingleStyleLoad
  );

  const leftMap = useMapInstance(
    leftMapRef,
    effectiveTheme,
    handleLeftStyleLoad
  );

  const rightMap = useMapInstance(
    rightMapRef,
    effectiveTheme,
    handleRightStyleLoad
  );

  // ===================
  // Esri Dates (Shared)
  // ===================
  useEffect(() => {
    console.log("[App] Fetching Esri dates on mount");
    api
      .getEsriLayers()
      .then((dates) => {
        console.log("[App] Esri dates fetched:", dates?.length || 0);
        if (dates && dates.length > 0) {
          console.log("[App] Dispatching SET_ESRI_DATES");
          dispatch({ type: "SET_ESRI_DATES", dates });
        }
      })
      .catch((error) => {
        console.error("[App] Failed to fetch Esri dates:", error);
      });
  }, [dispatch]);

  // ===================
  // Google Earth Dates (Per Map)
  // ===================
  const singleGeDates = useGoogleEarthDates(
    singleMap,
    state.viewMode === "single" && state.maps.single.source === "google"
  );

  const leftGeDates = useGoogleEarthDates(
    leftMap,
    state.viewMode === "split" && state.maps.left.source === "google"
  );

  const rightGeDates = useGoogleEarthDates(
    rightMap,
    state.viewMode === "split" && state.maps.right.source === "google"
  );

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
  // Download Progress
  // ===================
  useEffect(() => {
    const unsubscribe = api.onDownloadProgress((progress: DownloadProgress) => {
      setDownloadProgress(progress);
    });
    return unsubscribe;
  }, []);

  // ===================
  // Export Handler
  // ===================
  const handleExport = async () => {
    console.log("Export triggered - opening export dialog");
    setIsExportDialogOpen(true);
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

  // Helper to get export data for current view
  const getExportData = () => {
    if (state.viewMode !== "single") {
      return null; // Export only available in single view for now
    }

    const mapState = state.maps.single;
    const dates = getAvailableDates(state, "single");
    const currentDate = getCurrentDate(state, "single");
    const currentMap = singleMap;

    if (!currentMap || !currentDate) return null;

    const bbox = getCurrentBbox();
    const zoom = Math.round(currentMap.getZoom());

    if (mapState.source === "esri") {
      return {
        bbox,
        zoom,
        source: "esri" as const,
        singleDate: currentDate.date,
      };
    } else {
      const geDate = currentDate as import("@/types").GEAvailableDate;
      return {
        bbox,
        zoom,
        source: "google" as const,
        singleDate: geDate.date,
        singleHexDate: geDate.hexDate,
        singleEpoch: geDate.epoch,
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
          showExportOptions={state.viewMode === "single"}
          isRangeMode={false}
          exportOptions={{ mergedGeotiff: true, tiles: false, mp4: false, gif: false }}
          onExportOptionsChange={() => {}}
          onExport={handleExport}
          onOpenSettings={() => setIsSettingsDialogOpen(true)}
        />
      }
    >
      <div className="relative w-full h-full">
        {/* Single map view */}
        <div
          ref={singleMapRef}
          className="absolute inset-0"
          style={{ display: state.viewMode === "single" ? "block" : "none" }}
        />

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
        <MapControls onExport={handleExport} />

        {/* Download Progress */}
        {downloadProgress &&
          downloadProgress.percent > 0 &&
          downloadProgress.percent < 100 && (
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 bg-background/95 backdrop-blur-lg border border-border rounded-lg shadow-2xl p-6 min-w-[320px]">
              <h3 className="text-lg font-semibold mb-3">Downloading</h3>
              <div className="space-y-2">
                <div className="w-full bg-muted rounded-full h-2 overflow-hidden">
                  <div
                    className="h-full bg-primary transition-all duration-300"
                    style={{ width: `${downloadProgress.percent}%` }}
                  />
                </div>
                <div className="flex justify-between text-sm">
                  <span className="font-medium">
                    {downloadProgress.percent.toFixed(1)}%
                  </span>
                  <span className="text-muted-foreground">
                    {downloadProgress.downloaded} / {downloadProgress.total}
                  </span>
                </div>
                <p className="text-xs text-center text-muted-foreground mt-2">
                  {downloadProgress.status}
                </p>
              </div>
            </div>
          )}

        {/* Export Dialog */}
        <ExportDialog
          isOpen={isExportDialogOpen}
          onClose={() => setIsExportDialogOpen(false)}
          {...(getExportData() || {
            bbox: null,
            zoom: 10,
            source: "esri",
          })}
        />

        {/* Settings Dialog */}
        <SettingsDialog
          isOpen={isSettingsDialogOpen}
          onClose={() => setIsSettingsDialogOpen(false)}
        />
      </div>
    </MainLayout>
  );
}

export default App;
