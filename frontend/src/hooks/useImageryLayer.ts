import { useEffect } from "react";
import maplibregl from "maplibre-gl";
import { api } from "@/services/api";
import type { AvailableDate, GEAvailableDate, ImagerySource } from "@/types";

/**
 * Hook to manage imagery layers on a MapLibre GL map
 * Handles both Esri and Google Earth imagery sources
 * Automatically updates layer when date or source changes
 */
export function useImageryLayer(
  map: maplibregl.Map | null,
  source: ImagerySource,
  date: AvailableDate | GEAvailableDate | null,
  opacity: number = 1
) {
  useEffect(() => {
    // console.log("[useImageryLayer] Effect triggered:", {
    //   hasMap: !!map,
    //   source,
    //   hasDate: !!date,
    //   dateValue: date?.date,
    //   opacity,
    // });

    if (!map || !date) {
      // console.log("[useImageryLayer] No map or date, removing layer");
      // Remove layer if no date selected
      if (map) {
        if (map.getLayer("imagery-layer")) {
          map.removeLayer("imagery-layer");
        }
        if (map.getSource("imagery-source")) {
          map.removeSource("imagery-source");
        }
      }
      return;
    }

    const layerId = "imagery-layer";
    const sourceId = "imagery-source";

    const addLayer = async (retryCount = 0) => {
      // console.log("[useImageryLayer] Adding layer for date:", date.date, "Retry:", retryCount);

      // Remove existing layer if this is the first attempt (or we are re-trying completely)
      if (retryCount === 0) {
          if (map.getLayer(layerId)) map.removeLayer(layerId);
          if (map.getSource(sourceId)) map.removeSource(sourceId);
      }

      try {
        // Get tile URL based on source
        let tileURL: string;

        if (source === "esri") {
          tileURL = await api.getEsriTileURL(date.date);
          // console.log("[useImageryLayer] Esri tile URL:", tileURL);
        } else {
          // Google Earth historical
          const geDate = date as GEAvailableDate;
          tileURL = await api.getGoogleEarthHistoricalTileURL(
            geDate.hexDate,
            geDate.epoch
          );
          // console.log("[useImageryLayer] GE tile URL:", tileURL);
        }

        // Check if map is still valid after async wait
        if (!map || !map.getStyle()) return;

        // If URL is invalid, retry?
        if (!tileURL || tileURL === "") {
             throw new Error("Empty tile URL returned");
        }

        // Add source
        if (!map.getSource(sourceId)) {
            map.addSource(sourceId, {
            type: "raster",
            tiles: [tileURL],
            tileSize: 256,
            attribution:
                source === "esri"
                ? "&copy; Esri World Imagery Wayback"
                : "Google Earth",
            });
        }

        // Add layer (before bbox-fill or custom grid if they exist)
        let beforeLayer = undefined;
        if (map.getLayer("custom-tile-grid-lines")) {
            beforeLayer = "custom-tile-grid-lines";
        } else if (map.getLayer("bbox-fill")) {
            beforeLayer = "bbox-fill";
        }

        if (!map.getLayer(layerId)) {
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
        }
        // console.log("[useImageryLayer] Layer added successfully");
      } catch (error) {
        console.error("[useImageryLayer] Error loading imagery layer:", error);
        
        // Retry logic (max 3 retries)
        if (retryCount < 3) {
            // console.log(`[useImageryLayer] Retrying in ${1000 * (retryCount + 1)}ms...`);
            setTimeout(() => addLayer(retryCount + 1), 1000 * (retryCount + 1));
        }
      }
    };

    addLayer();

    // Re-add layer if style changes (e.g. theme switch) wipes it out
    const onStyleData = () => {
        if (map && map.getStyle() && !map.getSource(sourceId)) {
            // console.log("[useImageryLayer] Style changed, re-adding layer");
            addLayer();
        }
    };
    
    map.on("styledata", onStyleData);

    return () => {
        map.off("styledata", onStyleData);
    };
  }, [map, source, date, opacity]);
}
