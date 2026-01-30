import { useEffect, useState, RefObject } from "react";
import maplibregl from "maplibre-gl";

// Map styles for light and dark mode (CartoDB vector tiles)
const MAP_STYLES = {
  light: "https://basemaps.cartocdn.com/gl/positron-gl-style/style.json",
  dark: "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json",
};

/**
 * Hook to initialize and manage a MapLibre GL map instance
 * Handles map creation, theme updates, and cleanup
 */
export function useMapInstance(
  containerRef: RefObject<HTMLDivElement | null>,
  theme: "light" | "dark",
  onStyleLoad?: () => void
): maplibregl.Map | null {
  const [map, setMap] = useState<maplibregl.Map | null>(null);

  // Initialize map
  useEffect(() => {
    if (!containerRef.current) {
      console.log("[useMapInstance] No container ref yet");
      return;
    }
    if (map) {
      console.log("[useMapInstance] Map already exists");
      return;
    }

    console.log("[useMapInstance] Initializing map with theme:", theme);
    const mapInstance = new maplibregl.Map({
      container: containerRef.current,
      style: MAP_STYLES[theme],
      center: [31.2357, 30.0444], // Cairo, Egypt
      zoom: 10,
    });

    // Add event listeners for debugging
    mapInstance.on("load", () => console.log("[useMapInstance] Map load event"));
    mapInstance.on("styledata", () => console.log("[useMapInstance] Map styledata event"));
    mapInstance.on("movestart", () => console.log("[useMapInstance] Map movestart"));
    mapInstance.on("move", () => console.log("[useMapInstance] Map move"));
    mapInstance.on("moveend", () => {
      const center = mapInstance.getCenter();
      const zoom = mapInstance.getZoom();
      console.log("[useMapInstance] Map moveend - center:", center, "zoom:", zoom);
    });
    mapInstance.on("zoomstart", () => console.log("[useMapInstance] Map zoomstart"));
    mapInstance.on("zoom", () => console.log("[useMapInstance] Map zoom"));
    mapInstance.on("zoomend", () => console.log("[useMapInstance] Map zoomend"));

    // Wait for style to load before making map available
    mapInstance.once("styledata", () => {
      console.log("[useMapInstance] Initial style loaded, calling onStyleLoad");
      onStyleLoad?.();
      setMap(mapInstance);
      console.log("[useMapInstance] Map instance set in state");
    });

    // Add navigation controls
    mapInstance.addControl(
      new maplibregl.NavigationControl(),
      "bottom-right"
    );

    // Cleanup on unmount
    return () => {
      console.log("[useMapInstance] Cleaning up map instance");
      mapInstance.remove();
      setMap(null);
    };
  }, []); // Only run once on mount

  // Update theme when it changes
  useEffect(() => {
    if (!map) {
      console.log("[useMapInstance] Theme change but no map yet");
      return;
    }

    console.log("[useMapInstance] Updating map style to theme:", theme);
    map.setStyle(MAP_STYLES[theme]);

    // Notify when new style is loaded
    if (onStyleLoad) {
      console.log("[useMapInstance] Setting up onStyleLoad callback for theme change");
      map.once("styledata", () => {
        console.log("[useMapInstance] Style loaded after theme change");
        onStyleLoad();
      });
    }
  }, [map, theme, onStyleLoad]);

  return map;
}
