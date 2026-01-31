import * as React from "react";
import { useState, useEffect, useRef, useCallback } from "react";
import type { CropPreview } from "@/types";

interface MapCropOverlayProps {
  visible: boolean;
  crop: CropPreview;
  onChange?: (crop: CropPreview) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
  /** Lock aspect ratio based on video preset */
  aspectRatio?: number;
}

export function MapCropOverlay({
  visible,
  crop,
  onChange,
  containerRef,
  aspectRatio,
}: MapCropOverlayProps) {
  const [containerSize, setContainerSize] = useState({ width: 0, height: 0 });

  // Update container size on resize
  useEffect(() => {
    if (!containerRef.current) return;

    const updateSize = () => {
      if (containerRef.current) {
        const rect = containerRef.current.getBoundingClientRect();
        setContainerSize({ width: rect.width, height: rect.height });
      }
    };

    updateSize();

    const resizeObserver = new ResizeObserver(updateSize);
    resizeObserver.observe(containerRef.current);

    return () => resizeObserver.disconnect();
  }, [containerRef]);

  if (!visible || containerSize.width === 0) return null;

  // Calculate pixel positions
  const cropLeft = crop.x * containerSize.width;
  const cropTop = crop.y * containerSize.height;
  const cropWidth = crop.width * containerSize.width;
  const cropHeight = crop.height * containerSize.height;

  return (
    <>
      {/* Dark overlay - 4 rectangles around the crop area */}
      {/* Top */}
      <div
        className="absolute bg-black/60 pointer-events-none"
        style={{
          top: 0,
          left: 0,
          right: 0,
          height: cropTop,
          zIndex: 50,
        }}
      />
      {/* Bottom */}
      <div
        className="absolute bg-black/60 pointer-events-none"
        style={{
          top: cropTop + cropHeight,
          left: 0,
          right: 0,
          bottom: 0,
          zIndex: 50,
        }}
      />
      {/* Left */}
      <div
        className="absolute bg-black/60 pointer-events-none"
        style={{
          top: cropTop,
          left: 0,
          width: cropLeft,
          height: cropHeight,
          zIndex: 50,
        }}
      />
      {/* Right */}
      <div
        className="absolute bg-black/60 pointer-events-none"
        style={{
          top: cropTop,
          left: cropLeft + cropWidth,
          right: 0,
          height: cropHeight,
          zIndex: 50,
        }}
      />

      {/* Crop frame border */}
      <div
        className="absolute pointer-events-none"
        style={{
          top: cropTop,
          left: cropLeft,
          width: cropWidth,
          height: cropHeight,
          border: "2px solid rgba(59, 130, 246, 0.8)",
          boxShadow: "0 0 0 1px rgba(0, 0, 0, 0.3), inset 0 0 0 1px rgba(255, 255, 255, 0.2)",
          zIndex: 51,
        }}
      >
        {/* Rule of thirds grid */}
        <div className="absolute inset-0 pointer-events-none">
          {/* Vertical lines */}
          <div className="absolute top-0 bottom-0 left-1/3 w-px bg-white/20" />
          <div className="absolute top-0 bottom-0 left-2/3 w-px bg-white/20" />
          {/* Horizontal lines */}
          <div className="absolute left-0 right-0 top-1/3 h-px bg-white/20" />
          <div className="absolute left-0 right-0 top-2/3 h-px bg-white/20" />
        </div>

        {/* Corner markers */}
        {[
          { top: -2, left: -2 },
          { top: -2, right: -2 },
          { bottom: -2, left: -2 },
          { bottom: -2, right: -2 },
        ].map((pos, i) => (
          <div
            key={i}
            className="absolute w-4 h-4 border-2 border-blue-400"
            style={{
              ...pos,
              borderTop: pos.top !== undefined ? "2px solid rgb(96, 165, 250)" : "none",
              borderBottom: pos.bottom !== undefined ? "2px solid rgb(96, 165, 250)" : "none",
              borderLeft: pos.left !== undefined ? "2px solid rgb(96, 165, 250)" : "none",
              borderRight: pos.right !== undefined ? "2px solid rgb(96, 165, 250)" : "none",
            }}
          />
        ))}

        {/* Dimensions label */}
        <div className="absolute -bottom-8 left-1/2 -translate-x-1/2 bg-black/80 text-white text-xs px-2 py-1 rounded whitespace-nowrap">
          {Math.round(cropWidth)}×{Math.round(cropHeight)}px ({Math.round(crop.width * 100)}%)
        </div>
      </div>

      {/* Center crosshair */}
      <div
        className="absolute w-6 h-6 pointer-events-none"
        style={{
          top: cropTop + cropHeight / 2 - 12,
          left: cropLeft + cropWidth / 2 - 12,
          zIndex: 52,
        }}
      >
        <div className="absolute top-1/2 left-0 right-0 h-px bg-white/50" />
        <div className="absolute top-0 bottom-0 left-1/2 w-px bg-white/50" />
      </div>

      {/* Instructions */}
      <div
        className="absolute top-4 left-1/2 -translate-x-1/2 bg-black/70 text-white text-sm px-4 py-2 rounded-lg pointer-events-none"
        style={{ zIndex: 53 }}
      >
        Pan and zoom the map to frame your export area
      </div>
    </>
  );
}
