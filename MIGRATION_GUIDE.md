# Migration Guide - Refactor 1

## Breaking Changes

This refactoring standardizes naming conventions across the entire application. Users with existing cached tiles will need to clear their cache.

### Cache Structure Changes

**Old cache structure:**
```
~/.walkthru-earth/imagery-desktop/cache/
├── google/           ← Old provider name
│   └── 2024-12-31/
└── esri/             ← Old provider name
    └── 2024-01-15/
```

**New cache structure:**
```
~/.walkthru-earth/imagery-desktop/cache/
├── google_earth/     ← New provider name (matches constants)
│   └── 2024-12-31/
└── esri_wayback/     ← New provider name (matches constants)
    └── 2024-01-15/
```

### URL Path Changes

**Google Earth tile URLs:**
- Old: `http://localhost:PORT/ge/{date}/{z}/{x}/{y}`
- New: `http://localhost:PORT/google-earth/{date}/{z}/{x}/{y}`

**Google Earth Historical tile URLs:**
- Old: `http://localhost:PORT/ge-historical/{date}_{hexDate}/{z}/{x}/{y}`
- New: `http://localhost:PORT/google-earth-historical/{date}_{hexDate}/{z}/{x}/{y}`

###Backend API Changes

**Function Renames:**
- `GetEsriLayers()` → `GetEsriWaybackDatesForArea(bbox, zoom)`

**TypeScript/Frontend Changes:**
- `ImagerySource` type: `'google' | 'esri'` → `'google_earth' | 'esri_wayback'`
- Auto-generated Wails bindings will reflect new function names

## Migration Steps for Users

### 1. Clear Old Cache

Before updating to this version, clear your old cache:

```bash
rm -rf ~/.walkthru-earth/imagery-desktop/cache/google
rm -rf ~/.walkthru-earth/imagery-desktop/cache/esri
rm -f ~/.walkthru-earth/imagery-desktop/cache/cache_index.json
```

The application will automatically rebuild the cache with the new structure.

### 2. No Frontend Changes Required

The frontend will be automatically updated via Wails binding regeneration. No manual configuration needed.

### 3. Custom Integrations

If you have custom code that:
- Directly accesses the cache directory
- Makes HTTP requests to the tile server
- Uses the Wails bindings directly

You'll need to update provider strings and URL paths to match the new naming.

## Benefits of This Refactoring

1. **Consistent Naming**: All provider references now use the same convention
2. **Self-Documenting URLs**: `/google-earth/` is clearer than `/ge/`
3. **Code Maintainability**: Centralized constants prevent typos and inconsistencies
4. **Future-Proof**: Easy to add new providers with consistent patterns
5. **Bug Fixes**: Fixed critical decryption bug in Google Earth client
6. **Code Deduplication**: Removed ~300 lines of duplicated code

## Rollback

If you need to rollback to the previous version:

1. Checkout the previous commit before this refactoring
2. Clear the new cache structure
3. Rebuild from the old code

The cache is forward-compatible within this branch (old cache won't be read, but won't cause errors).
