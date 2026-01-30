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
    console.log("[useImageryLayer] Effect triggered:", {
      hasMap: !!map,
      source,
      hasDate: !!date,
      dateValue: date?.date,
      opacity,
    });

    if (!map || !date) {
      console.log("[useImageryLayer] No map or date, removing layer");
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

    const addLayer = async () => {
      console.log("[useImageryLayer] Adding layer for date:", date.date);

      // Remove existing layer
      if (map.getLayer(layerId)) {
        console.log("[useImageryLayer] Removing existing layer");
        map.removeLayer(layerId);
      }
      if (map.getSource(sourceId)) {
        console.log("[useImageryLayer] Removing existing source");
        map.removeSource(sourceId);
      }

      try {
        // Get tile URL based on source
        let tileURL: string;

        if (source === "esri") {
          console.log("[useImageryLayer] Getting Esri tile URL for date:", date.date);
          tileURL = await api.getEsriTileURL(date.date);
          console.log("[useImageryLayer] Esri tile URL:", tileURL);
        } else {
          // Google Earth historical
          const geDate = date as GEAvailableDate;
          console.log("[useImageryLayer] Getting GE tile URL:", {
            hexDate: geDate.hexDate,
            epoch: geDate.epoch,
          });
          tileURL = await api.getGoogleEarthHistoricalTileURL(
            geDate.hexDate,
            geDate.epoch
          );
          console.log("[useImageryLayer] GE tile URL:", tileURL);
        }

        // Add source
        console.log("[useImageryLayer] Adding raster source");
        map.addSource(sourceId, {
          type: "raster",
          tiles: [tileURL],
          tileSize: 256,
          attribution:
            source === "esri"
              ? "&copy; Esri World Imagery Wayback"
              : "Google Earth",
        });

        // Add layer (before bbox-fill or custom grid if they exist)
        let beforeLayer = undefined;
        if (map.getLayer("custom-tile-grid-lines")) {
            beforeLayer = "custom-tile-grid-lines";
        } else if (map.getLayer("bbox-fill")) {
            beforeLayer = "bbox-fill";
        }

        console.log("[useImageryLayer] Adding raster layer, beforeLayer:", beforeLayer);
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
        console.log("[useImageryLayer] Layer added successfully");
      } catch (error) {
        console.error("[useImageryLayer] Error loading imagery layer:", error);
      }
    };

    addLayer();
  }, [map, source, date, opacity]);
}
