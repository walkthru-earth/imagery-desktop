package esri

import (
	"fmt"
	"math"
)

// EsriTile represents a tile in Web Mercator projection (EPSG:3857)
type EsriTile struct {
	Level  int
	Row    int // Row from top (north)
	Column int
}

// GetRow implements common.Tile interface
func (t *EsriTile) GetRow() int {
	return t.Row
}

// GetColumn implements common.Tile interface
func (t *EsriTile) GetColumn() int {
	return t.Column
}

const (
	MaxLevel = 23
	// Web Mercator constants
	Equator    = 40075016.685578 // Earth's equator in meters
	EpsgNumber = 3857
)

// WebMercator represents coordinates in Web Mercator projection
type WebMercator struct {
	X float64 // meters east
	Y float64 // meters north
}

// Wgs84 represents WGS84 lat/lon coordinates
type Wgs84 struct {
	Lat float64
	Lon float64
}

// NewEsriTile creates a new Esri tile
func NewEsriTile(row, col, level int) (*EsriTile, error) {
	if level < 0 || level > MaxLevel {
		return nil, fmt.Errorf("level %d out of range [0, %d]", level, MaxLevel)
	}
	size := 1 << level
	if row < 0 || row >= size || col < 0 || col >= size {
		return nil, fmt.Errorf("row/col out of range for level %d", level)
	}
	return &EsriTile{Level: level, Row: row, Column: col}, nil
}

// NewEsriTileFromXYZ creates a tile from standard XYZ coordinates
// Note: Esri and standard XYZ both use Y from top, so no conversion needed
func NewEsriTileFromXYZ(x, y, z int) (*EsriTile, error) {
	return NewEsriTile(y, x, z)
}

// ToXYZ converts to standard XYZ coordinates
func (t *EsriTile) ToXYZ() (x, y, z int) {
	return t.Column, t.Row, t.Level
}

// toCoordinate converts tile position to Web Mercator coordinate
func (t *EsriTile) toCoordinate(column, row float64) WebMercator {
	n := float64(int(1) << t.Level)
	x := (column/n - 0.5) * Equator
	y := (0.5 - row/n) * Equator
	return WebMercator{X: x, Y: y}
}

// Center returns the center in Web Mercator
func (t *EsriTile) Center() WebMercator {
	return t.toCoordinate(float64(t.Column)+0.5, float64(t.Row)+0.5)
}

// Wgs84Center returns the center in WGS84
func (t *EsriTile) Wgs84Center() Wgs84 {
	return t.Center().ToWgs84()
}

// Bounds returns the bounding box in Web Mercator (minX, minY, maxX, maxY)
func (t *EsriTile) Bounds() (minX, minY, maxX, maxY float64) {
	ll := t.toCoordinate(float64(t.Column), float64(t.Row+1)) // lower-left
	ur := t.toCoordinate(float64(t.Column+1), float64(t.Row)) // upper-right
	return ll.X, ll.Y, ur.X, ur.Y
}

// Wgs84Bounds returns the bounding box in WGS84 (south, west, north, east)
func (t *EsriTile) Wgs84Bounds() (south, west, north, east float64) {
	minX, minY, maxX, maxY := t.Bounds()
	sw := WebMercator{X: minX, Y: minY}.ToWgs84()
	ne := WebMercator{X: maxX, Y: maxY}.ToWgs84()
	return sw.Lat, sw.Lon, ne.Lat, ne.Lon
}

// ToWgs84 converts Web Mercator to WGS84
func (m WebMercator) ToWgs84() Wgs84 {
	lon := m.X / Equator * 360.0
	lat := math.Atan(math.Sinh(m.Y/Equator*2*math.Pi)) * 180.0 / math.Pi
	return Wgs84{Lat: lat, Lon: lon}
}

// ToWebMercator converts WGS84 to Web Mercator
func (w Wgs84) ToWebMercator() WebMercator {
	x := w.Lon / 360.0 * Equator
	latRad := w.Lat * math.Pi / 180.0
	y := math.Log(math.Tan(math.Pi/4+latRad/2)) / (2 * math.Pi) * Equator
	return WebMercator{X: x, Y: y}
}

// GetTileForCoord returns the tile containing a Web Mercator coordinate at given level
func GetTileForCoord(coord WebMercator, level int) (*EsriTile, error) {
	size := 1 << level
	column := int((0.5 + coord.X/Equator) * float64(size))
	row := int((0.5 - coord.Y/Equator) * float64(size))

	// Clamp to valid range
	column = clamp(column, 0, size-1)
	row = clamp(row, 0, size-1)

	return NewEsriTile(row, column, level)
}

// GetTileForWgs84 returns the tile containing a WGS84 coordinate at given level
func GetTileForWgs84(lat, lon float64, level int) (*EsriTile, error) {
	coord := Wgs84{Lat: lat, Lon: lon}.ToWebMercator()
	return GetTileForCoord(coord, level)
}

// GetTilesInBounds returns all tiles within a WGS84 bounding box
func GetTilesInBounds(south, west, north, east float64, level int) ([]*EsriTile, error) {
	sw := Wgs84{Lat: south, Lon: west}.ToWebMercator()
	ne := Wgs84{Lat: north, Lon: east}.ToWebMercator()

	size := 1 << level

	minCol := int((0.5 + sw.X/Equator) * float64(size))
	maxCol := int((0.5 + ne.X/Equator) * float64(size))
	maxRow := int((0.5 - sw.Y/Equator) * float64(size)) // south = larger row
	minRow := int((0.5 - ne.Y/Equator) * float64(size)) // north = smaller row

	// Clamp to valid range
	minCol = clamp(minCol, 0, size-1)
	maxCol = clamp(maxCol, 0, size-1)
	minRow = clamp(minRow, 0, size-1)
	maxRow = clamp(maxRow, 0, size-1)

	var tiles []*EsriTile
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			tile, err := NewEsriTile(row, col, level)
			if err != nil {
				return nil, err
			}
			tiles = append(tiles, tile)
		}
	}

	return tiles, nil
}

// ResolutionAtZoom returns approximate meters per pixel at given zoom level
func ResolutionAtZoom(zoom int) float64 {
	// At zoom 0, the entire world (Equator meters) fits in 256 pixels
	return Equator / float64(int(256)<<zoom)
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

// TileToWebMercator converts tile column/row at a zoom level to Web Mercator coordinates
// Returns the top-left corner of the tile
func TileToWebMercator(col, row, zoom int) (x, y float64) {
	n := float64(int(1) << zoom)
	x = (float64(col)/n - 0.5) * Equator
	y = (0.5 - float64(row)/n) * Equator
	return x, y
}
