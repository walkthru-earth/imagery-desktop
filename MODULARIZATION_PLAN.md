# App.go Modularization Plan

## Current State
- **File Size**: 3,395 lines
- **Functions**: 62 functions
- **Issues**:
  - Monolithic structure (all logic in one file)
  - Mixed concerns (HTTP handlers, downloads, image processing, video export)
  - Backward compatibility code with legacy provider strings
  - Google Earth has persistent cache via proxy, Esri does not

## Goals
1. **Reduce app.go to < 500 lines** - Make it a thin coordinator
2. **Separate concerns** - Each package has single responsibility
3. **Remove legacy code** - No backward compatibility for old provider strings
4. **Add Esri caching** - Persistent cache proxy like Google Earth

---

## Refactoring Phases

### Phase 1: Remove Legacy Code ✅
**Target**: Clean up backward compatibility

**Changes**:
- Remove `"google"`, `"ge"`, `"esri"` from all switch/case statements
- Use only `common.ProviderGoogleEarth` and `common.ProviderEsriWayback`
- Update all comparisons to use constants

**Files Modified**:
- `app.go` (lines 2630, 3216, 3241, 3246)
- `internal/ratelimit/handler.go` (line 259)

---

### Phase 2: Extract Naming Utilities
**Target**: Create `internal/utils/naming` package

**Functions to Extract** (from app.go):
```go
// Line 79
func GenerateQuadkey(south, west, north, east float64, zoom int) string

// Line 106
func GenerateBBoxString(bbox BoundingBox) string

// Line 111
func SanitizeCoordinate(coord float64) string

// Line 136
func GenerateGeoTIFFFilename(source, date string, bbox BoundingBox, zoom int) string

// Line 151
func GenerateTilesDirName(source, date string, zoom int) string
```

**New Package Structure**:
```
internal/utils/
└── naming/
    ├── filename.go      # GeoTIFF and tiles directory naming
    ├── coordinates.go   # Coordinate formatting and quadkey generation
    └── bbox.go          # BBox string generation
```

---

### Phase 3: Extract Tile Server Handlers
**Target**: Create `internal/handlers/tileserver` package

**Functions to Extract** (from app.go):
```go
// HTTP Server
func corsMiddleware(next http.Handler) http.Handler            // Line 1372
func StartTileServer(port string) error                        // Line 1392

// Google Earth Handlers
func handleGoogleEarthTile(w http.ResponseWriter, r *http.Request)           // Line 1423
func handleGoogleEarthHistoricalTile(w http.ResponseWriter, r *http.Request) // Line 1821
func fetchHistoricalGETile(hexDate string, epoch int, tile *googleearth.Tile, date string) // Line 1678
func fetchHistoricalGETileWithZoomFallback(...)               // Line 1548
func extractQuadrantFromFallbackTile(...)                      // Line 1599

// Esri Handler (NEW - to be created)
func handleEsriTile(w http.ResponseWriter, r *http.Request)

// Utilities
func serveTransparentTile(w http.ResponseWriter)               // Line 1962
```

**New Package Structure**:
```
internal/handlers/
└── tileserver/
    ├── server.go          # HTTP server setup, CORS middleware
    ├── googleearth.go     # Google Earth tile handlers
    ├── esri.go            # Esri tile handlers (NEW - with caching!)
    └── utils.go           # Shared handler utilities (transparent tile, etc.)
```

---

### Phase 4: Add Esri Proxy Server with Caching
**Target**: Implement persistent cache for Esri tiles

**Rationale**: Currently Google Earth has tile reprojection proxy with cache, but Esri fetches directly without persistent cache.

**Implementation**:
```go
// internal/handlers/tileserver/esri.go

// handleEsriTile serves Esri Wayback tiles with persistent caching
// URL format: /esri-wayback/{date}/{z}/{x}/{y}
func (s *TileServer) handleEsriTile(w http.ResponseWriter, r *http.Request) {
    // 1. Parse URL: date, z, x, y
    // 2. Check cache: cache.Get(ProviderEsriWayback, z, x, y, date)
    // 3. If cache hit: serve cached tile
    // 4. If cache miss:
    //    a. Find layer for date
    //    b. Fetch tile from Esri API
    //    c. Cache tile: cache.Set(ProviderEsriWayback, z, x, y, date, data)
    //    d. Serve tile
}
```

**Benefits**:
- Consistent caching behavior across providers
- Faster tile loading for Esri imagery
- Reduced API calls to Esri servers
- Same URL pattern as Google Earth (`/provider/{date}/{z}/{x}/{y}`)

---

### Phase 5: Extract Download Functions
**Target**: Create `internal/downloads` package

**Functions to Extract**:

**Esri Downloads** → `internal/downloads/esri/esri.go`:
```go
func findLayerForDate(date string) (*wmts.Layer, error)              // Line 543
func DownloadImagery(bbox BoundingBox, zoom int, date, format string)  // Line 681
func DownloadImageryRange(bbox BoundingBox, zoom int, dates []AvailableDate, format string)  // Line 1209
```

**Google Earth Downloads** → `internal/downloads/googleearth/googleearth.go`:
```go
func DownloadImagery(bbox BoundingBox, zoom int, timestamp, format string)  // Line 1020
func DownloadHistoricalImagery(bbox BoundingBox, zoom int, hexDate string, epoch int, dateStr, format string)  // Line 2170
func DownloadHistoricalImageryRange(bbox BoundingBox, zoom int, dates []GEAvailableDate, format string)  // Line 2457
```

**Shared Download Logic** → `internal/downloads/common.go`:
```go
// Common download patterns
type DownloadOptions struct {
    BBox   BoundingBox
    Zoom   int
    Format string
    Provider string
}

// Progress callback
type ProgressCallback func(current, total int, status string)
```

**New Package Structure**:
```
internal/downloads/
├── common.go              # Shared types and interfaces
├── esri/
│   └── esri.go           # Esri-specific download logic
└── googleearth/
    └── googleearth.go    # Google Earth download logic
```

---

### Phase 6: Extract Image Processing
**Target**: Expand `internal/imagery/processing` package

**Functions to Extract**:
```go
func saveAsGeoTIFF(...)                    // Line 927
func savePNGCopy(...)                      // Line 934
func saveAsGeoTIFFWithMetadata(...)        // Line 951
func loadGeoTIFFImage(...)                 // Line 2891
```

**New Package Structure**:
```
internal/imagery/
├── downloader.go          # Existing download worker
└── processing/
    ├── geotiff.go         # GeoTIFF save/load operations
    ├── png.go             # PNG sidecar creation
    └── metadata.go        # Metadata XML generation
```

---

### Phase 7: Extract Video Export
**Target**: Expand `internal/video` package

**Functions to Extract**:
```go
func ExportTimelapseVideo(...)             // Line 2498
func exportTimelapseVideoInternal(...)     // Line 2503
func ReExportVideo(...)                    // Line 2766
func calculateSpotlightPixels(...)         // Line 2913
```

**New Package Structure**:
```
internal/video/
├── export.go              # Video export logic
├── spotlight.go           # Spotlight coordinate calculation
└── presets.go             # Video encoding presets
```

---

### Phase 8: Slim Down app.go
**Target**: Reduce app.go to thin coordinator (< 500 lines)

**Remaining Functions in app.go**:
- `startup()`, `Shutdown()` - Lifecycle
- `NewApp()` - Constructor
- Wails API wrappers (thin delegates to packages):
  - `GetTileInfo()` → delegates to `downloads.CalculateTileInfo()`
  - `GetEsriWaybackDatesForArea()` → delegates to `esri.GetDatesForArea()`
  - `GetGoogleEarthDatesForArea()` → delegates to `googleearth.GetDatesForArea()`
  - `DownloadEsriImagery()` → delegates to `downloads/esri.DownloadImagery()`
  - `DownloadGoogleEarthHistoricalImagery()` → delegates to `downloads/googleearth.DownloadHistoricalImagery()`
  - `ExportTimelapseVideo()` → delegates to `video.ExportTimelapse()`
  - Task queue wrappers (already mostly delegated)

**Final app.go Structure**:
```go
package main

import (
    "imagery-desktop/internal/handlers/tileserver"
    "imagery-desktop/internal/downloads/esri"
    "imagery-desktop/internal/downloads/googleearth"
    "imagery-desktop/internal/video"
    "imagery-desktop/internal/taskqueue"
    // ...
)

type App struct {
    ctx              context.Context
    tileServer       *tileserver.Server
    esriDownloader   *esri.Downloader
    geDownloader     *googleearth.Downloader
    videoExporter    *video.Exporter
    taskQueue        *taskqueue.Queue
    // ...
}

// Lifecycle
func (a *App) startup(ctx context.Context) error { ... }
func (a *App) Shutdown() error { ... }

// Wails API - Thin delegates
func (a *App) GetTileInfo(bbox BoundingBox, zoom int) (TileInfo, error) {
    return downloads.CalculateTileInfo(bbox, zoom)
}

func (a *App) DownloadEsriImagery(bbox BoundingBox, zoom int, date, format string) error {
    return a.esriDownloader.Download(bbox, zoom, date, format)
}

// ... (simple delegate methods)
```

---

## File Size Estimates After Refactoring

| File | Current Lines | Target Lines | Reduction |
|------|---------------|--------------|-----------|
| `app.go` | 3,395 | ~400 | -2,995 (-88%) |
| `internal/utils/naming/*.go` | 0 | ~150 | +150 |
| `internal/handlers/tileserver/*.go` | 0 | ~600 | +600 |
| `internal/downloads/**/*.go` | 0 | ~1,200 | +1,200 |
| `internal/imagery/processing/*.go` | 0 | ~300 | +300 |
| `internal/video/*.go` | ~50 | ~400 | +350 |

**Total**: Same logic, but organized into 15+ files across 6 packages

---

## Migration Checklist

### Phase 1: Remove Legacy Code
- [ ] Remove `"google"`, `"ge"`, `"esri"` from switch/case
- [ ] Use only constants throughout

### Phase 2: Extract Naming Utilities
- [ ] Create `internal/utils/naming` package
- [ ] Move filename generation functions
- [ ] Update all references in app.go

### Phase 3: Extract Tile Server
- [ ] Create `internal/handlers/tileserver` package
- [ ] Move HTTP handlers and server logic
- [ ] Update app.go to use new package

### Phase 4: Add Esri Proxy
- [ ] Implement `handleEsriTile()` with caching
- [ ] Register `/esri-wayback/{date}/{z}/{x}/{y}` endpoint
- [ ] Update frontend to use new Esri tile URL

### Phase 5: Extract Downloads
- [ ] Create `internal/downloads` package structure
- [ ] Move Esri download functions
- [ ] Move Google Earth download functions
- [ ] Update app.go delegates

### Phase 6: Extract Image Processing
- [ ] Expand `internal/imagery/processing` package
- [ ] Move GeoTIFF save/load functions
- [ ] Update download packages to use new functions

### Phase 7: Extract Video Export
- [ ] Expand `internal/video` package
- [ ] Move video export logic
- [ ] Move spotlight calculation
- [ ] Update app.go delegate

### Phase 8: Verify & Test
- [ ] Build application successfully
- [ ] Test tile server (Google Earth + Esri)
- [ ] Test downloads (both providers)
- [ ] Test video export
- [ ] Verify cache persistence

---

## Breaking Changes

### Esri Tile URLs
**Old**: Direct Esri API calls (no proxy)
**New**: `/esri-wayback/{date}/{z}/{x}/{y}` (with caching)

**Frontend Update Required**: Update MapLibre tile source to use new Esri proxy URL

---

## Benefits After Refactoring

1. **Modularity**: Each package has single responsibility
2. **Testability**: Easier to unit test isolated packages
3. **Maintainability**: Changes localized to specific packages
4. **Reusability**: Packages can be used independently
5. **Performance**: Esri tiles now cached persistently
6. **Consistency**: Both providers use same caching pattern
7. **Clarity**: app.go becomes easy to understand coordinator

---

## Execution Order

1. ✅ Create this plan
2. Remove backward compatibility
3. Extract naming utilities (low risk)
4. Extract tile server handlers (medium risk)
5. Add Esri proxy with caching (**high impact!**)
6. Extract downloads (medium risk)
7. Extract image processing (low risk)
8. Extract video export (low risk)
9. Final cleanup of app.go
10. Test everything

**Estimated Time**: 2-3 hours
**Risk Level**: Medium (comprehensive testing required)
