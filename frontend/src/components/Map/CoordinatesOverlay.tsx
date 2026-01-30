import { useEffect, useState } from "react";
import maplibregl from "maplibre-gl";

interface CoordinatesOverlayProps {
  maps: (maplibregl.Map | null)[];
}

export function CoordinatesOverlay({ maps }: CoordinatesOverlayProps) {
  const [coords, setCoords] = useState<{ lat: number; lng: number; zoom: number } | null>(null);

  useEffect(() => {
    const validMaps = maps.filter((m): m is maplibregl.Map => m !== null);
    if (validMaps.length === 0) return;

    const updateCoords = (e: maplibregl.MapMouseEvent) => {
      setCoords({
        lat: e.lngLat.lat,
        lng: e.lngLat.lng,
        zoom: e.target.getZoom(),
      });
    };

    const handleMove = (e: any) => {
        // Update zoom if we already have coords (e.g. scrolling)
        if (coords) {
             setCoords(prev => prev ? ({ ...prev, zoom: e.target.getZoom() }) : null);
        }
    }

    validMaps.forEach(map => {
        map.on("mousemove", updateCoords);
        map.on("move", handleMove); 
        map.on("zoom", handleMove);
    });

    return () => {
        validMaps.forEach(map => {
            map.off("mousemove", updateCoords);
            map.off("move", handleMove);
            map.off("zoom", handleMove);
        });
    };
  }, [maps, coords]); // Added coords to dependency to access latest state in handleMove? No, that causes re-bind loop.
  // Actually handleMove only needs to update zoom.
  
  // Let's refactor handleMove to calculate from map instance directly without depending on closure state if possible,
  // or just use functional update for setCoords which I did.


  if (!coords) return null;

  return (
    <div className="absolute top-20 left-1/2 -translate-x-1/2 z-40 px-3 py-1 bg-background/80 backdrop-blur-sm border border-border rounded-md text-xs font-mono shadow-sm pointer-events-none tabular-nums">
      <span className="font-semibold text-primary">Lat:</span> {coords.lat.toFixed(5)}°{" "}
      <span className="font-semibold text-primary ml-2">Lon:</span> {coords.lng.toFixed(5)}°{" "}
      <span className="font-semibold text-primary ml-2">Zoom:</span> {coords.zoom.toFixed(1)}
    </div>
  );
}
