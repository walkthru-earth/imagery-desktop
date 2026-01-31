import * as React from "react";
import { useState, useEffect } from "react";
import type { CropPreview } from "@/types";

interface MapCropOverlayProps {
  visible: boolean;
  crop: CropPreview;
  onChange?: (crop: CropPreview) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
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

  // Use z-index lower than sidebars (which use z-40)
  const overlayZ = 30;

  return (
    <div
      className="absolute inset-0 overflow-hidden pointer-events-none"
      style={{ zIndex: overlayZ }}
    >
      {/* Dark overlay - 4 rectangles around the crop area */}
      {/* Top */}
      <div
        className="absolute bg-black/60"
        style={{
          top: 0,
          left: 0,
          right: 0,
          height: cropTop,
        }}
      />
      {/* Bottom */}
      <div
        className="absolute bg-black/60"
        style={{
          top: cropTop + cropHeight,
          left: 0,
          right: 0,
          bottom: 0,
        }}
      />
      {/* Left */}
      <div
        className="absolute bg-black/60"
        style={{
          top: cropTop,
          left: 0,
          width: cropLeft,
          height: cropHeight,
        }}
      />
      {/* Right */}
      <div
        className="absolute bg-black/60"
        style={{
          top: cropTop,
          left: cropLeft + cropWidth,
          right: 0,
          height: cropHeight,
        }}
      />

      {/* Crop frame border */}
      <div
        className="absolute"
        style={{
          top: cropTop,
          left: cropLeft,
          width: cropWidth,
          height: cropHeight,
          border: "2px solid rgba(59, 130, 246, 0.9)",
          boxShadow: "0 0 0 1px rgba(0, 0, 0, 0.5), inset 0 0 0 1px rgba(255, 255, 255, 0.3)",
        }}
      >
        {/* Rule of thirds grid */}
        <div className="absolute inset-0">
          <div className="absolute top-0 bottom-0 left-1/3 w-px bg-white/30" />
          <div className="absolute top-0 bottom-0 left-2/3 w-px bg-white/30" />
          <div className="absolute left-0 right-0 top-1/3 h-px bg-white/30" />
          <div className="absolute left-0 right-0 top-2/3 h-px bg-white/30" />
        </div>

        {/* Corner brackets */}
        <div className="absolute -top-0.5 -left-0.5 w-5 h-5 border-t-2 border-l-2 border-blue-400" />
        <div className="absolute -top-0.5 -right-0.5 w-5 h-5 border-t-2 border-r-2 border-blue-400" />
        <div className="absolute -bottom-0.5 -left-0.5 w-5 h-5 border-b-2 border-l-2 border-blue-400" />
        <div className="absolute -bottom-0.5 -right-0.5 w-5 h-5 border-b-2 border-r-2 border-blue-400" />

        {/* Center crosshair */}
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-8 h-8">
          <div className="absolute top-1/2 left-0 right-0 h-px bg-white/60" />
          <div className="absolute left-1/2 top-0 bottom-0 w-px bg-white/60" />
        </div>

        {/* Dimensions label */}
        <div className="absolute -bottom-7 left-1/2 -translate-x-1/2 bg-black/80 text-white text-xs px-2 py-1 rounded whitespace-nowrap">
          {Math.round(cropWidth)}×{Math.round(cropHeight)}px
        </div>
      </div>

      {/* Instructions - positioned at top of crop area */}
      <div
        className="absolute bg-black/80 text-white text-xs px-3 py-1.5 rounded whitespace-nowrap"
        style={{
          top: Math.max(8, cropTop - 32),
          left: cropLeft + cropWidth / 2,
          transform: "translateX(-50%)",
        }}
      >
        Pan & zoom the map to frame your video
      </div>
    </div>
  );
}
