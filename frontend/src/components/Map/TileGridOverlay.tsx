import { useEffect } from "react";
import maplibregl from "maplibre-gl";

interface TileGridOverlayProps {
  map: maplibregl.Map | null;
  visible: boolean;
}

export function TileGridOverlay({ map, visible }: TileGridOverlayProps) {
  useEffect(() => {
    if (!map) return;

    // Helper: Convert tile coords to bounds
    const tile2long = (x: number, z: number) => (x / Math.pow(2, z)) * 360 - 180;
    const tile2lat = (y: number, z: number) => {
      const n = Math.PI - (2 * Math.PI * y) / Math.pow(2, z);
      return (180 / Math.PI) * Math.atan(0.5 * (Math.exp(n) - Math.exp(-n)));
    };

    const updateGrid = () => {
      const source = map.getSource("custom-tile-grid") as maplibregl.GeoJSONSource;
      
      if (!visible) {
        if (source) source.setData({ type: "FeatureCollection", features: [] });
        return;
      }

      const zoom = Math.floor(map.getZoom());
      const bounds = map.getBounds();
      const ne = bounds.getNorthEast();
      const sw = bounds.getSouthWest();

      // Convert bounds to tile coords
      const n = Math.pow(2, zoom);
      const xMin = Math.max(0, Math.floor(((sw.lng + 180) / 360) * n));
      const xMax = Math.min(n - 1, Math.floor(((ne.lng + 180) / 360) * n));
      const yMin = Math.max(0, Math.floor((1 - Math.log(Math.tan(ne.lat * Math.PI / 180) + 1 / Math.cos(ne.lat * Math.PI / 180)) / Math.PI) / 2 * n));
      const yMax = Math.min(n - 1, Math.floor((1 - Math.log(Math.tan(sw.lat * Math.PI / 180) + 1 / Math.cos(sw.lat * Math.PI / 180)) / Math.PI) / 2 * n));

      const features: GeoJSON.Feature[] = [];

      // Limit grid generation to reasonably small number of tiles to avoid hanging
      const MAX_TILES = 500;
      if ((xMax - xMin + 1) * (yMax - yMin + 1) > MAX_TILES) {
          // Too many tiles, don't render or fallback?
          // Just return, or render nothing.
          if (source) source.setData({ type: "FeatureCollection", features: [] });
          return;
      }

      for (let x = xMin; x <= xMax; x++) {
        for (let y = yMin; y <= yMax; y++) {
          const w = tile2long(x, zoom);
          const e = tile2long(x + 1, zoom);
          const nLat = tile2lat(y, zoom);
          const sLat = tile2lat(y + 1, zoom);

          // Grid Polygon
          const poly: GeoJSON.Feature = {
            type: "Feature",
            properties: {
              label: `${zoom}/${x}/${y}`
            },
            geometry: {
              type: "Polygon",
              coordinates: [[
                [w, nLat],
                [e, nLat],
                [e, sLat],
                [w, sLat],
                [w, nLat]
              ]]
            }
          };
          features.push(poly);
          
          // Center Point for Label
          const centerLon = (w + e) / 2;
          const centerLat = (nLat + sLat) / 2;
          const point: GeoJSON.Feature = {
              type: "Feature",
              properties: {
                  label: `${zoom}/${x}/${y}`
              },
              geometry: {
                  type: "Point",
                  coordinates: [centerLon, centerLat]
              }
          };
          features.push(point);
        }
      }
      
      if (source) {
          source.setData({ type: "FeatureCollection", features });
      }
    };

    const addLayers = () => {
        if (!map.getSource("custom-tile-grid")) {
            map.addSource("custom-tile-grid", {
                type: "geojson",
                data: { type: "FeatureCollection", features: [] }
            });

            // Grid Lines
            map.addLayer({
                id: "custom-tile-grid-lines",
                type: "line",
                source: "custom-tile-grid",
                filter: ["==", "$type", "Polygon"],
                layout: {},
                paint: {
                    "line-color": "#e11d48", // Red-600
                    "line-width": 1,
                    "line-opacity": 0.6
                }
            });

            // Grid Labels
            map.addLayer({
                id: "custom-tile-grid-labels",
                type: "symbol",
                source: "custom-tile-grid",
                filter: ["==", "$type", "Point"],
                layout: {
                    "text-field": ["get", "label"],
                    "text-size": 12,
                    "text-anchor": "center",
                    "text-justify": "center",
                    "text-allow-overlap": false
                },
                paint: {
                    "text-color": "#e11d48",
                    "text-halo-color": "rgba(255, 255, 255, 0.8)",
                    "text-halo-width": 2
                }
            });
        }
        updateGrid();
    };

    // Initialize
    if (map.loaded()) {
        addLayers();
    } else {
        map.once('load', addLayers);
    }
    
    // Re-add on style change
    const onStyleData = () => {
        if (map.isStyleLoaded()) {
             addLayers();
        }
    };
    map.on('styledata', onStyleData);

    map.on("moveend", updateGrid);

    // Initial update if already loaded and visible
    if (map.loaded()) updateGrid();

    return () => {
      map.off("moveend", updateGrid);
      map.off("styledata", onStyleData);
      
      // Cleanup
      if (map.getLayer("custom-tile-grid-labels")) map.removeLayer("custom-tile-grid-labels");
      if (map.getLayer("custom-tile-grid-lines")) map.removeLayer("custom-tile-grid-lines");
      if (map.getSource("custom-tile-grid")) map.removeSource("custom-tile-grid");
    };
  }, [map, visible]);

  return null;
}
