import { useEffect, useState, useCallback } from "react";
import maplibregl from "maplibre-gl";
import { debounce } from "@/utils/debounce";
import { api, createBoundingBox } from "@/services/api";
import type { GEAvailableDate } from "@/types";

interface UseGoogleEarthDatesResult {
  dates: GEAvailableDate[];
  isLoading: boolean;
}

/**
 * Hook to automatically fetch Google Earth dates based on map viewport
 * Debounces requests to avoid excessive API calls during map movement
 */
export function useGoogleEarthDates(
  map: maplibregl.Map | null,
  enabled: boolean
): UseGoogleEarthDatesResult {
  const [dates, setDates] = useState<GEAvailableDate[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  // Check if dates are equal to avoid unnecessary updates
  const areDatesEqual = (d1: GEAvailableDate[], d2: GEAvailableDate[]) => {
    if (d1.length !== d2.length) return false;
    for (let i = 0; i < d1.length; i++) {
      if (
        d1[i].date !== d2[i].date ||
        d1[i].epoch !== d2[i].epoch ||
        d1[i].hexDate !== d2[i].hexDate
      ) {
        return false;
      }
    }
    return true;
  };

  // Memoize the fetch function to avoid recreating on every render
  const fetchDates = useCallback(
    async (mapInstance: maplibregl.Map) => {
      try {
        setIsLoading(true);
        const bounds = mapInstance.getBounds();
        const zoom = Math.round(mapInstance.getZoom());

        // Debounce logs to avoid flooding
        // console.log("[useGoogleEarthDates] Fetching dates for zoom:", zoom);

        const bbox = createBoundingBox(
          bounds.getSouth(),
          bounds.getWest(),
          bounds.getNorth(),
          bounds.getEast()
        );

        const fetchedDates = await api.getGoogleEarthDatesForArea(bbox, zoom);

        setDates((prevDates) => {
            const newDates = fetchedDates || [];
            if (areDatesEqual(prevDates, newDates)) {
                // console.log("[useGoogleEarthDates] Dates unchanged, skipping update");
                return prevDates;
            }
            console.log("[useGoogleEarthDates] Dates changed, updating. Count:", newDates.length);
            return newDates;
        });
      } catch (error) {
        console.error("[useGoogleEarthDates] Error fetching dates:", error);
        setDates([]);
      } finally {
        setIsLoading(false);
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

    // console.log("[useGoogleEarthDates] Hook enabled, setting up listeners");

    // Create debounced version (500ms delay)
    // Reduce debounce to make UI snappier but keep it to prevent rapid API calls
    const debouncedFetch = debounce(() => {
      fetchDates(map);
    }, 500);

    // Initial fetch
    if (map.loaded()) {
      fetchDates(map);
    } else {
      map.once("load", () => {
         fetchDates(map);
      });
    }

    // Fetch on map movement
    map.on("moveend", debouncedFetch);
    
    // REMOVED 'idle' listener as it fires too frequently when tiles load,
    // causing an update loop with the imagery layer
    // map.on("idle", debouncedFetch); 

    // console.log("[useGoogleEarthDates] Added moveend listener");

    // Cleanup
    return () => {
      // console.log("[useGoogleEarthDates] Cleaning up listeners");
      map.off("moveend", debouncedFetch);
      // map.off("idle", debouncedFetch);
    };
  }, [map, enabled, fetchDates]);

  return { dates, isLoading };
}
