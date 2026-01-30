// Re-export Wails-generated types
export { main } from "../../wailsjs/go/models";

// Use Wails BoundingBox instead of redefining
export type BoundingBox = import("../../wailsjs/go/models").main.BoundingBox;
export type TileInfo = import("../../wailsjs/go/models").main.TileInfo;
export type AvailableDate = import("../../wailsjs/go/models").main.AvailableDate;
export type GEAvailableDate = import("../../wailsjs/go/models").main.GEAvailableDate;
export type GEDateInfo = import("../../wailsjs/go/models").main.GEDateInfo;

// Download Progress (custom frontend type)
export interface DownloadProgress {
  downloaded: number;
  total: number;
  percent: number;
  status: string;
}

// Imagery Source
export type ImagerySource = 'esri' | 'google';

// Selection Mode
export type SelectionMode = 'draw' | 'viewport';

// Download Format
export type DownloadFormat = 'geotiff' | 'png';

// Map View Mode
export type MapViewMode = 'single' | 'split';

// Layer Visibility
export interface LayerVisibility {
  osm: boolean;
  imagery: boolean;
  bbox: boolean;
}

// Layer Opacity
export interface LayerOpacity {
  osm: number;
  imagery: number;
}

// Timeline Playback State
export interface TimelineState {
  isPlaying: boolean;
  speed: number; // 0.5x, 1x, 2x, 5x
  loop: boolean;
  currentIndex: number;
}
