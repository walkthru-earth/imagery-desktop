import * as React from "react";
import { useState, useEffect, useCallback, useRef } from "react";
import type { CropPreview } from "@/types";

interface MapCropOverlayProps {
  /** Whether the overlay is visible */
  visible: boolean;
  /** Current crop settings (0-1 relative coordinates) */
  crop: CropPreview;
  /** Callback when crop changes */
  onChange: (crop: CropPreview) => void;
  /** Container element to render within */
  containerRef: React.RefObject<HTMLDivElement>;
}

type ResizeHandle = "nw" | "n" | "ne" | "e" | "se" | "s" | "sw" | "w";

export function MapCropOverlay({
  visible,
  crop,
  onChange,
  containerRef,
}: MapCropOverlayProps) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [isResizing, setIsResizing] = useState<ResizeHandle | null>(null);
  const [dragStart, setDragStart] = useState({ x: 0, y: 0 });
  const [cropStart, setCropStart] = useState<CropPreview>(crop);

  // Convert relative crop to pixel coordinates
  const getPixelCrop = useCallback(() => {
    if (!containerRef.current) return null;
    const rect = containerRef.current.getBoundingClientRect();
    return {
      x: crop.x * rect.width,
      y: crop.y * rect.height,
      width: crop.width * rect.width,
      height: crop.height * rect.height,
      containerWidth: rect.width,
      containerHeight: rect.height,
    };
  }, [crop, containerRef]);

  // Convert pixel coordinates to relative crop
  const pixelToRelative = useCallback(
    (x: number, y: number, width: number, height: number): CropPreview => {
      if (!containerRef.current) return crop;
      const rect = containerRef.current.getBoundingClientRect();
      return {
        x: Math.max(0, Math.min(1 - width / rect.width, x / rect.width)),
        y: Math.max(0, Math.min(1 - height / rect.height, y / rect.height)),
        width: Math.max(0.1, Math.min(1, width / rect.width)),
        height: Math.max(0.1, Math.min(1, height / rect.height)),
      };
    },
    [containerRef, crop]
  );

  // Handle mouse down on the crop area (for dragging)
  const handleCropMouseDown = (e: React.MouseEvent) => {
    if (isResizing) return;
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(true);
    setDragStart({ x: e.clientX, y: e.clientY });
    setCropStart(crop);
  };

  // Handle mouse down on resize handles
  const handleResizeMouseDown = (e: React.MouseEvent, handle: ResizeHandle) => {
    e.preventDefault();
    e.stopPropagation();
    setIsResizing(handle);
    setDragStart({ x: e.clientX, y: e.clientY });
    setCropStart(crop);
  };

  // Handle mouse move
  useEffect(() => {
    if (!isDragging && !isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      if (!containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();

      const dx = (e.clientX - dragStart.x) / rect.width;
      const dy = (e.clientY - dragStart.y) / rect.height;

      if (isDragging) {
        // Move the crop area
        const newX = Math.max(
          0,
          Math.min(1 - cropStart.width, cropStart.x + dx)
        );
        const newY = Math.max(
          0,
          Math.min(1 - cropStart.height, cropStart.y + dy)
        );
        onChange({ ...crop, x: newX, y: newY });
      } else if (isResizing) {
        // Resize the crop area
        let newCrop = { ...cropStart };

        switch (isResizing) {
          case "nw":
            newCrop.x = Math.max(0, Math.min(cropStart.x + cropStart.width - 0.1, cropStart.x + dx));
            newCrop.y = Math.max(0, Math.min(cropStart.y + cropStart.height - 0.1, cropStart.y + dy));
            newCrop.width = cropStart.width - (newCrop.x - cropStart.x);
            newCrop.height = cropStart.height - (newCrop.y - cropStart.y);
            break;
          case "n":
            newCrop.y = Math.max(0, Math.min(cropStart.y + cropStart.height - 0.1, cropStart.y + dy));
            newCrop.height = cropStart.height - (newCrop.y - cropStart.y);
            break;
          case "ne":
            newCrop.y = Math.max(0, Math.min(cropStart.y + cropStart.height - 0.1, cropStart.y + dy));
            newCrop.width = Math.max(0.1, Math.min(1 - cropStart.x, cropStart.width + dx));
            newCrop.height = cropStart.height - (newCrop.y - cropStart.y);
            break;
          case "e":
            newCrop.width = Math.max(0.1, Math.min(1 - cropStart.x, cropStart.width + dx));
            break;
          case "se":
            newCrop.width = Math.max(0.1, Math.min(1 - cropStart.x, cropStart.width + dx));
            newCrop.height = Math.max(0.1, Math.min(1 - cropStart.y, cropStart.height + dy));
            break;
          case "s":
            newCrop.height = Math.max(0.1, Math.min(1 - cropStart.y, cropStart.height + dy));
            break;
          case "sw":
            newCrop.x = Math.max(0, Math.min(cropStart.x + cropStart.width - 0.1, cropStart.x + dx));
            newCrop.width = cropStart.width - (newCrop.x - cropStart.x);
            newCrop.height = Math.max(0.1, Math.min(1 - cropStart.y, cropStart.height + dy));
            break;
          case "w":
            newCrop.x = Math.max(0, Math.min(cropStart.x + cropStart.width - 0.1, cropStart.x + dx));
            newCrop.width = cropStart.width - (newCrop.x - cropStart.x);
            break;
        }

        onChange(newCrop);
      }
    };

    const handleMouseUp = () => {
      setIsDragging(false);
      setIsResizing(null);
    };

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);

    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [isDragging, isResizing, dragStart, cropStart, crop, onChange, containerRef]);

  if (!visible) return null;

  const pixelCrop = getPixelCrop();
  if (!pixelCrop) return null;

  const handleStyle = {
    position: "absolute" as const,
    width: 12,
    height: 12,
    background: "white",
    border: "2px solid #3b82f6",
    borderRadius: 2,
    cursor: "pointer",
  };

  return (
    <div
      ref={overlayRef}
      className="absolute inset-0 pointer-events-none"
      style={{ zIndex: 100 }}
    >
      {/* Dark overlay for areas outside crop */}
      <svg
        className="absolute inset-0 w-full h-full"
        style={{ pointerEvents: "none" }}
      >
        <defs>
          <mask id="crop-mask">
            <rect width="100%" height="100%" fill="white" />
            <rect
              x={pixelCrop.x}
              y={pixelCrop.y}
              width={pixelCrop.width}
              height={pixelCrop.height}
              fill="black"
            />
          </mask>
        </defs>
        <rect
          width="100%"
          height="100%"
          fill="rgba(0,0,0,0.5)"
          mask="url(#crop-mask)"
        />
      </svg>

      {/* Crop area border */}
      <div
        className="absolute pointer-events-auto cursor-move"
        style={{
          left: pixelCrop.x,
          top: pixelCrop.y,
          width: pixelCrop.width,
          height: pixelCrop.height,
          border: "2px solid #3b82f6",
          boxShadow: "0 0 0 9999px rgba(0,0,0,0)",
        }}
        onMouseDown={handleCropMouseDown}
      >
        {/* Grid lines */}
        <div className="absolute inset-0 grid grid-cols-3 grid-rows-3 pointer-events-none">
          {[...Array(9)].map((_, i) => (
            <div key={i} className="border border-white/30" />
          ))}
        </div>

        {/* Resize handles */}
        {/* Corners */}
        <div
          style={{ ...handleStyle, top: -6, left: -6, cursor: "nwse-resize" }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "nw")}
        />
        <div
          style={{ ...handleStyle, top: -6, right: -6, cursor: "nesw-resize" }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "ne")}
        />
        <div
          style={{ ...handleStyle, bottom: -6, right: -6, cursor: "nwse-resize" }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "se")}
        />
        <div
          style={{ ...handleStyle, bottom: -6, left: -6, cursor: "nesw-resize" }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "sw")}
        />

        {/* Edges */}
        <div
          style={{
            ...handleStyle,
            top: -6,
            left: "50%",
            transform: "translateX(-50%)",
            cursor: "ns-resize",
          }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "n")}
        />
        <div
          style={{
            ...handleStyle,
            right: -6,
            top: "50%",
            transform: "translateY(-50%)",
            cursor: "ew-resize",
          }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "e")}
        />
        <div
          style={{
            ...handleStyle,
            bottom: -6,
            left: "50%",
            transform: "translateX(-50%)",
            cursor: "ns-resize",
          }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "s")}
        />
        <div
          style={{
            ...handleStyle,
            left: -6,
            top: "50%",
            transform: "translateY(-50%)",
            cursor: "ew-resize",
          }}
          className="pointer-events-auto"
          onMouseDown={(e) => handleResizeMouseDown(e, "w")}
        />

        {/* Dimensions label */}
        <div
          className="absolute bottom-2 right-2 bg-black/70 text-white text-xs px-2 py-1 rounded pointer-events-none"
        >
          {Math.round(crop.width * 100)}% × {Math.round(crop.height * 100)}%
        </div>
      </div>
    </div>
  );
}
