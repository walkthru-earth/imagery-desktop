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
  currentDate?: number;  // Current date index in range download (1-based)
  totalDates?: number;   // Total dates in range download
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

// ============================================================================
// Task Queue Types
// ============================================================================

// Task Status
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';

// Task Progress
export interface TaskProgress {
  currentPhase: string; // "downloading", "merging", "encoding"
  totalDates: number;
  currentDate: number;
  tilesTotal: number;
  tilesCompleted: number;
  percent: number;
}

// Crop Preview (relative 0-1 coords for map overlay)
export interface CropPreview {
  x: number;      // Left position (0-1)
  y: number;      // Top position (0-1)
  width: number;  // Width (0-1)
  height: number; // Height (0-1)
}

// Video Export Options
export interface VideoExportOptions {
  width: number;
  height: number;
  preset: string;
  cropX: number;
  cropY: number;
  spotlightEnabled: boolean;
  spotlightCenterLat: number;
  spotlightCenterLon: number;
  spotlightRadiusKm: number;
  overlayOpacity: number;
  showDateOverlay: boolean;
  dateFontSize: number;
  datePosition: string;
  showLogo: boolean;
  logoPosition: string;
  frameDelay: number;
  outputFormat: string;
  quality: number;
}

// Export Task
export interface ExportTask {
  id: string;
  name: string;
  status: TaskStatus;
  priority: number;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  source: string;
  bbox: BoundingBox;
  zoom: number;
  format: string;
  dates: GEDateInfo[];
  videoExport: boolean;
  videoOpts?: VideoExportOptions;
  cropPreview?: CropPreview;
  progress: TaskProgress;
  error?: string;
  outputPath?: string;
}

// Queue Status
export interface QueueStatus {
  isRunning: boolean;
  isPaused: boolean;
  currentTaskID: string;
  totalTasks: number;
  completedTasks: number;
  pendingTasks: number;
}

// Task Progress Event
export interface TaskProgressEvent {
  taskId: string;
  progress: TaskProgress;
}

// Task Complete Event
export interface TaskCompleteEvent {
  taskId: string;
  success: boolean;
  error?: string;
}

// System Notification
export interface SystemNotification {
  title: string;
  message: string;
  type: 'success' | 'error' | 'info';
}
