/**
 * Imagery source types and interfaces
 * Designed to be extensible for future imagery providers
 */

export type ImagerySourceType = "esri_wayback" | "google_earth" | string;

export interface ImageryDate {
  date: string;
  // Future: Add metadata like resolution, quality, etc.
}

export interface ImagerySource {
  id: ImagerySourceType;
  name: string;
  dates: ImageryDate[];
  getTileURL: (date?: string, zoom?: number, bbox?: BoundingBox) => Promise<string>;
  // Future: Add capabilities like maxZoom, minZoom, attribution
}

export interface BoundingBox {
  north: number;
  south: number;
  east: number;
  west: number;
}

export interface MapViewport {
  center: [number, number];
  zoom: number;
  bbox: BoundingBox;
}
