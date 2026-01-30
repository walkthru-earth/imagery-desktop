# Zoom Fallback Fix for Tile Downloads

## Problem

When attempting to download tiles at zoom level 19 (or other high zoom levels), many tiles don't exist on Google Earth servers, resulting in:
- Multiple 404 errors in the logs
- Empty or mostly empty GeoTIFF files being created
- Wasted bandwidth and time attempting to download non-existent tiles

## Root Cause

The tile server (for map preview) had zoom fallback logic, but the **download functions** did not. When a zoom 19 tile doesn't exist, the download would simply fail and continue with an empty tile, eventually creating a blank GeoTIFF.

## Solution Implemented

### 1. Smart Zoom Fallback Function

Added `fetchHistoricalGETileWithZoomFallback()` that:
- Tries the requested zoom level first
- If it fails, automatically tries lower zoom levels (z-1, z-2, z-3, etc.)
- Stops at zoom 10 minimum
- Uses more aggressive fallback for lower zooms (6 levels) vs high zooms (3 levels)
- Logs which zoom level succeeded

```go
// Tries zoom 19, then 18, then 17, etc. until it finds tiles
data, actualZoom, err := a.fetchHistoricalGETileWithZoomFallback(tile, hexDate, maxFallback)
```

### 2. Empty GeoTIFF Prevention

Added validation to prevent creating empty or mostly empty GeoTIFFs:
- Checks if **any** tiles succeeded (returns error if 0 tiles)
- Warns if success rate is below 30%
- Provides clear error messages about what failed

```go
if successCount == 0 {
    return fmt.Errorf("failed to download any tiles - all attempts failed at all zoom levels")
}
```

### 3. Enhanced Logging

- Logs when zoom fallback occurs
- Shows which zoom level succeeded
- Tracks success/failure rates
- Provides clear progress messages

## Technical Details

### Zoom Fallback Strategy

**High Zoom (z ≥ 17):**
- Max fallback: 3 levels (e.g., 19→18→17→16)
- Rationale: High zoom imagery is usually consistent within 3 levels

**Lower Zoom (z < 17):**
- Max fallback: 6 levels
- Rationale: Lower zoom levels may have sparser coverage

### Affected Functions

1. **`DownloadGoogleEarthHistoricalImagery`** - Historical imagery downloads
2. **`DownloadGoogleEarthImagery`** - Current imagery downloads (added validation)
3. **`fetchHistoricalGETileWithZoomFallback`** - New helper function

### How It Works

```
User requests zoom 19 download
↓
For each tile:
  1. Try zoom 19 → 404 error
  2. Try zoom 18 → 404 error
  3. Try zoom 17 → SUCCESS ✓
  4. Use zoom 17 tile data
↓
All tiles collected with fallback
↓
Validate: successCount > 0 and > 30% success rate
↓
Create GeoTIFF from collected tiles
```

## Benefits

1. **No Empty Files**: Never creates empty or useless GeoTIFFs
2. **Better Coverage**: Gets the best available imagery even if requested zoom doesn't exist
3. **Clearer Errors**: User knows exactly what went wrong
4. **Bandwidth Efficient**: Stops trying once a working zoom is found
5. **Maintains Quality**: Only falls back when necessary, tries requested zoom first

## Usage

No changes required from user perspective - the fallback happens automatically:

```javascript
// User requests zoom 19 download
DownloadGoogleEarthHistoricalImagery(bbox, 19, hexDate, epoch, date, "geotiff")

// Backend automatically:
// - Tries zoom 19, 18, 17 until tiles are found
// - Logs which zoom worked
// - Only creates GeoTIFF if tiles were successfully downloaded
```

## Example Log Output

```
[ZoomFallback] Tile 02002021313 at zoom 19 failed, trying fallback...
[ZoomFallback] Trying zoom 18 (tile: 0200202131)...
[ZoomFallback] SUCCESS at zoom 18
[GEHistorical] Tile 02002021313 downloaded from zoom 18 (requested 19)
[GEHistorical] Processed 247/256 tiles
Warning: Only 247/256 tiles downloaded - GeoTIFF may have gaps
```

## Testing

To verify the fix:

1. Request a zoom 19 download in an area with sparse coverage
2. Check logs for `[ZoomFallback]` messages
3. Verify GeoTIFF is created with actual imagery (not blank)
4. Confirm success rate is logged

## Configuration

Current settings:
- Minimum zoom: 10
- High zoom max fallback: 3 levels
- Low zoom max fallback: 6 levels
- Minimum success rate: 30%

These can be adjusted in [app.go](app.go) if needed.
