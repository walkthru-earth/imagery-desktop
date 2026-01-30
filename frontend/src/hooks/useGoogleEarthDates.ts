import { useEffect, useState, useCallback } from "react";
import maplibregl from "maplibre-gl";
import { debounce } from "@/utils/debounce";
import { api, createBoundingBox } from "@/services/api";
import type { GEAvailableDate } from "@/types";

/**
 * Hook to automatically fetch Google Earth dates based on map viewport
 * Debounces requests to avoid excessive API calls during map movement
 */
export function useGoogleEarthDates(
  map: maplibregl.Map | null,
  enabled: boolean
): GEAvailableDate[] {
  const [dates, setDates] = useState<GEAvailableDate[]>([]);

  // Memoize the fetch function to avoid recreating on every render
  const fetchDates = useCallback(
    async (mapInstance: maplibregl.Map) => {
      try {
        const bounds = mapInstance.getBounds();
        const zoom = Math.round(mapInstance.getZoom());

        console.log("[useGoogleEarthDates] Fetching dates for zoom:", zoom);
        console.log("[useGoogleEarthDates] Bounds:", {
          south: bounds.getSouth(),
          west: bounds.getWest(),
          north: bounds.getNorth(),
          east: bounds.getEast(),
        });

        const bbox = createBoundingBox(
          bounds.getSouth(),
          bounds.getWest(),
          bounds.getNorth(),
          bounds.getEast()
        );

        const fetchedDates = await api.getGoogleEarthDatesForArea(bbox, zoom);
        console.log("[useGoogleEarthDates] Fetched dates:", fetchedDates?.length || 0);

        if (fetchedDates && fetchedDates.length > 0) {
          console.log("[useGoogleEarthDates] Setting dates:", fetchedDates);
          setDates(fetchedDates);
        } else {
          console.log("[useGoogleEarthDates] No dates available, clearing");
          setDates([]);
        }
      } catch (error) {
        console.error("[useGoogleEarthDates] Error fetching dates:", error);
        setDates([]);
      }
    },
    [] // No dependencies, function is stable
  );

  useEffect(() => {
    if (!map || !enabled) {
      console.log("[useGoogleEarthDates] Hook disabled or no map");
      // Clear dates when disabled
      setDates([]);
      return;
    }

    console.log("[useGoogleEarthDates] Hook enabled, setting up listeners");

    // Create debounced version (500ms delay)
    const debouncedFetch = debounce(() => {
      console.log("[useGoogleEarthDates] Map moveend triggered (debounced)");
      fetchDates(map);
    }, 500);

    // Initial fetch
    if (map.loaded()) {
      console.log("[useGoogleEarthDates] Map loaded, fetching initial dates");
      fetchDates(map);
    } else {
      console.log("[useGoogleEarthDates] Map not yet loaded, waiting for load event");
    }

    // Fetch on map movement (debounced)
    map.on("moveend", debouncedFetch);
    console.log("[useGoogleEarthDates] Added moveend listener");

    // Cleanup
    return () => {
      console.log("[useGoogleEarthDates] Cleaning up moveend listener");
      map.off("moveend", debouncedFetch);
    };
  }, [map, enabled, fetchDates]);

  return dates;
}
