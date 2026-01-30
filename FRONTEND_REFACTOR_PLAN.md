# Frontend Refactoring Plan

**Date:** 2026-01-30
**Status:** In Progress
**Goal:** Transform monolithic App.tsx (1,313 lines) into modular, scalable architecture

---

## ✅ Completed

### 1. Package Installation
- ✅ Installed `react-map-gl` (vis.gl) for MapLibre React integration
- ✅ Installed `@maplibre/maplibre-gl-compare` for split-screen comparison
- ✅ All dependencies verified (0 vulnerabilities)

### 2. Branding & Design System
- ✅ Downloaded Quicksand font (5 weights: 300, 400, 500, 600, 700)
- ✅ Added `@font-face` declarations to index.css
- ✅ Set Quicksand as default font family
- ✅ Created `Header.tsx` component with logo and branding

### 3. Foundation Files
- ✅ Created `/types/index.ts` with all TypeScript interfaces
- ✅ Created `/services/api.ts` wrapping all Wails backend calls
- ✅ Created modular directory structure:
  ```
  frontend/src/
  ├── contexts/        # React contexts
  ├── hooks/           # Custom hooks
  ├── components/
  │   ├── Layout/      # Header, Sidebar
  │   ├── Map/         # MapContainer, layers
  │   ├── Controls/    # Layer panels, timeline
  │   └── Dialogs/     # Modals
  ├── services/        # API wrappers
  └── types/           # TypeScript types
  ```

---

## 🚧 In Progress

### 4. Core Architecture Refactoring

The current App.tsx (1,313 lines) needs to be split into:

#### A. React Contexts (State Management)

**`contexts/MapContext.tsx`**
```typescript
interface MapContextValue {
  map: maplibregl.Map | null;
  mapLoaded: boolean;
  bbox: BoundingBox | null;
  zoom: number;
  viewMode: MapViewMode; // 'single' | 'split'
  setMap: (map: maplibregl.Map) => void;
  setBbox: (bbox: BoundingBox | null) => void;
  setZoom: (zoom: number) => void;
  setViewMode: (mode: MapViewMode) => void;
}
```

**`contexts/ImageryContext.tsx`**
```typescript
interface ImageryContextValue {
  // Active source
  activeSource: ImagerySource; // 'esri' | 'google'
  setActiveSource: (source: ImagerySource) => void;

  // Esri dates
  availableDates: AvailableDate[];
  selectedDateIndex: number;
  dateRangeMode: boolean;
  dateRangeIndex: number[];

  // Google Earth dates
  geDates: GEAvailableDate[];
  geSelectedDateIndex: number;
  geDateRangeMode: boolean;
  geDateRangeIndex: number[];

  // Layer visibility/opacity
  layerVisibility: LayerVisibility;
  layerOpacity: LayerOpacity;

  // Actions
  loadEsriDates: () => Promise<void>;
  loadGoogleEarthDates: (bbox: BoundingBox, zoom: number) => Promise<void>;
  selectDate: (index: number) => void;
  toggleRangeMode: () => void;
}
```

**`contexts/DownloadContext.tsx`**
```typescript
interface DownloadContextValue {
  downloadPath: string;
  downloadProgress: DownloadProgress | null;
  isDownloading: boolean;
  downloadFormat: DownloadFormat; // 'geotiff' | 'png'
  logs: string[];

  startDownload: () => Promise<void>;
  selectFolder: () => Promise<void>;
  openFolder: () => void;
  clearLogs: () => void;
}
```

**`contexts/TimelineContext.tsx`** (NEW)
```typescript
interface TimelineContextValue {
  playbackState: TimelineState;
  play: () => void;
  pause: () => void;
  stop: () => void;
  setSpeed: (speed: number) => void;
  toggleLoop: () => void;
  goToDate: (index: number) => void;
}
```

#### B. Custom Hooks

**`hooks/useMap.ts`**
- Initialize MapLibre map
- Handle map lifecycle (load, cleanup)
- Manage map events (zoom, move)

**`hooks/useImageryDates.ts`**
- Fetch Esri dates
- Fetch Google Earth dates
- Manage date selection logic
- Handle date range selection

**`hooks/useBboxSelection.ts`**
- Draw mode (mouse events)
- Viewport mode
- Bbox validation

**`hooks/useLayerVisibility.ts`**
- Toggle layer visibility
- Manage layer opacity
- Sync with map layers

**`hooks/useTimelinePlayback.ts`** (NEW)
- Auto-advance through dates
- Speed control (0.5x, 1x, 2x, 5x)
- Loop functionality
- Preload next tiles

**`hooks/useDownload.ts`**
- Handle download progress events
- Manage download state
- Format selection

#### C. React Components

**Layout Components**
- ✅ `Header.tsx` - Logo, title, branding
- `Sidebar.tsx` - Main sidebar container
- `MainLayout.tsx` - Overall app layout

**Map Components**
- `MapContainer.tsx` - Single map view (react-map-gl wrapper)
- `MapCompare.tsx` - Split-screen with slider (@maplibre/maplibre-gl-compare)
- `ImageryLayer.tsx` - Dynamic raster layer for imagery
- `BboxLayer.tsx` - Selection polygon visualization

**Control Components**
- `LayerPanel.tsx` - Layer visibility/opacity controls (IMPROVED UI/UX)
- `SelectionPanel.tsx` - Draw vs Viewport mode
- `DatePicker.tsx` - Date selection with year grouping
- `ZoomControl.tsx` - Zoom level selector
- `DownloadPanel.tsx` - Download options and path
- `TimelinePlayer.tsx` (NEW) - Play/pause, speed, progress bar
- `ExportPanel.tsx` (NEW) - Timelapse export UI (backend integration)

**Dialog Components**
- `DownloadProgressModal.tsx` - Progress dialog
- `LogsDialog.tsx` - Application logs viewer

---

## 🎯 Priority Features to Implement

### Phase 1: Core Refactoring (Foundation)
1. Create all contexts (MapContext, ImageryContext, DownloadContext)
2. Create essential hooks (useMap, useImageryDates, useBboxSelection)
3. Extract map components (MapContainer, ImageryLayer, BboxLayer)
4. Update App.tsx to use contexts and components

### Phase 2: Enhanced Map Features
5. Implement `MapCompare.tsx` with @maplibre/maplibre-gl-compare
6. Add toggle between single/split view modes
7. Improve `LayerPanel.tsx` with better UI/UX:
   - Visual toggle switches (shadcn/ui Switch)
   - Better opacity sliders with live preview
   - Grouped controls for each layer
   - Icons for each layer type

### Phase 3: Timeline Playback (NEW Feature)
8. Create `TimelineContext` and `useTimelinePlayback` hook
9. Build `TimelinePlayer.tsx` component:
   - Play/Pause/Stop buttons
   - Speed selector (0.5x, 1x, 2x, 5x)
   - Progress bar with date labels
   - Loop toggle
10. Integrate with map to auto-advance imagery

### Phase 4: Timelapse Export Preparation
11. Create `ExportPanel.tsx` UI component:
    - Format selector (MP4, GIF, WebM)
    - Quality/framerate settings
    - Frame range selector
    - Export button
12. Add backend Wails functions:
    - `CaptureMapFrame() []byte` - Capture current map as image
    - `ExportTimelapse(frames [][]byte, format string, fps int) string` - Create video
    - Use Go libraries: `github.com/go-webp/webp`, `github.com/icza/mjpeg`
13. Wire up frontend to backend export pipeline

---

## 📐 Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         App.tsx                              │
│                    (Main Layout Only)                        │
└───────────────────────────┬─────────────────────────────────┘
                            │
        ┌───────────────────┴───────────────────┐
        │                                       │
┌───────▼────────┐                    ┌────────▼────────┐
│   Providers    │                    │   Components    │
│                │                    │                 │
│ - MapContext   │                    │ - Header        │
│ - ImageryCtx   │◄───────────────────│ - Sidebar       │
│ - DownloadCtx  │                    │ - MapContainer  │
│ - TimelineCtx  │                    │ - MapCompare    │
└────────────────┘                    │ - LayerPanel    │
                                      │ - DatePicker    │
                                      │ - TimelinePlayer│
                                      │ - ExportPanel   │
                                      └─────────────────┘
```

---

## 🎨 UI/UX Improvements

### Before (Current):
- Basic opacity sliders
- Eye icons for visibility
- No playback controls
- No split-screen
- No timelapse export

### After (Target):
- **Better Layer Controls:**
  - Visual toggle switches with icons
  - Opacity sliders with percentage display
  - Live preview during adjustment
  - Grouped panels (OSM, Imagery Source, Bbox)
  - Layer thumbnails/previews

- **Split-Screen Comparison:**
  - Vertical or horizontal slider
  - Compare Esri (left) vs Google Earth (right)
  - Synchronized pan/zoom/rotate
  - Independent date selection per side

- **Timeline Playback:**
  - Play button auto-advances through dates
  - Speed control (0.5x, 1x, 2x, 5x)
  - Progress bar with date markers
  - Loop toggle
  - Smooth transitions between frames

- **Timelapse Export:**
  - UI for selecting frame range
  - Format selection (MP4, GIF, WebM)
  - Quality/FPS settings
  - Export progress indicator
  - Backend handles video encoding

---

## 🔧 Technical Decisions

### React State Management
- **Contexts** for global state (map, imagery, download)
- **Local state** for UI-only state (dialogs, dropdowns)
- **No Redux** - contexts are sufficient for this app size

### MapLibre Integration
- Use **react-map-gl** for React-friendly API
- Wrapped in custom components for additional logic
- Keep vanilla maplibregl.Map accessible for advanced features

### Split-Screen Implementation
- Use **@maplibre/maplibre-gl-compare** plugin
- Two separate Map instances
- Synchronized navigation via plugin
- Toggle between single/split modes

### Timeline Playback
- `setInterval` for auto-advancement
- Preload next tiles to avoid flicker
- Use MapLibre's AnimationOptions for smooth transitions
- Pause on user interaction

### Timelapse Export (Backend)
- **Why backend:** Video encoding is CPU-intensive
- Go library: `github.com/icza/mjpeg` for MP4
- Capture frames via screenshot of canvas
- Export formats: MP4 (H.264), GIF (animated), WebM (VP8)

---

## 📦 Dependencies Added

```json
{
  "dependencies": {
    "react-map-gl": "^7.x.x",
    "@maplibre/maplibre-gl-compare": "^1.x.x"
  }
}
```

**No canvas-capture needed** - backend will handle export

---

## 🚀 Next Steps

1. **Immediate:** Create MapContext and refactor map initialization
2. **Next:** Create ImageryContext and extract date management
3. **Then:** Build LayerPanel with improved UI
4. **Then:** Implement MapCompare for split-screen
5. **Finally:** Add TimelinePlayer and export UI

---

## 📝 Notes

- Backend export (MP4/GIF) simplifies frontend - no ffmpeg.wasm needed
- Modular architecture makes it easy to add features later
- Each component is independently testable
- Contexts prevent prop drilling
- Custom hooks encapsulate business logic

---

**Status:** Foundation complete, ready for core refactoring
**ETA:** 4-6 hours for complete refactoring + new features
