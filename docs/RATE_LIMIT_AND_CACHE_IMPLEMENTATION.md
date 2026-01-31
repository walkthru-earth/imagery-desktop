# Rate Limit Handling & Persistent Cache Implementation

**Created:** 2026-01-31
**Status:** Phase 1 Complete (Backend), Phase 2 Pending (Integration & UI)

## Overview

This document describes the implementation of two critical features:
1. **Rate Limit Detection & Handling** with exponential backoff and user notifications
2. **Persistent Tile Cache** with OGC ZXY structure for cross-session performance

---

## Phase 1: Core Components (COMPLETED ✅)

### 1. Rate Limit Handler

**File:** [`internal/ratelimit/handler.go`](internal/ratelimit/handler.go)

**Features:**
- Detects rate limits from HTTP responses (403, 429, 509 status codes)
- Exponential backoff retry strategy: 5min → 10min → 15min → 20min → 30min
- Automatic retry after backoff interval (configurable)
- Manual retry option for user-initiated retries
- Callback system for UI notifications

**Usage:**
```go
// Initialize handler
rateLimitHandler := ratelimit.NewHandler(nil) // Uses default strategy

// Set callbacks for UI notifications
rateLimitHandler.SetOnRateLimit(func(event ratelimit.RateLimitEvent) {
    // Show notification to user
    wailsRuntime.EventsEmit(ctx, "rate-limit-detected", event)
})

rateLimitHandler.SetOnRetry(func(event ratelimit.RateLimitEvent) {
    // Notify user of retry attempt
    wailsRuntime.EventsEmit(ctx, "rate-limit-retry", event)
})

rateLimitHandler.SetOnRecovered(func(provider string) {
    // Notify user that rate limit cleared
    wailsRuntime.EventsEmit(ctx, "rate-limit-cleared", provider)
})

// Check response for rate limiting
if rateLimitHandler.CheckResponse("google", resp) {
    // Downloads paused automatically
    // User will be notified via callback
}

// Check if currently rate limited before downloading
if rateLimitHandler.IsRateLimited("google") {
    return fmt.Errorf("downloads paused due to rate limit")
}

// Manual retry (called from UI)
rateLimitHandler.ManualRetry("google")
```

**Retry Schedule:**
| Attempt | Wait Time | When User Sees This |
|---------|-----------|---------------------|
| 1st detection | 5 minutes | "Rate limit detected. Auto-retry in 5 min" |
| 1st retry fails | 10 minutes | "Still limited. Auto-retry in 10 min" |
| 2nd retry fails | 15 minutes | "Still limited. Auto-retry in 15 min" |
| 3rd retry fails | 20 minutes | "Still limited. Auto-retry in 20 min" |
| 4th+ retries fail | 30 minutes | "Still limited. Consider waiting longer" |

### 2. Persistent Tile Cache

**File:** [`internal/cache/persistent_cache.go`](internal/cache/persistent_cache.go)

**Features:**
- OGC-compliant ZXY directory structure: `{provider}/{z}/{x}/{y}.jpg`
- Historical imagery support: `{provider}/{z}/{x}/{y}_{date}.jpg`
- Metadata index persists across app restarts (`cache_index.json`)
- LRU eviction when cache exceeds max size
- TTL-based expiration for old tiles
- Automatic metadata rebuild if index corrupted

**Cache Structure (OGC-Compliant):**
```
~/.walkthru-earth/imagery-desktop/cache/
├── cache_index.json                    # Metadata index
├── google/                              # Google Earth provider
│   ├── 2024-12-31/                     # Date as directory (OGC temporal standard)
│   │   ├── 15/                         # Zoom level 15
│   │   │   ├── 16384/                  # X coordinate
│   │   │   │   └── 8192.jpg            # Y coordinate
│   │   │   └── 16385/
│   │   │       └── 8193.jpg
│   │   └── 16/
│   │       └── ...
│   └── 2020-01-01/                     # Another date
│       └── 15/
│           └── ...
└── esri/                                # Esri provider
    ├── 2024-01-15/
    │   └── 15/
    │       └── ...
    └── 2023-06-30/
        └── 15/
            └── ...
```

**OGC Compliance Benefits:**
- ✅ Compatible with GeoServer, PyGeoAPI, QGIS, GDAL
- ✅ Standard temporal tile structure (date as directory)
- ✅ Can be served directly by OGC-compliant map servers
- ✅ GDAL can access via `/vsicurl/file:///path/cache/{provider}/{date}/{z}/{x}/{y}.jpg`
```

**Usage:**
```go
// Initialize cache (persists across app restarts)
cache, err := cache.NewPersistentTileCache(
    config.GetCachePath(settings),
    settings.CacheMaxSizeMB,
    settings.CacheTTLDays,
)

// Get tile from cache
if data, found := cache.Get("google:15:16384:8192:2024-12-31"); found {
    return data // Cache hit!
}

// Fetch from network and cache
data, err := fetchFromNetwork(...)
cache.Set("google", 15, 16384, 8192, "2024-12-31", data)

// Stats
entries, sizeBytes, maxBytes := cache.Stats()
log.Printf("Cache: %d tiles, %.2f MB / %.2f MB",
    entries, float64(sizeBytes)/1024/1024, float64(maxBytes)/1024/1024)
```

### 3. Configuration Updates

**File:** [`internal/config/settings.go`](internal/config/settings.go)

**New Settings:**
```go
type UserSettings struct {
    // ... existing fields ...

    // Cache settings
    CachePath      string `json:"cachePath"` // Custom location (empty = default)
    CacheMaxSizeMB int    `json:"cacheMaxSizeMB"` // Increased to 500MB
    CacheTTLDays   int    `json:"cacheTTLDays"`   // Increased to 90 days

    // Rate limit handling
    AutoRetryOnRateLimit bool `json:"autoRetryOnRateLimit"` // Default: true
}
```

**Default Values:**
- Cache Path: `~/.walkthru-earth/imagery-desktop/cache/` (user can override)
- Max Size: 500 MB (increased from 250 MB)
- TTL: 90 days (increased from 30 days)
- Auto Retry: Enabled

---

## Phase 2: Integration (PENDING ⏳)

### Tasks Remaining:

#### 1. Integrate Rate Limit Handler into App

**File to Modify:** `app.go`

**Changes Needed:**

a) Add rate limit handler to App struct:
```go
type App struct {
    // ... existing fields ...
    rateLimitHandler *ratelimit.Handler
}
```

b) Initialize in `NewApp()`:
```go
func NewApp() *App {
    // ... existing initialization ...

    rateLimitHandler := ratelimit.NewHandler(nil)

    // Set up callbacks
    rateLimitHandler.SetOnRateLimit(func(event ratelimit.RateLimitEvent) {
        app.emitRateLimitEvent(event)
    })

    rateLimitHandler.SetOnRetry(func(event ratelimit.RateLimitEvent) {
        app.emitRetryEvent(event)
    })

    rateLimitHandler.SetOnRecovered(func(provider string) {
        app.emitRecoveryEvent(provider)
    })

    return &App{
        // ... existing fields ...
        rateLimitHandler: rateLimitHandler,
    }
}
```

c) Check rate limits before downloads:
```go
// In DownloadGoogleEarthHistoricalImagery() and similar functions
if a.rateLimitHandler.IsRateLimited("google") {
    return fmt.Errorf("downloads paused due to rate limit - waiting for automatic retry")
}
```

d) Check responses for rate limiting:
```go
// After each HTTP request
if a.rateLimitHandler.CheckResponse("google", resp) {
    // Downloads automatically paused
    // User notified via event
    return fmt.Errorf("rate limit detected")
}
```

e) Add event emitters:
```go
func (a *App) emitRateLimitEvent(event ratelimit.RateLimitEvent) {
    wailsRuntime.EventsEmit(a.ctx, "rate-limit-detected", event)
}

func (a *App) emitRetryEvent(event ratelimit.RateLimitEvent) {
    wailsRuntime.EventsEmit(a.ctx, "rate-limit-retry", event)
}

func (a *App) emitRecoveryEvent(provider string) {
    wailsRuntime.EventsEmit(a.ctx, "rate-limit-cleared", provider)
}

// Wails-exported function for manual retry
func (a *App) ManualRetryRateLimit(provider string) {
    a.rateLimitHandler.ManualRetry(provider)
}
```

#### 2. Replace Old Cache with Persistent Cache

**File to Modify:** `app.go`

**Changes Needed:**

a) Update cache initialization in `NewApp()`:
```go
// OLD:
// tileCache, _ := cache.NewTileCache(cacheDir, settings.CacheMaxSizeMB)

// NEW:
cachePath := config.GetCachePath(settings)
tileCache, err := cache.NewPersistentTileCache(
    cachePath,
    settings.CacheMaxSizeMB,
    settings.CacheTTLDays,
)
if err != nil {
    log.Printf("Failed to initialize tile cache: %v", err)
    // Continue without cache
}
```

b) Update cache Get/Set calls to use new API:
```go
// OLD:
// cacheKey := fmt.Sprintf("tile:%s:%d:%d:%d", source, z, x, y)
// data, found := cache.Get(cacheKey)

// NEW (for current imagery):
data, found := cache.Get(fmt.Sprintf("%s:%d:%d:%d", provider, z, x, y))

// NEW (for historical imagery):
data, found := cache.Get(fmt.Sprintf("%s:%d:%d:%d:%s", provider, z, x, y, date))

// When setting:
cache.Set(provider, z, x, y, date, data)  // date="" for current imagery
```

#### 3. Add Frontend UI for Rate Limit Notifications

**File to Create:** `frontend/src/components/RateLimitNotification.tsx`

**Features Needed:**
- Toast/modal notification when rate limit detected
- Show countdown to next retry
- "Retry Now" button
- "Change IP and Retry" instruction
- Dismiss button
- Auto-dismiss when rate limit clears

**Example:**
```typescript
import { useEffect, useState } from 'react'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { ManualRetryRateLimit } from '../../wailsjs/go/main/App'

export function RateLimitNotification() {
    const [event, setEvent] = useState<RateLimitEvent | null>(null)
    const [countdown, setCountdown] = useState<string>('')

    useEffect(() => {
        // Listen for rate limit events
        EventsOn('rate-limit-detected', (event: RateLimitEvent) => {
            setEvent(event)
        })

        EventsOn('rate-limit-cleared', (provider: string) => {
            if (event?.provider === provider) {
                setEvent(null)
            }
        })
    }, [])

    useEffect(() => {
        if (!event) return

        const interval = setInterval(() => {
            const now = new Date().getTime()
            const nextRetry = new Date(event.nextRetryAt).getTime()
            const diff = nextRetry - now

            if (diff <= 0) {
                setCountdown('Retrying...')
            } else {
                const minutes = Math.floor(diff / 60000)
                const seconds = Math.floor((diff % 60000) / 1000)
                setCountdown(`${minutes}:${seconds.toString().padStart(2, '0')}`)
            }
        }, 1000)

        return () => clearInterval(interval)
    }, [event])

    if (!event) return null

    return (
        <div className="rate-limit-notification">
            <h3>⚠️ Rate Limit Detected</h3>
            <p>{event.message}</p>
            <p>Next retry in: {countdown}</p>
            <button onClick={() => ManualRetryRateLimit(event.provider)}>
                Retry Now
            </button>
        </div>
    )
}
```

#### 4. Add Settings UI for Cache Configuration

**File to Modify:** `frontend/src/components/SettingsDialog.tsx`

**Settings to Add:**
- Cache Location (text input with folder picker)
- Cache Size Limit (slider: 100MB - 2GB)
- Cache TTL (dropdown: 30/60/90/180 days / Never)
- Auto-Retry on Rate Limit (toggle)
- Clear Cache button (with confirmation)
- Cache stats display (X tiles, Y MB used)

---

## Phase 3: Testing (PENDING ⏳)

### Test Scenarios:

1. **Rate Limit Detection**
   - Trigger rate limit by downloading large area
   - Verify notification appears
   - Verify countdown timer works
   - Verify automatic retry after interval
   - Verify manual retry button works

2. **Cache Persistence**
   - Download tiles
   - Close app
   - Reopen app
   - Verify tiles load from cache (faster)
   - Check cache directory structure matches OGC ZXY

3. **Cache Eviction**
   - Fill cache to max size
   - Download more tiles
   - Verify oldest tiles are evicted
   - Verify cache stays under limit

4. **TTL Expiration**
   - Set TTL to 1 day
   - Wait 25 hours
   - Verify expired tiles are removed

5. **Custom Cache Location**
   - Change cache path in settings
   - Verify new location is used
   - Verify old cache is NOT deleted (user choice)

---

## File Summary

### New Files Created:
1. ✅ `internal/ratelimit/handler.go` - Rate limit detection and retry logic
2. ✅ `internal/cache/persistent_cache.go` - Persistent OGC ZXY cache
3. ✅ `RATE_LIMIT_AND_CACHE_IMPLEMENTATION.md` - This document

### Files Modified:
1. ✅ `internal/config/settings.go` - Added cache path and rate limit settings

### Files Pending Modification:
1. ⏳ `app.go` - Integrate rate limit handler and persistent cache
2. ⏳ `frontend/src/components/RateLimitNotification.tsx` - Create UI component
3. ⏳ `frontend/src/components/SettingsDialog.tsx` - Add cache settings UI
4. ⏳ `internal/googleearth/client.go` - Check rate limits in FetchTile()
5. ⏳ `internal/esri/client.go` - Check rate limits in FetchTile()

---

## Benefits

### For Users:
- ✅ **No lost work**: Cache persists across app restarts
- ✅ **Faster performance**: Reuse previously downloaded tiles
- ✅ **Clear communication**: Know exactly why downloads paused and when they'll resume
- ✅ **Control**: Manual retry option + automatic fallback
- ✅ **Disk space management**: Configurable cache size and TTL
- ✅ **Standard format**: OGC ZXY structure compatible with other tools

### For Developers:
- ✅ **Maintainable**: Clean separation of concerns
- ✅ **Testable**: Well-defined interfaces and callbacks
- ✅ **Observable**: Rich logging and event system
- ✅ **Extensible**: Easy to add new providers or retry strategies
- ✅ **Robust**: Handles corrupted metadata gracefully

---

## Next Steps

1. **Complete Integration** (Phase 2 tasks above)
2. **Add Frontend UI** for rate limit notifications
3. **Update Documentation** in ARCHITECTURE.md
4. **Test Thoroughly** with real rate limit scenarios
5. **Consider Future Enhancements**:
   - Predictive rate limit prevention (track request rate)
   - Cache warming (pre-download adjacent tiles)
   - Smart retry timing based on provider patterns
   - Cache sharing between multiple app instances
   - Export cache to standard map tile server format

---

## Questions to Consider

1. **Should we show a persistent banner** when rate limited, or just a dismissible toast?
2. **Should cache location migration** be automatic when user changes the path?
3. **Should we provide a "Download for Offline"** mode that respects rate limits but queues tiles?
4. **Should we implement cache compression** for older tiles to save space?
5. **Should we track and display** download speed/rate to help users avoid rate limits?

---

**Document Version:** 1.0
**Last Updated:** 2026-01-31
**Author:** Claude Code AI Assistant
