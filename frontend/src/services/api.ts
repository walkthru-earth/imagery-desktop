/**
 * API Service Layer
 * Wraps Wails Go backend functions for cleaner imports and better type safety
 */

import {
  GetTileInfo,
  GetEsriLayers,
  DownloadEsriImagery,
  DownloadEsriImageryRange,
  DownloadGoogleEarthImagery,
  DownloadGoogleEarthHistoricalImagery,
  DownloadGoogleEarthHistoricalImageryRange,
  SelectDownloadFolder,
  GetDownloadPath,
  OpenDownloadFolder,
  GetEsriTileURL,
  GetGoogleEarthTileURL,
  GetGoogleEarthDatesForArea,
  GetGoogleEarthHistoricalTileURL,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { BoundingBox } from "@/types";

export const api = {
  // Tile Information
  getTileInfo: (bbox: BoundingBox, zoom: number) => GetTileInfo(bbox.south, bbox.west, bbox.north, bbox.east, zoom),

  // Esri Wayback
  getEsriLayers: () => GetEsriLayers(),
  getEsriTileURL: (date: string, x: number, y: number, z: number) => GetEsriTileURL(date, x, y, z),
  downloadEsriImagery: (bbox: BoundingBox, date: string, zoom: number, format: string) =>
    DownloadEsriImagery(bbox.south, bbox.west, bbox.north, bbox.east, date, zoom, format),
  downloadEsriImageryRange: (bbox: BoundingBox, dates: string[], zoom: number, format: string) =>
    DownloadEsriImageryRange(bbox.south, bbox.west, bbox.north, bbox.east, dates, zoom, format),

  // Google Earth
  getGoogleEarthTileURL: (x: number, y: number, z: number) => GetGoogleEarthTileURL(x, y, z),
  getGoogleEarthDatesForArea: (bbox: BoundingBox, zoom: number) =>
    GetGoogleEarthDatesForArea(bbox.south, bbox.west, bbox.north, bbox.east, zoom),
  getGoogleEarthHistoricalTileURL: (quadtree: string, epoch: number, hexDate: string) =>
    GetGoogleEarthHistoricalTileURL(quadtree, epoch, hexDate),
  downloadGoogleEarthImagery: (bbox: BoundingBox, zoom: number, format: string) =>
    DownloadGoogleEarthImagery(bbox.south, bbox.west, bbox.north, bbox.east, zoom, format),
  downloadGoogleEarthHistoricalImagery: (bbox: BoundingBox, hexDate: string, zoom: number, format: string) =>
    DownloadGoogleEarthHistoricalImagery(bbox.south, bbox.west, bbox.north, bbox.east, hexDate, zoom, format),
  downloadGoogleEarthHistoricalImageryRange: (bbox: BoundingBox, hexDates: string[], zoom: number, format: string) =>
    DownloadGoogleEarthHistoricalImageryRange(bbox.south, bbox.west, bbox.north, bbox.east, hexDates, zoom, format),

  // Download Management
  selectDownloadFolder: () => SelectDownloadFolder(),
  getDownloadPath: () => GetDownloadPath(),
  openDownloadFolder: () => OpenDownloadFolder(),

  // Events
  onDownloadProgress: (callback: (progress: any) => void) => EventsOn("download-progress", callback),
  onLog: (callback: (log: string) => void) => EventsOn("log", callback),
};
