# Export Format Specification

## Overview

This document describes the standardized export formats for imagery downloads, following OGC tile naming conventions and including comprehensive metadata.

## Directory Structure

### OGC-Compliant Tile Organization

All downloaded tiles follow the OGC/OSM tile naming convention with source and date organization:

```
downloads/
├── {source}_{date}_z{zoom}_tiles/
│   └── {source}/
│       └── {date}/
│           └── {z}/
│               └── {x}/
│                   └── {y}.jpg
└── {source}_{date}_{quadkey}_z{zoom}_{bbox}.tif
```

### Example Structure

```
downloads/
├── esri_2024-01-15_z18_tiles/
│   └── esri/
│       └── 2024-01-15/
│           └── 18/
│               ├── 123456/
│               │   ├── 789012.jpg
│               │   ├── 789013.jpg
│               │   └── 789014.jpg
│               └── 123457/
│                   ├── 789012.jpg
│                   └── 789013.jpg
│
├── ge_historical_2020-06-10_z19_tiles/
│   └── google_earth_historical/
│       └── 2020-06-10/
│           └── 19/
│               └── 308257/
│                   ├── 216105.jpg
│                   └── 216106.jpg
│
└── esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif
```

## File Naming Conventions

### GeoTIFF Filenames

Format: `{source}_{date}_{quadkey}_z{zoom}_{bbox}.tif`

**Components:**
- **source**: Data source identifier
  - `esri` - Esri Wayback imagery
  - `ge` - Google Earth current imagery
  - `ge_historical` - Google Earth historical imagery

- **date**: ISO 8601 date format (`YYYY-MM-DD`)
  - Example: `2024-01-15`, `2020-06-10`

- **quadkey**: Bing Maps quadkey for the center tile
  - Hierarchical tile identifier (e.g., `0313131323`)
  - Encodes zoom, x, and y coordinates
  - Allows quick geographic lookup

- **zoom**: Zoom level (10-19)
  - Higher = more detailed

- **bbox**: Bounding box in cardinal notation
  - Format: `{south}-{north}_{west}-{east}`
  - Example: `45.2345N-45.5678N_122.1234W-122.5678W`
  - Uses N/S/E/W suffixes for clarity

**Examples:**
```
esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif
ge_2026-01-30_0200202131_z17_31.2000N-31.3000N_30.0000E-30.1000E.tif
ge_historical_2020-06-10_0313131320_z19_45.2340N-45.2360N_122.1230W-122.1250W.tif
```

### Tile Directory Names

Format: `{source}_{date}_z{zoom}_tiles`

**Examples:**
```
esri_2024-01-15_z18_tiles/
ge_2026-01-30_z17_tiles/
ge_historical_2020-06-10_z19_tiles/
```

### Individual Tile Paths

Format: `{source}/{date}/{z}/{x}/{y}.jpg`

This follows the standard Web Mercator / XYZ tile scheme:
- **z**: Zoom level
- **x**: Column (longitude axis, increases eastward)
- **y**: Row (latitude axis, increases southward in Web Mercator)

**Examples:**
```
esri/2024-01-15/18/123456/789012.jpg
google_earth/2026-01-30/17/65536/43210.jpg
google_earth_historical/2020-06-10/19/308257/216105.jpg
```

## GeoTIFF Metadata

### Embedded TIFF Tags

All GeoTIFFs include standard geospatial metadata tags:

**GeoKey Directory (Tag 34735):**
```
Version: 1
Revision: 1
Keys: 3
  - GTModelTypeGeoKey (1024) = 1 (Projected CRS)
  - GTRasterTypeGeoKey (1025) = 1 (PixelIsArea)
  - ProjectedCSTypeGeoKey (3072) = 3857 (EPSG:3857 - Web Mercator)
```

**Model Pixel Scale (Tag 33550):**
```
[ScaleX, ScaleY, ScaleZ]
- ScaleX: Pixel width in meters
- ScaleY: Pixel height in meters (absolute value)
- ScaleZ: 0.0
```

**Model Tiepoint (Tag 33922):**
```
[I, J, K, X, Y, Z]
- I, J, K: Raster pixel coordinates (0, 0, 0)
- X, Y, Z: Model coordinates (originX, originY, 0)
- Ties pixel (0,0) to real-world coordinate
```

### Metadata Sidecar File

Each GeoTIFF has an accompanying `.tif.aux.xml` file with additional metadata:

```xml
<PAMDataset>
  <Metadata domain="IMAGE_STRUCTURE">
    <MDI key="COMPRESSION">NONE</MDI>
    <MDI key="INTERLEAVE">PIXEL</MDI>
  </Metadata>
  <Metadata domain="">
    <MDI key="Source">Esri Wayback</MDI>
    <MDI key="Date">2024-01-15</MDI>
    <MDI key="CRS">EPSG:3857</MDI>
    <MDI key="Generated_By">WalkThru Earth Imagery Desktop v1.0.0</MDI>
  </Metadata>
</PAMDataset>
```

## Coordinate Reference System

All exports use **EPSG:3857** (WGS 84 / Pseudo-Mercator):

- **Also known as:** Web Mercator, Google Maps Mercator
- **Units:** Meters
- **Bounds:**
  - Longitude: -180° to +180°
  - Latitude: -85.051129° to +85.051129°
- **Properties:**
  - Not conformal at high latitudes
  - Designed for web mapping
  - Compatible with all major web mapping libraries

## Zoom Levels and Resolution

Resolution (meters per pixel at equator):

| Zoom | Resolution | Tile Coverage |
|------|-----------|---------------|
| 10 | ~152.87m | ~39,135 km |
| 12 | ~38.22m | ~9,784 km |
| 14 | ~9.55m | ~2,446 km |
| 16 | ~2.39m | ~611 km |
| 18 | ~0.60m | ~153 km |
| 19 | ~0.30m | ~76 km |

## Tile Format

**Image Format:** JPEG
- **Compression:** Lossy JPEG compression
- **Quality:** 90%
- **Color Space:** RGB
- **Dimensions:** 256x256 pixels per tile

**GeoTIFF Format:** Uncompressed RGBA TIFF
- **Compression:** None (for maximum compatibility)
- **Color Space:** RGBA (8 bits per channel)
- **Pixel Type:** PixelIsArea
- **Encoding:** Little-endian

## Usage Examples

### Loading in QGIS

1. **GeoTIFF:**
   ```
   Layer → Add Layer → Add Raster Layer
   Select: esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif
   ```

2. **Tile Directory (as XYZ layer):**
   ```
   Layer → Add Layer → Add XYZ Tiles
   URL: file:///path/to/downloads/esri_2024-01-15_z18_tiles/esri/2024-01-15/{z}/{x}/{y}.jpg
   Min zoom: 18
   Max zoom: 18
   ```

### Loading in ArcGIS

1. **GeoTIFF:**
   ```
   Add Data → Raster Dataset
   Navigate to .tif file
   ```

2. **Verify CRS:**
   ```
   Right-click layer → Properties → Source
   Coordinate System: WGS_1984_Web_Mercator_Auxiliary_Sphere (EPSG:3857)
   ```

### Using with GDAL

```bash
# Get GeoTIFF info
gdalinfo esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif

# Convert to different format
gdal_translate -of GTiff -co COMPRESS=LZW \
  esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif \
  output_compressed.tif

# Reproject to WGS84
gdalwarp -t_srs EPSG:4326 \
  esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif \
  output_wgs84.tif

# Build tile pyramid
gdal_translate -of COG -co COMPRESS=JPEG -co QUALITY=90 \
  esri_2024-01-15_0313131323_z18_45.2345N-45.5678N_122.1234W-122.5678W.tif \
  output_cog.tif
```

### Serving Tiles with Web Server

The OGC tile structure can be served directly:

**nginx configuration:**
```nginx
location /tiles/ {
    alias /path/to/downloads/;
    autoindex on;

    # Enable CORS for web mapping
    add_header Access-Control-Allow-Origin *;
    add_header Access-Control-Allow-Methods 'GET, OPTIONS';
}
```

**Access tiles:**
```
http://localhost/tiles/esri_2024-01-15_z18_tiles/esri/2024-01-15/18/123456/789012.jpg
```

**MapLibre GL JS:**
```javascript
map.addSource('custom-tiles', {
  type: 'raster',
  tiles: ['http://localhost/tiles/esri_2024-01-15_z18_tiles/esri/2024-01-15/{z}/{x}/{y}.jpg'],
  tileSize: 256,
  minzoom: 18,
  maxzoom: 18
});
```

## Quadkey Reference

Quadkeys encode tile coordinates in a hierarchical string:

```
Zoom 0: Single tile (root)
Quadrant layout per zoom level:
+-----+-----+
|  0  |  1  |
+-----+-----+
|  2  |  3  |
+-----+-----+
```

**Example:** Quadkey `0313131323` at zoom 10
- Read left to right: 0-3-1-3-1-3-1-3-2-3
- Each digit represents a quadrant at that zoom level
- Allows hierarchical tile organization

**Converting quadkey to tile coordinates:**
```python
def quadkey_to_tile(quadkey):
    x, y = 0, 0
    z = len(quadkey)
    for i, digit in enumerate(quadkey):
        mask = 1 << (z - i - 1)
        if digit == '1' or digit == '3':
            x |= mask
        if digit == '2' or digit == '3':
            y |= mask
    return x, y, z
```

## Version History

- **v1.0** (2026-01-30): Initial standardized export format
  - OGC-compliant tile naming
  - Quadkey-based GeoTIFF filenames
  - Source/date organization for tiles
  - Enhanced GeoTIFF metadata
  - Zoom fallback support

## Future Enhancements

Planned improvements:
1. Cloud-Optimized GeoTIFF (COG) format option
2. Compressed tile formats (WebP, PNG)
3. Multiple CRS support (EPSG:4326, custom)
4. MBTiles archive format for mobile apps
5. Metadata JSON manifests for batches
6. Tile pyramid generation for multi-zoom
