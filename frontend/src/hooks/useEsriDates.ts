import { useEffect, useState, useRef, useCallback } from "react";
import maplibregl from "maplibre-gl";
import { debounce } from "@/utils/debounce";
import { api, createBoundingBox } from "@/services/api";
import type { AvailableDate } from "@/types";

/**
 * Hook to automatically fetch Esri Wayback dates based on map viewport
 * Uses the local changes API to only return dates where imagery actually changed
 * Debounces requests to avoid excessive API calls during map movement
 */
export function useEsriDates(
  map: maplibregl.Map | null,
  enabled: boolean
): { dates: AvailableDate[]; isLoading: boolean } {
  const [dates, setDates] = useState<AvailableDate[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const isLoadingRef = useRef(false);
  const lastFetchKeyRef = useRef<string>("");

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
      const fetchKey = `${zoom}-${bounds.getSouth().toFixed(4)}-${bounds.getWest().toFixed(4)}`;
      if (fetchKey === lastFetchKeyRef.current) {
        console.log("[useEsriDates] Same viewport, skipping fetch");
        return;
      }

      try {
        isLoadingRef.current = true;
        setIsLoading(true);
        console.log("[useEsriDates] Fetching local changes for zoom:", zoom);

        const bbox = createBoundingBox(
          bounds.getSouth(),
          bounds.getWest(),
          bounds.getNorth(),
          bounds.getEast()
        );

        const fetchedDates = await api.getAvailableDatesForArea(bbox, zoom);
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
      }
      return;
    }

    console.log("[useEsriDates] Hook enabled, setting up listeners");

    // Create debounced version (300ms delay - faster response)
    const debouncedFetch = debounce(() => {
      fetchDates(map);
    }, 300);

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
    };
  }, [map, enabled, fetchDates]);

  return { dates, isLoading };
}
