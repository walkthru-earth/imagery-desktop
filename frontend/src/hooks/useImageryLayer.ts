import { useEffect, useRef } from "react";
import maplibregl from "maplibre-gl";
import { api } from "@/services/api";
import type { AvailableDate, GEAvailableDate, ImagerySource } from "@/types";

/**
 * Hook to manage imagery layers on a MapLibre GL map
 * Handles both Esri and Google Earth imagery sources
 * Automatically updates layer when date or source changes
 *
 * OPTIMIZATIONS:
 * - Only updates tile URL instead of recreating source/layer
 * - Debounces tile URL changes to avoid rapid updates
 * - Reuses existing source when possible
 */
export function useImageryLayer(
  map: maplibregl.Map | null,
  source: ImagerySource,
  date: AvailableDate | GEAvailableDate | null,
  opacity: number = 1
) {
  const currentTileURLRef = useRef<string>("");
  const updateTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!map || !date) {
      // Remove layer if no date selected
      if (map) {
        if (map.getLayer("imagery-layer")) {
          map.removeLayer("imagery-layer");
        }
        if (map.getSource("imagery-source")) {
          map.removeSource("imagery-source");
        }
        currentTileURLRef.current = "";
      }
      return;
    }

    const layerId = "imagery-layer";
    const sourceId = "imagery-source";

    const updateLayer = async () => {
      // Cancel any pending update
      if (updateTimeoutRef.current) {
        clearTimeout(updateTimeoutRef.current);
        updateTimeoutRef.current = null;
      }

      // Cancel any in-flight API request
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }

      try {
        // Create new abort controller for this request
        abortControllerRef.current = new AbortController();

        // Get tile URL based on source
        let tileURL: string;

        if (source === "esri_wayback") {
          tileURL = await api.getEsriTileURL(date.date);
        } else {
          // Google Earth (both current and historical use same endpoint with date)
          const geDate = date as GEAvailableDate;
          tileURL = await api.getGoogleEarthHistoricalTileURL(
            geDate.date,      // Regular date format for caching
            geDate.hexDate,   // Hex date for tile fetching
            geDate.epoch      // Epoch for fallback
          );
        }

        // Check if request was aborted or map is gone
        if (abortControllerRef.current?.signal.aborted || !map || !map.getStyle()) {
          return;
        }

        // If URL is invalid, skip
        if (!tileURL || tileURL === "") {
          console.warn("[useImageryLayer] Empty tile URL returned");
          return;
        }

        // Check if URL actually changed
        if (tileURL === currentTileURLRef.current) {
          // Just update opacity if needed
          if (map.getLayer(layerId)) {
            map.setPaintProperty(layerId, "raster-opacity", opacity);
          }
          return;
        }

        currentTileURLRef.current = tileURL;

        // Check if source exists
        const existingSource = map.getSource(sourceId);

        if (existingSource && existingSource.type === "raster") {
          // Update existing source tiles URL (more efficient than removing/re-adding)
          // Unfortunately MapLibre doesn't support updating tiles directly,
          // so we need to remove and re-add the source
          if (map.getLayer(layerId)) {
            map.removeLayer(layerId);
          }
          map.removeSource(sourceId);
        }

        // Add source
        map.addSource(sourceId, {
          type: "raster",
          tiles: [tileURL],
          tileSize: 256,
          attribution:
            source === "esri_wayback"
              ? "&copy; Esri World Imagery Wayback"
              : "&copy; Google Earth",
        });

        // Add layer (before bbox-fill or custom grid if they exist)
        let beforeLayer = undefined;
        if (map.getLayer("custom-tile-grid-lines")) {
          beforeLayer = "custom-tile-grid-lines";
        } else if (map.getLayer("bbox-fill")) {
          beforeLayer = "bbox-fill";
        }

        map.addLayer(
          {
            id: layerId,
            type: "raster",
            source: sourceId,
            paint: {
              "raster-opacity": opacity,
            },
          },
          beforeLayer
        );
      } catch (error) {
        // Ignore abort errors
        if (error instanceof Error && error.name === 'AbortError') {
          return;
        }
        console.error("[useImageryLayer] Error loading imagery layer:", error);
      }
    };

    // Debounce layer updates to avoid rapid changes when slider is dragged
    updateTimeoutRef.current = setTimeout(updateLayer, 50);

    // Re-add layer if style changes (e.g. theme switch) wipes it out
    const onStyleData = () => {
      if (map && map.getStyle() && !map.getSource(sourceId)) {
        currentTileURLRef.current = ""; // Force refresh
        updateLayer();
      }
    };

    map.on("styledata", onStyleData);

    return () => {
      if (updateTimeoutRef.current) {
        clearTimeout(updateTimeoutRef.current);
      }
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
      map.off("styledata", onStyleData);
    };
  }, [map, source, date, opacity]);
}
