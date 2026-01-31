# Backend Settings Analysis

## Overview

This document analyzes all backend settings in `internal/config/settings.go` and identifies which ones are exposed in the frontend settings dialog.

---

## 📊 Complete Backend Settings Inventory

### Cache Settings (3 fields)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `CachePath` | string | `~/.walkthru-earth/imagery-desktop/cache/` | ❌ **MISSING** | Custom cache location |
| `CacheMaxSizeMB` | int | 500 MB | ✅ Yes | Maximum cache size |
| `CacheTTLDays` | int | 90 days | ✅ Yes | Time to live for cached tiles |

**Default Cache Path**:
- macOS/Linux: `/Users/username/.walkthru-earth/imagery-desktop/cache/`
- Windows: `C:\Users\username\.walkthru-earth\imagery-desktop\cache\`

---

### Download Settings (3 fields)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `DownloadPath` | string | `~/Downloads/imagery` | ✅ Yes | Where downloads are saved |
| `DownloadZoomStrategy` | string | `"fixed"` | ❌ **MISSING** | "current" or "fixed" |
| `DownloadFixedZoom` | int | 19 | ❌ **MISSING** | Fixed zoom for downloads |

---

### Rate Limit Settings (1 field)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `AutoRetryOnRateLimit` | bool | `true` | ✅ Yes | Auto-retry on rate limits |

---

### Default Map Settings (4 fields)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `DefaultZoom` | int | 15 | ❌ **MISSING** | Initial map zoom |
| `DefaultSource` | string | `"esri_wayback"` | ❌ **MISSING** | Default imagery source |
| `DefaultCenterLat` | float64 | 30.0621 | ❌ **MISSING** | Cairo, Egypt |
| `DefaultCenterLon` | float64 | 31.2219 | ❌ **MISSING** | Cairo, Egypt |

---

### UI Preferences (5 fields)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `Theme` | string | `"system"` | ❌ **MISSING** | "light", "dark", "system" |
| `ShowTileGrid` | bool | `false` | ❌ **MISSING** | Display tile boundaries |
| `ShowCoordinates` | bool | `false` | ❌ **MISSING** | Display coordinates |
| `AutoOpenDownloadDir` | bool | `true` | ❌ **MISSING** | Auto-open after download |
| `TaskPanelOpen` | bool | `false` | ❌ **MISSING** | Task panel state |

---

### Task Queue Settings (1 field)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `MaxConcurrentTasks` | int | 1 (range: 1-5) | ❌ **MISSING** | Concurrent task limit |

---

### Advanced Settings (4 fields)

| Field | Type | Default | In Frontend? | Notes |
|-------|------|---------|--------------|-------|
| `CustomSources` | array | `[]` | ❌ **MISSING** | User-added imagery sources |
| `DateFilterPatterns` | array | 3 patterns | ❌ **MISSING** | Date filtering regex |
| `DefaultDatePattern` | string | `""` | ❌ **MISSING** | Active date filter |
| `LastCenterLat/Lon/Zoom` | floats | Cairo | ✅ Auto-saved | Last session state |

---

## 🚨 CRITICAL FINDINGS

### Missing from Frontend Settings Dialog

Out of **21 backend settings**, only **4 are exposed** in the frontend:
1. ✅ `DownloadPath` (with browse button)
2. ✅ `CacheMaxSizeMB` (input field)
3. ✅ `CacheTTLDays` (input field)
4. ✅ `AutoRetryOnRateLimit` (checkbox)

**17 settings are NOT exposed** (81% missing!)

---

## 📋 PRIORITY RECOMMENDATIONS

### Priority 1: Cache Settings ⚠️ HIGH

**Missing**:
- `CachePath` - Users cannot see or change where cache is stored

**Recommended UI**:
```
Cache Location:
[Browse] [Open Folder]
/Users/username/.walkthru-earth/imagery-desktop/cache/

Current Cache Usage:
250.3 MB / 500 MB (50%)
[Clear Cache] button

Max Cache Size (MB):
[500] (requires restart)

Cache TTL (days):
[90] (requires restart)
```

**Implementation**:
1. Add `cachePath` to frontend `UserSettings` interface
2. Display current cache path
3. Add browse button (like download path)
4. Add "Open Folder" button
5. Add cache statistics display (`GetCacheStats()`)
6. Add "Clear Cache" button (`ClearCache()`)

---

### Priority 2: Download Settings 🟡 MEDIUM

**Missing**:
- `DownloadZoomStrategy` - "current" (use map zoom) vs "fixed" (use fixed zoom)
- `DownloadFixedZoom` - Fixed zoom level when strategy is "fixed"

**Current Behavior**: Backend defaults to fixed zoom 19, but user can't change it in UI

**Recommended UI**:
```
Download Zoom Strategy:
○ Use Current Map Zoom
● Use Fixed Zoom: [19]

(If "Use Fixed Zoom" selected, show input field)
```

**Impact**: Users always download at zoom 19 even if they want different zoom levels

---

### Priority 3: Default Map Settings 🟡 MEDIUM

**Missing**:
- `DefaultZoom` - Initial map zoom (currently 15)
- `DefaultSource` - Initial imagery source (esri_wayback or google_earth)
- `DefaultCenterLat/Lon` - Initial map center (currently Cairo)

**Current Behavior**: Hardcoded to Cairo, Esri, zoom 15

**Recommended UI**:
```
Default Map Settings:
Initial Center: [30.0621, 31.2219]
  [Use Current Map Center]

Initial Zoom: [15]
  [Use Current Zoom]

Default Imagery Source:
  ○ Esri Wayback
  ○ Google Earth Historical
```

**Impact**: Users in different locations always start with Cairo view

---

### Priority 4: UI Preferences 🟢 LOW

**Missing**:
- `Theme` - Light/Dark/System theme
- `ShowTileGrid` - Debug overlay
- `ShowCoordinates` - Coordinate display
- `AutoOpenDownloadDir` - Auto-open downloads folder

**Recommended UI**:
```
UI Preferences:
Theme: [System ▾] (Light, Dark, System)

□ Show Tile Grid (debug overlay)
□ Show Coordinates on Map
☑ Auto-open Downloads Folder
```

**Impact**: Users can't customize UI appearance

---

### Priority 5: Task Queue Settings 🟢 LOW

**Missing**:
- `MaxConcurrentTasks` - How many tasks run simultaneously (1-5)

**Current Behavior**: Fixed at 1 task at a time

**Recommended UI**:
```
Task Queue:
Max Concurrent Tasks: [1 ▾] (1, 2, 3, 4, 5)

Note: Higher values may trigger rate limits
```

**Impact**: Users can't speed up downloads by running multiple tasks

---

### Priority 6: Advanced Settings 🔵 FUTURE

**Missing**:
- `CustomSources` - User-added tile sources (WMTS, WMS, XYZ, TMS)
- `DateFilterPatterns` - Regex filters for dates
- `DefaultDatePattern` - Active date filter

**These require dedicated UI panels** - consider for future versions

---

## 🔍 Frontend Settings Component Analysis

**File**: `frontend/src/components/SettingsDialog.tsx`

### Current TypeScript Interface (Lines 10-29)

```typescript
interface UserSettings {
  downloadPath: string;
  cacheMaxSizeMB: number;
  cacheTTLDays: number;
  autoRetryOnRateLimit: boolean;
}
```

### Backend UserSettings (Go struct - 21 fields)

```go
type UserSettings struct {
    // Only these 4 are in frontend:
    DownloadPath         string
    CacheMaxSizeMB       int
    CacheTTLDays         int
    AutoRetryOnRateLimit bool

    // Missing from frontend (17 fields):
    CachePath            string
    DefaultZoom          int
    DefaultSource        string
    DefaultCenterLat     float64
    DefaultCenterLon     float64
    DownloadZoomStrategy string
    DownloadFixedZoom    int
    Theme                string
    ShowTileGrid         bool
    ShowCoordinates      bool
    AutoOpenDownloadDir  bool
    MaxConcurrentTasks   int
    TaskPanelOpen        bool
    CustomSources        []CustomSource
    DateFilterPatterns   []DateFilterPattern
    DefaultDatePattern   string
    LastCenterLat        float64 // Auto-saved
    LastCenterLon        float64 // Auto-saved
    LastZoom             float64 // Auto-saved
}
```

---

## 📝 IMPLEMENTATION PLAN

### Phase 1: Cache Settings Enhancement (HIGH PRIORITY)

**Files to Modify**:
1. `frontend/src/components/SettingsDialog.tsx`
   - Add `cachePath` to `UserSettings` interface
   - Add cache statistics display
   - Add "Clear Cache" button
   - Add cache path display with browse/open buttons

2. `frontend/src/services/api.ts`
   - Add `getCacheStats()` wrapper
   - Add `clearCache()` wrapper

**Backend Functions** (already exist):
- ✅ `GetCacheStats()` - app_ratelimit.go:58
- ✅ `ClearCache()` - app_ratelimit.go:76
- ✅ `GetCachePath()` - Available via settings

---

### Phase 2: Download Settings (MEDIUM PRIORITY)

**Add to SettingsDialog.tsx**:
- Download zoom strategy radio buttons
- Fixed zoom input (conditionally shown)

**Backend**: No changes needed (already supported)

---

### Phase 3: Default Map Settings (MEDIUM PRIORITY)

**Add to SettingsDialog.tsx**:
- Default center lat/lon inputs
- "Use Current" button to copy current map position
- Default zoom selector
- Default source selector

**Backend**: No changes needed (already supported)

---

### Phase 4: UI Preferences (LOW PRIORITY)

**Add to SettingsDialog.tsx**:
- Theme selector dropdown
- Checkboxes for tile grid, coordinates, auto-open

**Backend**: No changes needed (already supported)

---

### Phase 5: Task Queue Settings (LOW PRIORITY)

**Add to SettingsDialog.tsx**:
- Max concurrent tasks dropdown (1-5)
- Warning about rate limits

**Backend**: No changes needed (already supported)

---

## 🎯 RECOMMENDED IMMEDIATE ACTIONS

1. **Add Cache Path Display** ⚠️ HIGH
   - Users should see where cache is stored
   - Add "Open Folder" button
   - Add cache statistics (size, tile count)

2. **Add Clear Cache Button** ⚠️ HIGH
   - Users need way to clear cache
   - Backend function exists, just need UI

3. **Add Download Zoom Settings** 🟡 MEDIUM
   - Let users choose download zoom strategy
   - Currently stuck at fixed zoom 19

4. **Add Default Map Settings** 🟡 MEDIUM
   - Let users set preferred starting location
   - Currently always starts at Cairo

5. **Keep GO_TILEPROXY_LEARNINGS.md** 📚
   - It documents security patterns we implemented
   - It lists future optimizations
   - Good reference for future development

---

## 📊 SUMMARY

| Category | Backend Fields | Frontend Fields | Missing | % Missing |
|----------|----------------|-----------------|---------|-----------|
| Cache | 3 | 2 | 1 | 33% |
| Download | 3 | 1 | 2 | 67% |
| Rate Limit | 1 | 1 | 0 | 0% |
| Map Defaults | 4 | 0 | 4 | 100% |
| UI Preferences | 5 | 0 | 5 | 100% |
| Task Queue | 1 | 0 | 1 | 100% |
| Advanced | 3 | 0 | 3 | 100% |
| Session State | 3 | 3 (auto) | 0 | 0% |
| **TOTAL** | **23** | **4** | **16** | **70%** |

**70% of backend settings are not exposed in the frontend!**

The backend has a comprehensive settings system, but the frontend only exposes 4 fields. This creates a disconnect where users can't configure many available options.
