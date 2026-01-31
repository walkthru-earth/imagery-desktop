# Key Learnings from go-tileproxy

## Overview
Analysis of [flywave/go-tileproxy](https://github.com/flywave/go-tileproxy) - a high-performance tile proxy server in Go.

**Repository Stats**:
- Language: Go
- Stars: 6
- Size: ~60KB codebase
- Supports: WMS, WMTS, Mapbox Vector Tiles, XYZ/OSM, Cesium 3D Tiles, ArcGIS REST

---

## 📚 Architectural Patterns We Can Adopt

### 1. **Interface-Based Cache Abstraction**

**Pattern**: Define minimal cache interfaces with focused responsibilities.

```go
// Their approach - simple, focused interface
type Cache interface {
    LoadTile(tile *Tile, withMetadata bool) error
    LoadTiles(tiles *TileCollection, withMetadata bool) error
    StoreTile(tile *Tile) error
    StoreTiles(tiles *TileCollection) error
    RemoveTile(tile *Tile) error
    IsCached(tile *Tile) bool
    LoadTileMetadata(tile *Tile) error
    LevelLocation(level int) string
}
```

**Application to Our Project**:
- Our `internal/cache/persistent_cache.go` already follows this pattern
- Could add `IsCached()` method for quick cache checks without loading data
- Could add batch operations (`LoadTiles`/`StoreTiles`) for performance

---

### 2. **Multiple Cache Layout Strategies**

**Pattern**: Support multiple directory layouts via function pointers.

```go
type TileLocationFunc func(*Tile, string, string, bool) (string, error)

func LocationPaths(layout string) (TileLocationFunc, func(int, string) string, error) {
    switch layout {
    case "tc":       return tile_location_tc, level_location, nil
    case "mp":       return tile_location_mp, level_location, nil
    case "tms":      return tile_location_tms, level_location, nil
    case "quadkey":  return tile_location_quadkey, no_level_location, nil
    case "arcgis":   return tile_location_arcgiscache, level_location_arcgiscache, nil
    }
}
```

**Supported Layouts**:
- **TMS**: `/z/x/y.ext` - Standard XYZ (what we use)
- **Quadkey**: `/quadkey.ext` - Bing Maps style
- **ArcGIS**: `/L02/R00000123/C00000456.ext` - ArcGIS compact cache
- **MapProxy (mp)**: `/z/0000/0001/0000/0002.ext` - Hierarchical 4-digit grouping
- **TileCache (tc)**: `/z/000/001/002/000/003/456.ext` - Deep 3-digit grouping

**Application to Our Project**:
- We currently hard-code TMS layout in `internal/cache/persistent_cache.go`
- Could make layout configurable via `internal/config/settings.go`
- **Recommendation**: Keep TMS for now (most common), but architect for future flexibility

---

### 3. **Path Traversal Security**

**Pattern**: Validate cache paths to prevent directory traversal attacks.

```go
func validateCachePath(cacheDir, filePath string) error {
    absCacheDir, _ := filepath.Abs(cacheDir)
    absFilePath, _ := filepath.Abs(filePath)
    relPath, _ := filepath.Rel(absCacheDir, absFilePath)

    if strings.HasPrefix(relPath, "..") {
        return fmt.Errorf("path traversal attempt detected")
    }
    return nil
}
```

**Application to Our Project**:
- **CRITICAL**: We don't validate cache paths in `persistent_cache.go`
- **Action Required**: Add validation before writing tiles
- User-controlled dates could potentially inject `../` sequences

---

### 4. **Coordinate Validation**

**Pattern**: Validate tile coordinates before processing.

```go
func validateCoordinates(x, y, z int) error {
    const maxZoom = 30
    if z < 0 || z > maxZoom {
        return fmt.Errorf("invalid zoom level: %d", z)
    }
    if x < 0 || y < 0 {
        return fmt.Errorf("negative coordinates not allowed")
    }
    return nil
}
```

**Application to Our Project**:
- We validate zoom in tile server handlers, but not x/y coordinates
- **Recommendation**: Add coordinate validation in cache layer for defense-in-depth

---

### 5. **Concurrent Tile Processing with Semaphores**

**Pattern**: Use semaphores to limit concurrent goroutines during batch operations.

```go
func (c *LocalCache) LoadTiles(tiles *TileCollection, withMetadata bool) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(tiles.tiles)+100)
    semaphore := make(chan struct{}, 10)  // Limit to 10 concurrent operations

    for _, tile := range tiles.tiles {
        wg.Add(1)
        go func(t *Tile) {
            defer wg.Done()
            semaphore <- struct{}{}        // Acquire
            defer func() { <-semaphore }() // Release

            // Load tile logic...
        }(tile)
    }

    wg.Wait()
    close(errChan)
    return nil
}
```

**Application to Our Project**:
- Our download logic in `app.go` uses worker pools but not semaphores
- **Recommendation**: Adopt semaphore pattern in new `internal/downloads` package for batch downloads

---

### 6. **Sync.Pool for Buffer Reuse**

**Pattern**: Reuse byte buffers to reduce GC pressure.

```go
type LocalCache struct {
    readBufPool   sync.Pool
    maxBufferSize int
}

func NewLocalCache(...) *LocalCache {
    c := &LocalCache{maxBufferSize: 10 * 1024 * 1024}
    c.readBufPool = sync.Pool{
        New: func() interface{} {
            return make([]byte, 0, 256*1024)  // 256KB initial capacity
        },
    }
    return c
}
```

**Application to Our Project**:
- We create new buffers for each tile operation
- **Recommendation**: Add buffer pool to cache layer for high-throughput scenarios
- Most useful for video export where we process many tiles sequentially

---

### 7. **Layer Abstraction for Different Data Sources**

**Pattern**: Use interfaces to abstract different tile sources (WMS, WMTS, XYZ, etc.).

```go
type Layer interface {
    GetMap(query *MapQuery) (tile.Source, error)
    GetResolutionRange() *geo.ResolutionRange
    IsSupportMetaTiles() bool
    GetExtent() *geo.MapExtent
    GetCoverage() geo.Coverage
}

type MapLayer struct {
    SupportMetaTiles bool
    ResRange         *geo.ResolutionRange
    Coverage         geo.Coverage
    Extent           *geo.MapExtent
}
```

**Application to Our Project**:
- We have hardcoded Google Earth and Esri logic
- **Future Enhancement**: Could abstract providers into a common `TileProvider` interface
- Would enable easier addition of new providers (Mapbox, Bing, etc.)

---

### 8. **Service-Layer Router Pattern**

**Pattern**: Use function maps for request routing.

```go
type TileService struct {
    router map[string]func(r request.Request) *Response
}

func NewTileService(opts *TileServiceOptions) *TileService {
    s := &TileService{}
    s.router = map[string]func(r request.Request) *Response{
        "map": func(r request.Request) *Response {
            return s.GetMap(r)
        },
        "tms_capabilities": func(r request.Request) *Response {
            return s.GetCapabilities(r)
        },
    }
    return s
}
```

**Application to Our Project**:
- Our tile server uses `http.ServeMux` with explicit route registration
- Current approach is clearer for our simple use case
- **No action needed** - our approach is appropriate

---

## 🔐 Security Improvements to Implement

### 1. **Path Traversal Protection**
```go
// Add to internal/cache/persistent_cache.go
func (c *PersistentCache) validatePath(tilePath string) error {
    absCache, _ := filepath.Abs(c.cacheDir)
    absTile, _ := filepath.Abs(tilePath)
    relPath, _ := filepath.Rel(absCache, absTile)

    if strings.HasPrefix(relPath, "..") {
        return fmt.Errorf("invalid tile path: path traversal detected")
    }
    return nil
}
```

### 2. **Coordinate Bounds Checking**
```go
// Add to internal/cache/persistent_cache.go or tile handlers
const MaxZoomLevel = 30

func validateTileCoords(z, x, y int) error {
    if z < 0 || z > MaxZoomLevel {
        return fmt.Errorf("invalid zoom: %d", z)
    }
    if x < 0 || y < 0 {
        return fmt.Errorf("invalid tile coordinates: x=%d, y=%d", x, y)
    }
    maxTiles := 1 << z  // 2^z
    if x >= maxTiles || y >= maxTiles {
        return fmt.Errorf("tile coords out of bounds for zoom %d", z)
    }
    return nil
}
```

### 3. **Filename Sanitization**
```go
// Already handled by our date format validation, but add explicit check
func sanitizeFilename(filename string) error {
    if strings.Contains(filename, "..") {
        return fmt.Errorf("invalid filename")
    }
    return nil
}
```

---

## ⚡ Performance Optimizations to Consider

### 1. **Buffer Pooling** (High Priority)
- **Where**: `internal/cache/persistent_cache.go`, video export
- **Impact**: Reduce GC pressure during batch operations
- **Effort**: Low (add sync.Pool)

### 2. **Batch Cache Operations** (Medium Priority)
- **Where**: Download range functions in `app.go`
- **Impact**: Reduce lock contention, faster bulk downloads
- **Effort**: Medium (refactor to use batch APIs)

### 3. **Semaphore-Based Concurrency** (Low Priority)
- **Where**: New `internal/downloads` package
- **Impact**: Better control over resource usage
- **Effort**: Low (replace current worker pool)

---

## 🏗️ Architectural Recommendations

### Apply Now (During Modularization)

1. **Security Validation**
   - Add path traversal checks to cache layer
   - Add coordinate validation to tile handlers
   - Priority: **HIGH** (security issue)

2. **Cache Interface Enhancement**
   - Add `IsCached(provider, z, x, y, date) bool` method
   - Useful for pre-checking before expensive operations
   - Priority: **MEDIUM**

3. **Error Collection Pattern**
   - Use error channels for batch operations (like their `LoadTiles`)
   - Better error reporting during range downloads
   - Priority: **MEDIUM**

### Consider for Future

1. **Pluggable Cache Layouts**
   - Make directory layout configurable
   - Support Quadkey for Bing Maps integration
   - Priority: **LOW** (no immediate need)

2. **Provider Abstraction**
   - Define `TileProvider` interface
   - Easier to add new imagery sources
   - Priority: **LOW** (works for now)

3. **Buffer Pooling**
   - Add to cache and image processing
   - Most useful for video export workloads
   - Priority: **MEDIUM** (performance optimization)

---

## 📋 Action Items for Current Refactoring

### Phase 5 (Downloads Package) - Apply These Patterns:
- ✅ Use semaphore pattern for concurrent downloads
- ✅ Implement error collection with channels
- ✅ Add coordinate validation before downloads

### Phase 6 (Image Processing) - Apply These Patterns:
- ✅ Add buffer pool for image operations
- ✅ Use batch processing patterns

### New Phase (Security Hardening):
- ✅ Add path traversal validation to cache
- ✅ Add coordinate bounds checking
- ✅ Add max zoom level constant

---

## 🎯 Summary

**Key Takeaways**:
1. **Security First**: Add path validation and coordinate checking
2. **Performance**: Buffer pools and semaphores for batch operations
3. **Architecture**: Interface-based design enables future extensibility
4. **Testing**: They have comprehensive test coverage we should match

**Best Patterns to Adopt**:
- ✅ Path traversal validation (security)
- ✅ Coordinate validation (security + correctness)
- ✅ Semaphore-based concurrency (performance)
- ✅ Error collection channels (better error handling)
- 🤔 Buffer pooling (performance, if needed)
- 🤔 Cache layout abstraction (future flexibility)

**Patterns to Skip**:
- ❌ Complex layer abstraction (overkill for 2 providers)
- ❌ Router function maps (our ServeMux is clearer)
- ❌ Multiple cache layouts (no requirement yet)
