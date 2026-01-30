// Bounding Box
export interface BoundingBox {
  south: number;
  west: number;
  north: number;
  east: number;
}

// Tile Information
export interface TileInfo {
  tileCount: number;
  zoomLevel: number;
  resolution: number;
  estSizeMB: number;
}

// Download Progress
export interface DownloadProgress {
  downloaded: number;
  total: number;
  percent: number;
  status: string;
}

// Esri Date
export interface AvailableDate {
  date: string;
  source: string;
}

// Google Earth Date
export interface GEAvailableDate {
  date: string;
  epoch: number;
  hexDate: string;
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
