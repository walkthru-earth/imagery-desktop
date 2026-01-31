import { useEffect, useState, useRef, useCallback } from "react";
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
 *
 * OPTIMIZATIONS:
 * - Increased debounce to 800ms (was 500ms) to reduce API calls during panning
 * - Abort controller to cancel stale requests
 * - Request deduplication based on viewport
 */
export function useGoogleEarthDates(
  map: maplibregl.Map | null,
  enabled: boolean
): UseGoogleEarthDatesResult {
  const [dates, setDates] = useState<GEAvailableDate[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const abortControllerRef = useRef<AbortController | null>(null);
  const lastFetchKeyRef = useRef<string>("");

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
      const bounds = mapInstance.getBounds();
      const zoom = Math.round(mapInstance.getZoom());

      // Create a key to avoid duplicate fetches for same viewport
      const fetchKey = `${zoom}-${bounds.getSouth().toFixed(3)}-${bounds.getWest().toFixed(3)}-${bounds.getNorth().toFixed(3)}-${bounds.getEast().toFixed(3)}`;
      if (fetchKey === lastFetchKeyRef.current) {
        console.log("[useGoogleEarthDates] Same viewport, skipping fetch");
        return;
      }

      // Cancel any in-flight request
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }

      try {
        setIsLoading(true);
        abortControllerRef.current = new AbortController();

        // Debounce logs to avoid flooding
        // console.log("[useGoogleEarthDates] Fetching dates for zoom:", zoom);

        const bbox = createBoundingBox(
          bounds.getSouth(),
          bounds.getWest(),
          bounds.getNorth(),
          bounds.getEast()
        );

        const fetchedDates = await api.getGoogleEarthDatesForArea(bbox, zoom);

        // Check if request was aborted
        if (abortControllerRef.current?.signal.aborted) {
          console.log("[useGoogleEarthDates] Request aborted");
          return;
        }

        lastFetchKeyRef.current = fetchKey;

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
        // Ignore abort errors
        if (error instanceof Error && error.name === 'AbortError') {
          return;
        }
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
      lastFetchKeyRef.current = "";
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
      return;
    }

    // console.log("[useGoogleEarthDates] Hook enabled, setting up listeners");

    // Create debounced version (800ms delay - better performance)
    // Increased from 500ms to reduce API calls during continuous panning
    const debouncedFetch = debounce(() => {
      fetchDates(map);
    }, 800);

    // Initial fetch
    const doInitialFetch = () => {
      // Reset fetch key to force a new fetch
      lastFetchKeyRef.current = "";
      fetchDates(map);
    };

    if (map.loaded()) {
      doInitialFetch();
    } else {
      map.once("load", doInitialFetch);
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
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, [map, enabled, fetchDates]);

  return { dates, isLoading };
}
