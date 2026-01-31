import { useEffect, useState, useRef, useCallback } from "react";
import maplibregl from "maplibre-gl";
import { debounce } from "@/utils/debounce";
import { api, createBoundingBox } from "@/services/api";
import type { AvailableDate } from "@/types";

/**
 * Hook to automatically fetch Esri Wayback dates based on map viewport
 * Uses the local changes API to only return dates where imagery actually changed
 * Debounces requests to avoid excessive API calls during map movement
 *
 * OPTIMIZATIONS:
 * - Increased debounce to 800ms (was 300ms) to reduce API calls during panning
 * - Request deduplication based on viewport
 * - Abort controller to cancel stale requests
 * - Loading state tracking to prevent concurrent requests
 */
export function useEsriDates(
  map: maplibregl.Map | null,
  enabled: boolean
): { dates: AvailableDate[]; isLoading: boolean } {
  const [dates, setDates] = useState<AvailableDate[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const isLoadingRef = useRef(false);
  const lastFetchKeyRef = useRef<string>("");
  const abortControllerRef = useRef<AbortController | null>(null);

  // Check if dates are equal to avoid unnecessary updates
  const areDatesEqual = (d1: AvailableDate[], d2: AvailableDate[]) => {
    if (d1.length !== d2.length) return false;
    for (let i = 0; i < d1.length; i++) {
      if (d1[i].date !== d2[i].date) {
        return false;
      }
    }
    return true;
  };

  // Fetch function - stable reference using ref for loading state
  const fetchDates = useCallback(
    async (mapInstance: maplibregl.Map) => {
      if (isLoadingRef.current) {
        console.log("[useEsriDates] Already loading, skipping");
        return;
      }

      const bounds = mapInstance.getBounds();
      const zoom = Math.round(mapInstance.getZoom());

      // Create a key to avoid duplicate fetches for same viewport
      // Use more precision to detect meaningful viewport changes
      const fetchKey = `${zoom}-${bounds.getSouth().toFixed(3)}-${bounds.getWest().toFixed(3)}-${bounds.getNorth().toFixed(3)}-${bounds.getEast().toFixed(3)}`;
      if (fetchKey === lastFetchKeyRef.current) {
        console.log("[useEsriDates] Same viewport, skipping fetch");
        return;
      }

      // Cancel any in-flight request
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }

      try {
        isLoadingRef.current = true;
        setIsLoading(true);
        abortControllerRef.current = new AbortController();

        console.log("[useEsriDates] Fetching local changes for zoom:", zoom);

        const bbox = createBoundingBox(
          bounds.getSouth(),
          bounds.getWest(),
          bounds.getNorth(),
          bounds.getEast()
        );

        const fetchedDates = await api.getAvailableDatesForArea(bbox, zoom);

        // Check if request was aborted
        if (abortControllerRef.current?.signal.aborted) {
          console.log("[useEsriDates] Request aborted");
          return;
        }

        lastFetchKeyRef.current = fetchKey;

        setDates((prevDates) => {
          const newDates = fetchedDates || [];
          if (areDatesEqual(prevDates, newDates)) {
            console.log("[useEsriDates] Dates unchanged, skipping update");
            return prevDates;
          }
          console.log("[useEsriDates] Found", newDates.length, "dates with local changes");
          return newDates;
        });
      } catch (error) {
        // Ignore abort errors
        if (error instanceof Error && error.name === 'AbortError') {
          return;
        }
        console.error("[useEsriDates] Error fetching dates:", error);
      } finally {
        isLoadingRef.current = false;
        setIsLoading(false);
      }
    },
    [] // No dependencies - uses refs for mutable state
  );

  // Effect to set up listeners and trigger initial fetch
  useEffect(() => {
    if (!map || !enabled) {
      console.log("[useEsriDates] Hook disabled or no map, enabled:", enabled);
      // Reset state when disabled
      if (!enabled) {
        setDates([]);
        setIsLoading(false);
        lastFetchKeyRef.current = "";
        if (abortControllerRef.current) {
          abortControllerRef.current.abort();
        }
      }
      return;
    }

    console.log("[useEsriDates] Hook enabled, setting up listeners");

    // Create debounced version (800ms delay - better performance)
    // Increased from 300ms to reduce API calls during continuous panning
    const debouncedFetch = debounce(() => {
      fetchDates(map);
    }, 800);

    // Initial fetch - do it immediately when enabled
    const doInitialFetch = () => {
      console.log("[useEsriDates] Doing initial fetch");
      // Reset the fetch key to force a new fetch
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

    // Cleanup
    return () => {
      map.off("moveend", debouncedFetch);
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, [map, enabled, fetchDates]);

  return { dates, isLoading };
}
