import { useEffect, useRef, useState } from "react";
import maplibregl from "maplibre-gl";
import Compare from "@maplibre/maplibre-gl-compare";
import "@maplibre/maplibre-gl-compare/dist/maplibre-gl-compare.css";

export interface MapCompareProps {
  /**
   * The left/before map instance
   */
  leftMap: maplibregl.Map | null;

  /**
   * The right/after map instance
   */
  rightMap: maplibregl.Map | null;

  /**
   * Orientation of the comparison slider
   * @default "vertical"
   */
  orientation?: "vertical" | "horizontal";

  /**
   * Container class name for styling
   */
  className?: string;

  /**
   * Callback when compare instance is created
   */
  onCompareReady?: (compare: Compare) => void;
}

/**
 * MapCompare - A split-screen map comparison component using MapLibre GL Compare
 *
 * Features:
 * - Side-by-side map comparison with draggable slider
 * - Synchronized pan, zoom, and rotate
 * - Supports vertical (left/right) and horizontal (top/bottom) orientations
 * - Automatic cleanup on unmount
 *
 * Use cases:
 * 1. Esri vs Google Earth comparison
 * 2. Date-to-date comparison (same source, different dates)
 * 3. Before/after analysis
 *
 * @example
 * ```tsx
 * const leftMap = useRef<maplibregl.Map | null>(null);
 * const rightMap = useRef<maplibregl.Map | null>(null);
 *
 * return (
 *   <MapCompare
 *     leftMap={leftMap.current}
 *     rightMap={rightMap.current}
 *     orientation="vertical"
 *   />
 * );
 * ```
 */
export function MapCompare({
  leftMap,
  rightMap,
  orientation = "vertical",
  onCompareReady,
}: MapCompareProps) {
  const compareInstanceRef = useRef<Compare | null>(null);

  useEffect(() => {
    // Wait for both map instances to be available
    if (!leftMap || !rightMap) {
      console.log("MapCompare: Waiting for both map instances...");
      return;
    }

    console.log("MapCompare: Both maps available, checking if loaded...");

    const initializeCompare = () => {
      if (compareInstanceRef.current) {
        // Clean up existing instance before creating new one
        console.log("MapCompare: Cleaning up existing compare instance");
        compareInstanceRef.current.remove();
        compareInstanceRef.current = null;
      }

      try {
        // Create the compare instance
        const container = document.getElementById("compare-container");
        if (!container) {
          console.error("MapCompare: Compare container not found");
          return;
        }

        console.log("MapCompare: Initializing with container:", container);
        console.log("MapCompare: Left map loaded:", leftMap.loaded(), "Right map loaded:", rightMap.loaded());

        const compare = new Compare(leftMap, rightMap, container, {
          orientation: orientation,
          // Disable mousemove to prevent slider from following mouse
          mousemove: false,
        });

        compareInstanceRef.current = compare;

        // Notify parent component
        if (onCompareReady) {
          onCompareReady(compare);
        }

        console.log("MapCompare: Compare initialized successfully, slider should be visible");

        // Synchronization is automatic with maplibre-gl-compare:
        // - Pan movements are synced
        // - Zoom levels are synced
        // - Bearing (rotation) is synced
        // - Pitch is synced
      } catch (error) {
        console.error("MapCompare: Failed to initialize:", error);
      }
    };

    // Wait for both maps to be fully loaded AND styled
    const leftLoaded = leftMap.loaded();
    const rightLoaded = rightMap.loaded();

    if (leftLoaded && rightLoaded) {
      // Both maps are already loaded, but wait for styles to be applied
      console.log("MapCompare: Both maps already loaded, waiting for styles...");

      // Small delay to ensure styles are fully applied
      const timer = setTimeout(() => {
        console.log("MapCompare: Styles should be ready, initializing compare");
        initializeCompare();
      }, 100);

      return () => {
        clearTimeout(timer);
        if (compareInstanceRef.current) {
          compareInstanceRef.current.remove();
          compareInstanceRef.current = null;
        }
      };
    } else {
      // Wait for both maps to load
      console.log("MapCompare: Waiting for maps to load... Left:", leftLoaded, "Right:", rightLoaded);

      let leftReady = leftLoaded;
      let rightReady = rightLoaded;

      const checkAndInitialize = () => {
        if (leftReady && rightReady) {
          console.log("MapCompare: Both maps loaded, initializing compare");
          setTimeout(() => initializeCompare(), 100);
        }
      };

      const handleLeftLoad = () => {
        console.log("MapCompare: Left map loaded");
        leftReady = true;
        checkAndInitialize();
      };

      const handleRightLoad = () => {
        console.log("MapCompare: Right map loaded");
        rightReady = true;
        checkAndInitialize();
      };

      if (!leftLoaded) {
        leftMap.once("load", handleLeftLoad);
      }
      if (!rightLoaded) {
        rightMap.once("load", handleRightLoad);
      }

      return () => {
        if (compareInstanceRef.current) {
          compareInstanceRef.current.remove();
          compareInstanceRef.current = null;
        }
      };
    }
  }, [leftMap, rightMap, orientation, onCompareReady]);

  // This component doesn't render anything - it just manages the Compare instance
  // The container is managed by the parent (App.tsx)
  return null;
}

export default MapCompare;
