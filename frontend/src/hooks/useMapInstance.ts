import { useEffect, useState, useRef, RefObject } from "react";
import maplibregl from "maplibre-gl";
import iconUrl from "@/assets/images/icon.svg";
import { BrowserOpenURL } from "../../wailsjs/runtime";

// Map styles for light and dark mode (CartoDB vector tiles)
const MAP_STYLES = {
  light: "https://basemaps.cartocdn.com/gl/positron-gl-style/style.json",
  dark: "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json",
};

// Default center (Zamalek, Cairo, Egypt) - used when no initial position provided
const DEFAULT_CENTER: [number, number] = [31.2219, 30.0621];
const DEFAULT_ZOOM = 15;

/**
 * Hook to initialize and manage a MapLibre GL map instance
 * Handles map creation, theme updates, and cleanup
 */
export function useMapInstance(
  containerRef: RefObject<HTMLDivElement | null>,
  theme: "light" | "dark",
  onStyleLoad?: () => void,
  initialCenter?: [number, number],
  initialZoom?: number
): maplibregl.Map | null {
  const [map, setMap] = useState<maplibregl.Map | null>(null);

  // Use provided position or fallback to defaults
  const center = initialCenter || DEFAULT_CENTER;
  const zoom = initialZoom ?? DEFAULT_ZOOM;

  // Initialize map
  useEffect(() => {
    if (!containerRef.current) {
      // console.log("[useMapInstance] No container ref yet");
      return;
    }
    if (map) {
      // console.log("[useMapInstance] Map already exists");
      return;
    }

    // console.log("[useMapInstance] Initializing map with theme:", theme);
    const mapInstance = new maplibregl.Map({
      container: containerRef.current,
      style: MAP_STYLES[theme],
      center,
      zoom,
      // @ts-ignore - preserveDrawingBuffer is valid WebGL context option
      preserveDrawingBuffer: true, // Required for canvas recording/screenshots
      attributionControl: false, // We will add a custom one below
    });

    // Add custom attribution with logo and static credits
    // "Always add google and esri and walkthru.earth"
    // Use span with inline-flex to ensure it stays on the same line as other attributions
    const attributionHtml = `
      <span style="display: inline-flex; align-items: center; gap: 4px;">
        <a href="https://walkthru.earth" target="_blank" style="display: flex; align-items: center; gap: 4px; text-decoration: none; color: inherit;">
          <img src="${iconUrl}" alt="" style="height: 14px; width: 14px;" />
          <span class="font-semibold">Walkthru.earth</span>
        </a>
      </span>
    `;

    mapInstance.addControl(
      new maplibregl.AttributionControl({
        compact: false, // Expand by default to show branding
        customAttribution: attributionHtml,
      }),
      "bottom-right"
    );

    // Wait for style to load before making map available
    mapInstance.once("styledata", () => {
      onStyleLoad?.();
      setMap(mapInstance);
    });

    // Add navigation controls
    mapInstance.addControl(
      new maplibregl.NavigationControl(),
      "bottom-right"
    );

    // Intercept attribution link clicks to open in system browser
    const handleLinkClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      // Use closest to find the anchor if user clicks image or span inside
      const link = target.closest("a");
      // Check if it's the walkthru link (or any external link we want to handle)
      if (link && link.href && link.href.includes("walkthru.earth")) {
        e.preventDefault();
        BrowserOpenURL(link.href);
      }
    };
    
    // Attach listener to container (MapLibre puts controls inside container)
    // Note: use capture phase to ensure we catch it before MapLibre potentially handles it
    const container = containerRef.current;
    container.addEventListener("click", handleLinkClick);

    // Cleanup on unmount
    return () => {
      // console.log("[useMapInstance] Cleaning up map instance");
      if (container) {
        container.removeEventListener("click", handleLinkClick);
      }
      mapInstance.remove();
      setMap(null);
    };
  }, []); // Only run once on mount

  // Track current theme to avoid redundant setStyle calls
  const currentThemeRef = useRef<"light" | "dark">(theme);

  // Update theme when it changes
  useEffect(() => {
    if (!map) {
      // console.log("[useMapInstance] Theme change but no map yet");
      return;
    }

    // Skip if theme hasn't changed (avoids wiping layers on init)
    if (currentThemeRef.current === theme) {
      // console.log("[useMapInstance] Theme matched current, skipping setStyle");
      return;
    }

    // console.log("[useMapInstance] Updating map style to theme:", theme);
    currentThemeRef.current = theme;
    map.setStyle(MAP_STYLES[theme]);

    // Notify when new style is loaded
    if (onStyleLoad) {
      // console.log("[useMapInstance] Setting up onStyleLoad callback for theme change");
      map.once("styledata", () => {
        // console.log("[useMapInstance] Style loaded after theme change");
        onStyleLoad();
      });
    }
  }, [map, theme, onStyleLoad]);

  return map;
}
