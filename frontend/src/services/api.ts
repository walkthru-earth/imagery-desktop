/**
 * API Service Layer
 * Wraps Wails Go backend functions for cleaner imports and better type safety
 */

import {
  GetTileInfo,
  GetEsriLayers,
  GetEsriTileURL,
  GetGoogleEarthTileURL,
  GetGoogleEarthDatesForArea,
  GetGoogleEarthHistoricalTileURL,
  GetAvailableDatesForArea,
  DownloadEsriImagery,
  DownloadEsriImageryRange,
  DownloadGoogleEarthImagery,
  DownloadGoogleEarthHistoricalImagery,
  DownloadGoogleEarthHistoricalImageryRange,
  ExportTimelapseVideo,
  SelectDownloadFolder,
  GetDownloadPath,
  SetDownloadPath,
  OpenDownloadFolder,
  StartTileServer,
  // Task Queue API
  AddExportTask,
  GetTaskQueue,
  GetTask,
  UpdateTask,
  DeleteTask,
  StartTaskQueue,
  PauseTaskQueue,
  StopTaskQueue,
  CancelTask,
  ReorderTask,
  GetTaskQueueStatus,
  ClearCompletedTasks,
} from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime/runtime";

// Re-export types from models
export type { main };

// Helper to create BoundingBox from coordinates
export const createBoundingBox = (south: number, west: number, north: number, east: number): main.BoundingBox => {
  return new main.BoundingBox({ south, west, north, east });
};

// API wrapper with correct signatures matching Wails bindings
export const api = {
  // Tile Information
  getTileInfo: (bbox: main.BoundingBox, zoom: number) =>
    GetTileInfo(bbox, zoom),

  // Esri Wayback
  getEsriLayers: () =>
    GetEsriLayers(),

  getEsriTileURL: (date: string) =>
    GetEsriTileURL(date),

  downloadEsriImagery: (bbox: main.BoundingBox, zoom: number, date: string, format: string) =>
    DownloadEsriImagery(bbox, zoom, date, format),

  downloadEsriImageryRange: (bbox: main.BoundingBox, zoom: number, dates: string[], format: string) =>
    DownloadEsriImageryRange(bbox, zoom, dates, format),

  // Google Earth Current
  getGoogleEarthTileURL: () =>
    GetGoogleEarthTileURL(),

  downloadGoogleEarthImagery: (bbox: main.BoundingBox, zoom: number, format: string) =>
    DownloadGoogleEarthImagery(bbox, zoom, format),

  // Google Earth Historical
  getGoogleEarthDatesForArea: (bbox: main.BoundingBox, zoom: number) =>
    GetGoogleEarthDatesForArea(bbox, zoom),

  getGoogleEarthHistoricalTileURL: (quadtree: string, epoch: number) =>
    GetGoogleEarthHistoricalTileURL(quadtree, epoch),

  downloadGoogleEarthHistoricalImagery: (
    bbox: main.BoundingBox,
    zoom: number,
    hexDate: string,
    epoch: number,
    dateStr: string,
    format: string
  ) => DownloadGoogleEarthHistoricalImagery(bbox, zoom, hexDate, epoch, dateStr, format),

  downloadGoogleEarthHistoricalImageryRange: (
    bbox: main.BoundingBox,
    zoom: number,
    dates: main.GEDateInfo[],
    format: string
  ) => DownloadGoogleEarthHistoricalImageryRange(bbox, zoom, dates, format),

  // Video Export
  exportTimelapseVideo: (
    bbox: main.BoundingBox,
    zoom: number,
    dates: main.GEDateInfo[],
    source: string,
    videoOpts: main.VideoExportOptions
  ) => ExportTimelapseVideo(bbox, zoom, dates, source, videoOpts),

  // General Date Query
  getAvailableDatesForArea: (bbox: main.BoundingBox, zoom: number) =>
    GetAvailableDatesForArea(bbox, zoom),

  // Download Management
  selectDownloadFolder: () =>
    SelectDownloadFolder(),

  getDownloadPath: () =>
    GetDownloadPath(),

  setDownloadPath: (path: string) =>
    SetDownloadPath(path),

  openDownloadFolder: () =>
    OpenDownloadFolder(),

  // Tile Server
  startTileServer: () =>
    StartTileServer(),

  // Events
  onDownloadProgress: (callback: (progress: any) => void) =>
    EventsOn("download-progress", callback),

  onLog: (callback: (log: string) => void) =>
    EventsOn("log", callback),

  // Task Queue API
  addExportTask: (task: main.TaskQueueExportTask) =>
    AddExportTask(task),

  getTaskQueue: () =>
    GetTaskQueue(),

  getTask: (id: string) =>
    GetTask(id),

  updateTask: (id: string, updates: Record<string, any>) =>
    UpdateTask(id, updates),

  deleteTask: (id: string) =>
    DeleteTask(id),

  startTaskQueue: () =>
    StartTaskQueue(),

  pauseTaskQueue: () =>
    PauseTaskQueue(),

  stopTaskQueue: () =>
    StopTaskQueue(),

  cancelTask: (id: string) =>
    CancelTask(id),

  reorderTask: (id: string, newIndex: number) =>
    ReorderTask(id, newIndex),

  getTaskQueueStatus: () =>
    GetTaskQueueStatus(),

  clearCompletedTasks: () =>
    ClearCompletedTasks(),

  // Task Queue Events
  onTaskQueueUpdate: (callback: (status: main.TaskQueueStatus) => void) =>
    EventsOn("task-queue-update", callback),

  onTaskProgress: (callback: (event: { taskId: string; progress: main.TaskProgress }) => void) =>
    EventsOn("task-progress", callback),

  onTaskComplete: (callback: (event: { taskId: string; success: boolean; error?: string }) => void) =>
    EventsOn("task-complete", callback),

  onSystemNotification: (callback: (notification: { title: string; message: string; type: string }) => void) =>
    EventsOn("system-notification", callback),
};
