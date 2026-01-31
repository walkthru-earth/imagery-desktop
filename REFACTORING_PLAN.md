# Comprehensive Refactoring Plan

## Goals
1. **Standardize naming** across frontend and backend
2. **Eliminate code duplication** by extracting common patterns
3. **Improve modularity** for easier scaling and maintenance
4. **Fix bugs** discovered during refactoring

## Phase 1: Backend Core Refactoring

### 1.1 Standardize Provider Naming
- [x] Analysis complete
- [ ] Change cache strings: `"google"` → `"google_earth"`, `"esri"` → `"esri_wayback"`
- [ ] Update URL paths: `/ge/` → `/google-earth/`, `/ge-historical/` → `/google-earth-historical/`
- [ ] Rename functions: Ensure all Google Earth functions use `GoogleEarth` prefix, all Esri use `EsriWayback`
- [ ] Update type names: `GEAvailableDate` → `GoogleEarthDate`, add `EsriWaybackDate`

### 1.2 Extract Common Utilities (NEW)
Create `internal/common/` package with:
- `tile_bounds.go` - Shared tile bounds calculation
- `date_format.go` - Centralized date parsing/formatting
- `download_format.go` - Download format validation
- `result.go` - Unified TileResult type

### 1.3 Fix Code Duplication
- [ ] **FIX BUG**: `decryptWithKey` uses wrong key variable (line 373-390)
- [ ] Refactor encryption methods in `googleearth/client.go`
- [ ] Extract HTTP header setup to shared interface
- [ ] Use existing `internal/imagery/downloader.go` instead of reimplemented workers
- [ ] Consolidate GeoTIFF saving methods

### 1.4 Standardize HTTP Handlers
- [ ] Rename: `handleGoogleEarthTile` → `handleGoogleEarthCurrentTile`
- [ ] Update URL registration to use consistent paths
- [ ] Extract common handler error patterns

## Phase 2: Frontend Refactoring

### 2.1 Regenerate Wails Bindings
- [ ] Run `wails generate` after backend changes
- [ ] Verify generated TypeScript matches new naming

### 2.2 Update TypeScript Types
- [ ] Change `ImagerySourceType` from `"google" | "esri"` to `"google_earth" | "esri_wayback"`
- [ ] Update all type references in contexts, components, hooks

### 2.3 Update API Service Layer
- [ ] Rename functions to match backend
- [ ] Update URL patterns to use new paths

### 2.4 Update Components
- [ ] Update SourceSelector display mapping
- [ ] Update context state property names
- [ ] Verify UI still displays "Google Earth" and "Esri Wayback"

## Phase 3: Documentation & Testing

### 3.1 Update Documentation
- [ ] README.md - Update cache structure examples
- [ ] ARCHITECTURE.md - Update naming conventions
- [ ] Add MIGRATION.md for users with existing caches

### 3.2 Testing
- [ ] Build and test backend
- [ ] Test frontend hot reload
- [ ] Verify tile loading for both providers
- [ ] Test download functionality
- [ ] Test cache invalidation

## Breaking Changes
- **Cache**: Old cache directories will be invalid (`google/` → `google_earth/`)
- **URLs**: Tile URLs will change (MapLibre layers need update)
- **API**: Frontend-backend contract changes (auto-handled by Wails bindings)

## Estimated Impact
- **Files Modified**: ~60 files
- **Lines Changed**: ~1500 lines
- **New Files Created**: ~8 utility files
- **Risk Level**: Medium (comprehensive testing required)
