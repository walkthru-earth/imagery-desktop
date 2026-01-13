package googleearth

import (
	"fmt"
	"image"
	"math"
)

// Tile represents a Google Earth tile using quadtree path
type Tile struct {
	Path   string
	Level  int
	Row    int
	Column int
}

const MaxLevel = 30

// NewTileFromPath creates a Tile from a quadtree path string
func NewTileFromPath(path string) (*Tile, error) {
	if len(path) == 0 || path[0] != '0' {
		return nil, fmt.Errorf("invalid quadtree path: must start with '0'")
	}

	t := &Tile{
		Path:  path,
		Level: len(path) - 1,
	}

	// Convert quadtree path to row/column
	// Quadrant layout:
	// |-----|-----|
	// |  3  |  2  |
	// |-----|-----|
	// |  0  |  1  |
	// |-----|-----|
	for i := 0; i < len(path); i++ {
		cell := int(path[i] - '0')
		if cell < 0 || cell > 3 {
			return nil, fmt.Errorf("invalid quadtree path character: %c", path[i])
		}
		row := cell >> 1
		col := row ^ (cell & 1)

		t.Row = (t.Row << 1) | row
		t.Column = (t.Column << 1) | col
	}

	return t, nil
}

// NewTileFromRowCol creates a Tile from row, column and level
func NewTileFromRowCol(row, col, level int) (*Tile, error) {
	numTiles := 1 << level
	if row < 0 || row >= numTiles || col < 0 || col >= numTiles {
		return nil, fmt.Errorf("row/col out of range for level %d", level)
	}
	if level > MaxLevel {
		return nil, fmt.Errorf("level exceeds maximum: %d", MaxLevel)
	}

	// Build path from row/col
	chars := make([]byte, level+1)
	r, c := row, col
	for i := level; i >= 0; i-- {
		rowBit := r & 1
		colBit := c & 1
		r >>= 1
		c >>= 1
		chars[i] = byte((rowBit << 1) | (rowBit ^ colBit) + '0')
	}

	return &Tile{
		Path:   string(chars),
		Level:  level,
		Row:    row,
		Column: col,
	}, nil
}

// NewTileFromXYZ converts standard Web Mercator XYZ tile coordinates to a KeyholeTile
// MapLibre/OSM uses Web Mercator (EPSG:3857), Google Earth uses Plate Carrée (EPSG:4326)
// We convert the XYZ tile center from Web Mercator to lat/lon, then find the GE tile
func NewTileFromXYZ(x, y, z int) (*Tile, error) {
	// Get the center of the Web Mercator XYZ tile in lat/lon
	lat, lon := xyzTileToLatLon(x, y, z)

	// Find the Google Earth tile at this lat/lon
	return GetTileForCoord(lat, lon, z)
}

// xyzTileToLatLon converts Web Mercator XYZ tile coordinates to lat/lon (center of tile)
func xyzTileToLatLon(x, y, z int) (lat, lon float64) {
	n := float64(int(1) << z)

	// Tile center in normalized coordinates (0-1)
	tileX := (float64(x) + 0.5) / n
	tileY := (float64(y) + 0.5) / n

	// Convert to longitude (-180 to 180)
	lon = tileX*360.0 - 180.0

	// Convert to latitude using inverse Web Mercator formula
	// Web Mercator Y goes from 0 (top/north) to 1 (bottom/south)
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2*tileY)))
	lat = latRad * 180.0 / math.Pi

	return lat, lon
}

// ToXYZ converts the tile to standard XYZ coordinates
func (t *Tile) ToXYZ() (x, y, z int) {
	numTiles := 1 << t.Level
	z = t.Level
	x = t.Column
	y = numTiles - 1 - t.Row
	return
}

// Center returns the center lat/lon of the tile
func (t *Tile) Center() (lat, lon float64) {
	lat = t.rowColToLatLon(float64(t.Row) + 0.5)
	lon = t.rowColToLatLon(float64(t.Column) + 0.5)
	return
}

// Bounds returns the bounding box (south, west, north, east)
func (t *Tile) Bounds() (south, west, north, east float64) {
	south = t.rowColToLatLon(float64(t.Row))
	west = t.rowColToLatLon(float64(t.Column))
	north = t.rowColToLatLon(float64(t.Row + 1))
	east = t.rowColToLatLon(float64(t.Column + 1))
	return
}

func (t *Tile) rowColToLatLon(rowCol float64) float64 {
	numTiles := float64(int(1) << t.Level)
	return (rowCol/numTiles)*360.0 - 180.0
}

// GetTileForCoord returns the tile containing a lat/lon at a given zoom level
// Google Earth uses a Plate Carrée projection where the world is mapped to a square:
// - Longitude: -180 to +180 maps to columns 0 to numTiles-1
// - Latitude: -180 to +180 maps to rows 0 to numTiles-1 (not -90 to +90!)
// This means actual geographic content (-90 to +90 lat) only covers the middle half of the row range.
func GetTileForCoord(lat, lon float64, level int) (*Tile, error) {
	numTiles := 1 << level

	// Convert lat/lon to row/col
	// GE Plate Carrée: both dimensions span -180 to +180
	// Latitude (-90 to +90) maps to the middle half of the grid
	row := int((lat + 180.0) / 360.0 * float64(numTiles))
	col := int((lon + 180.0) / 360.0 * float64(numTiles))

	// Clamp to valid range
	row = clamp(row, 0, numTiles-1)
	col = clamp(col, 0, numTiles-1)

	return NewTileFromRowCol(row, col, level)
}

// GetTilesInBounds returns all tiles within a bounding box at a given zoom level
func GetTilesInBounds(south, west, north, east float64, level int) ([]*Tile, error) {
	numTiles := 1 << level

	// Convert bounds to row/col range
	minRow := int((south + 180.0) / 360.0 * float64(numTiles))
	maxRow := int((north + 180.0) / 360.0 * float64(numTiles))
	minCol := int((west + 180.0) / 360.0 * float64(numTiles))
	maxCol := int((east + 180.0) / 360.0 * float64(numTiles))

	// Clamp to valid range
	minRow = clamp(minRow, 0, numTiles-1)
	maxRow = clamp(maxRow, 0, numTiles-1)
	minCol = clamp(minCol, 0, numTiles-1)
	maxCol = clamp(maxCol, 0, numTiles-1)

	var tiles []*Tile
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			tile, err := NewTileFromRowCol(row, col, level)
			if err != nil {
				return nil, err
			}
			tiles = append(tiles, tile)
		}
	}

	return tiles, nil
}

// ResolutionAtZoom returns approximate meters per pixel at given zoom level
func ResolutionAtZoom(zoom int, lat float64) float64 {
	// Earth circumference at equator ≈ 40,075,016.686 meters
	// Tile size = 256 pixels
	earthCircumference := 40075016.686
	return earthCircumference * math.Cos(lat*math.Pi/180) / float64(int(256)<<zoom)
}

// Equator is the circumference of the Earth in Web Mercator coordinates
const Equator = 40075016.686

// TileToWebMercator converts tile row/col at a zoom level to Web Mercator coordinates
// Returns the top-left corner of the tile (in EPSG:3857)
// IMPORTANT: GE tiles are in Plate Carrée, so we must convert via lat/lon
func TileToWebMercator(row, col, zoom int) (x, y float64) {
	numTiles := float64(int(1) << zoom)

	// Step 1: Convert GE row/col to lat/lon (Plate Carrée - linear mapping)
	// Row increases from south to north in GE coordinate system
	lat := (float64(row)/numTiles)*360.0 - 180.0
	lon := (float64(col)/numTiles)*360.0 - 180.0

	// Step 2: Convert lat/lon to Web Mercator (EPSG:3857)
	x = lon * Equator / 360.0

	// Clamp latitude to valid Web Mercator range (avoid infinity at poles)
	if lat > 85.051129 {
		lat = 85.051129
	} else if lat < -85.051129 {
		lat = -85.051129
	}

	// Web Mercator Y formula: R * ln(tan(π/4 + φ/2))
	// Simplified: y = R * ln((1 + sin(φ)) / (1 - sin(φ))) / 2
	latRad := lat * math.Pi / 180.0
	y = Equator * math.Log(math.Tan(math.Pi/4+latRad/2)) / (2 * math.Pi)

	return x, y
}

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// SubIndex calculation constants
const SubIndexMaxSz = 4

// GetSubIndex returns the sub-index for a quadtree path
// This mirrors KeyholeTile.GetSubIndex from the C# reference
func GetSubIndex(path string) int {
	if len(path) <= SubIndexMaxSz {
		return GetRootSubIndex(path)
	}
	// "getSubindexPath": quadTreePath.Substring((quadTreePath.Length - 1) / SUBINDEX_MAX_SZ * SUBINDEX_MAX_SZ)
	startIndex := (len(path) - 1) / SubIndexMaxSz * SubIndexMaxSz
	subIndexPath := path[startIndex:]
	return GetTreeSubIndex(subIndexPath)
}

// GetRootSubIndex calculates sub-index for root nodes
func GetRootSubIndex(path string) int {
	subIndex := 0
	for i := 1; i < len(path); i++ {
		subIndex *= SubIndexMaxSz
		subIndex += int(path[i]-'0') + 1
	}
	return subIndex
}

// GetTreeSubIndex calculates sub-index for non-root nodes
func GetTreeSubIndex(path string) int {
	// (quadTreePath[0] - 0x30) * 85 + 1 + GetRootSubIndex(path)
	// 0x30 is '0'
	return GetRootSubIndex(path) + (int(path[0]-'0') * 85) + 1
}

// ParentPath returns the path of the parent node (or sub-root for traversal)
// For traversal, we need the path corresponding to the packet we are in.
// If we are at depth > 4, we need the path rounded down to nearest multiple of 4.
func (t *Tile) PacketPath() string {
	if len(t.Path) <= SubIndexMaxSz {
		return "0" // Root packet
	}
	// Round down to nearest multiple of 4 (excluding the last partial block if it matches traversal logic? No.)
	// Based on GetSubIndex logic:
	// The packet covering a tile at subIndexPath starts at path[0:startIndex]
	startIndex := (len(t.Path) - 1) / SubIndexMaxSz * SubIndexMaxSz
	return t.Path[:startIndex]
}

// TraversalPath returns the indices required to traverse from root to this tile
func (t *Tile) TraversalPaths() []string {
	var paths []string
	// Logic from KeyholeTile.EnumerateIndices
	// for (int end = SUBINDEX_MAX_SZ; end < Path.Length; end += SUBINDEX_MAX_SZ)
	for end := SubIndexMaxSz; end < len(t.Path); end += SubIndexMaxSz {
		paths = append(paths, t.Path[:end])
	}
	return paths
}

// WebMercatorTileBounds returns the geographic bounds (lat/lon) for a Web Mercator XYZ tile
// This is what MapLibre expects each tile to cover
func WebMercatorTileBounds(x, y, z int) (south, west, north, east float64) {
	n := float64(int(1) << z)

	// Convert tile edges to normalized coordinates (0-1)
	west = (float64(x) / n) * 360.0 - 180.0
	east = (float64(x+1) / n) * 360.0 - 180.0

	// Web Mercator Y: 0 at top (north), increases going south
	// Convert using inverse Mercator formula
	northY := float64(y) / n
	southY := float64(y+1) / n

	// Inverse Web Mercator: lat = atan(sinh(π * (1 - 2*y)))
	north = math.Atan(math.Sinh(math.Pi*(1-2*northY))) * 180.0 / math.Pi
	south = math.Atan(math.Sinh(math.Pi*(1-2*southY))) * 180.0 / math.Pi

	return south, west, north, east
}

// PixelToLatLon converts a pixel position within a Web Mercator tile to lat/lon
// px and py are pixel coordinates (0-255), tileSize is typically 256
func PixelToLatLon(x, y, z, px, py, tileSize int) (lat, lon float64) {
	n := float64(int(1) << z)

	// Normalized position within the tile (0-1)
	fracX := (float64(px) + 0.5) / float64(tileSize)
	fracY := (float64(py) + 0.5) / float64(tileSize)

	// Global normalized position
	globalX := (float64(x) + fracX) / n
	globalY := (float64(y) + fracY) / n

	// Convert to lon/lat
	lon = globalX*360.0 - 180.0
	lat = math.Atan(math.Sinh(math.Pi*(1-2*globalY))) * 180.0 / math.Pi

	return lat, lon
}

// LatLonToGETilePixel converts a lat/lon to GE tile row/col and pixel position within that tile
// Returns the tile and the pixel coordinates (0-255) within it
func LatLonToGETilePixel(lat, lon float64, level, tileSize int) (row, col, px, py int) {
	numTiles := float64(int(1) << level)

	// GE uses Plate Carrée: both lat and lon map linearly to -180..+180
	// Note: GE row increases from south (-180) to north (+180)
	rowF := (lat + 180.0) / 360.0 * numTiles
	colF := (lon + 180.0) / 360.0 * numTiles

	// Tile indices
	row = int(rowF)
	col = int(colF)

	// Clamp to valid range
	if row < 0 {
		row = 0
	} else if row >= int(numTiles) {
		row = int(numTiles) - 1
	}
	if col < 0 {
		col = 0
	} else if col >= int(numTiles) {
		col = int(numTiles) - 1
	}

	// Pixel position within tile
	px = int((colF - float64(col)) * float64(tileSize))
	py = int((1 - (rowF - float64(row))) * float64(tileSize)) // Invert Y since image Y=0 is at top

	// Clamp pixels
	if px < 0 {
		px = 0
	} else if px >= tileSize {
		px = tileSize - 1
	}
	if py < 0 {
		py = 0
	} else if py >= tileSize {
		py = tileSize - 1
	}

	return row, col, px, py
}

// TileCoord represents a GE tile coordinate (row, col at a level)
type TileCoord struct {
	Row    int
	Column int
	Level  int
}

// GetRequiredGETiles returns all GE tiles needed to cover a Web Mercator XYZ tile
func GetRequiredGETiles(x, y, z int) []TileCoord {
	south, west, north, east := WebMercatorTileBounds(x, y, z)

	numTiles := float64(int(1) << z)

	// Convert bounds to GE row/col range
	minRow := int((south + 180.0) / 360.0 * numTiles)
	maxRow := int((north + 180.0) / 360.0 * numTiles)
	minCol := int((west + 180.0) / 360.0 * numTiles)
	maxCol := int((east + 180.0) / 360.0 * numTiles)

	// Clamp
	maxTile := int(numTiles) - 1
	minRow = clamp(minRow, 0, maxTile)
	maxRow = clamp(maxRow, 0, maxTile)
	minCol = clamp(minCol, 0, maxTile)
	maxCol = clamp(maxCol, 0, maxTile)

	var tiles []TileCoord
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			tiles = append(tiles, TileCoord{Row: row, Column: col, Level: z})
		}
	}

	return tiles
}

// ReprojectToWebMercator creates a Web Mercator tile by sampling from GE tile images
// geTiles is a map from "row,col" to the decoded image for that GE tile
// x, y, z are the Web Mercator tile coordinates
// tileSize is typically 256
func ReprojectToWebMercator(geTiles map[string]image.Image, x, y, z, tileSize int) *image.RGBA {
	output := image.NewRGBA(image.Rect(0, 0, tileSize, tileSize))

	for py := 0; py < tileSize; py++ {
		for px := 0; px < tileSize; px++ {
			// Get lat/lon for this output pixel
			lat, lon := PixelToLatLon(x, y, z, px, py, tileSize)

			// Find which GE tile and pixel this corresponds to
			geRow, geCol, gePx, gePy := LatLonToGETilePixel(lat, lon, z, tileSize)

			// Look up the source tile
			key := fmt.Sprintf("%d,%d", geRow, geCol)
			srcImg, ok := geTiles[key]
			if !ok {
				// No tile available, leave transparent
				continue
			}

			// Sample the source pixel
			c := srcImg.At(gePx, gePy)
			output.Set(px, py, c)
		}
	}

	return output
}
