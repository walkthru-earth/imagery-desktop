import { useEffect, useState, useCallback } from "react";
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
): AvailableDate[] {
  const [dates, setDates] = useState<AvailableDate[]>([]);
  const [isLoading, setIsLoading] = useState(false);

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

  // Memoize the fetch function to avoid recreating on every render
  const fetchDates = useCallback(
    async (mapInstance: maplibregl.Map) => {
      if (isLoading) return; // Prevent concurrent requests

      try {
        setIsLoading(true);
        const bounds = mapInstance.getBounds();
        const zoom = Math.round(mapInstance.getZoom());

        console.log("[useEsriDates] Fetching local changes for zoom:", zoom);

        const bbox = createBoundingBox(
          bounds.getSouth(),
          bounds.getWest(),
          bounds.getNorth(),
          bounds.getEast()
        );

        // Use getAvailableDatesForArea which filters by local changes
        const fetchedDates = await api.getAvailableDatesForArea(bbox, zoom);

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
        // Keep existing dates on error
      } finally {
        setIsLoading(false);
      }
    },
    [isLoading]
  );

  useEffect(() => {
    if (!map || !enabled) {
      console.log("[useEsriDates] Hook disabled or no map");
      return;
    }

    console.log("[useEsriDates] Hook enabled, setting up listeners");

    // Create debounced version (800ms delay for Esri since it's slower)
    const debouncedFetch = debounce(() => {
      fetchDates(map);
    }, 800);

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

    // Cleanup
    return () => {
      map.off("moveend", debouncedFetch);
    };
  }, [map, enabled, fetchDates]);

  return dates;
}
