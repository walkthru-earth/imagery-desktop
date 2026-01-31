package taskqueue

import (
	"math"
)

// TileCoord represents a tile coordinate
type TileCoord struct {
	X, Y, Z int
}

// CalculateTilesForBBox calculates all tile coordinates needed for a bounding box at a given zoom level
func CalculateTilesForBBox(bbox BoundingBox, zoom int) []TileCoord {
	minX, minY := LatLonToTile(bbox.North, bbox.West, zoom)
	maxX, maxY := LatLonToTile(bbox.South, bbox.East, zoom)

	tiles := make([]TileCoord, 0)
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			tiles = append(tiles, TileCoord{X: x, Y: y, Z: zoom})
		}
	}

	return tiles
}

// CalculateTilesForCrop calculates the minimal tile set needed for a cropped output
// This is useful when video export has crop settings - we can skip downloading tiles
// that won't be visible in the final output
func CalculateTilesForCrop(bbox BoundingBox, zoom int, crop *CropPreview) []TileCoord {
	if crop == nil {
		return CalculateTilesForBBox(bbox, zoom)
	}

	// Calculate the geographic extent of the crop area
	latRange := bbox.North - bbox.South
	lonRange := bbox.East - bbox.West

	cropSouth := bbox.South + (1-crop.Y-crop.Height)*latRange
	cropNorth := bbox.South + (1-crop.Y)*latRange
	cropWest := bbox.West + crop.X*lonRange
	cropEast := bbox.West + (crop.X+crop.Width)*lonRange

	// Add a small buffer to ensure we get tiles at the edges
	buffer := 0.001 // ~100m buffer
	cropSouth -= buffer
	cropNorth += buffer
	cropWest -= buffer
	cropEast += buffer

	croppedBBox := BoundingBox{
		South: cropSouth,
		North: cropNorth,
		West:  cropWest,
		East:  cropEast,
	}

	return CalculateTilesForBBox(croppedBBox, zoom)
}

// LatLonToTile converts latitude/longitude to tile coordinates
func LatLonToTile(lat, lon float64, zoom int) (x, y int) {
	n := math.Pow(2, float64(zoom))
	x = int((lon + 180.0) / 360.0 * n)
	latRad := lat * math.Pi / 180.0
	y = int((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n)

	// Clamp to valid range
	maxTile := int(n) - 1
	if x < 0 {
		x = 0
	}
	if x > maxTile {
		x = maxTile
	}
	if y < 0 {
		y = 0
	}
	if y > maxTile {
		y = maxTile
	}

	return x, y
}

// TileToLatLon converts tile coordinates to latitude/longitude (returns center of tile)
func TileToLatLon(x, y, zoom int) (lat, lon float64) {
	n := math.Pow(2, float64(zoom))
	lon = float64(x)/n*360.0 - 180.0
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(y)/n)))
	lat = latRad * 180.0 / math.Pi
	return lat, lon
}

// EstimateTileCount estimates the number of tiles needed for a bbox at a given zoom
func EstimateTileCount(bbox BoundingBox, zoom int) int {
	minX, minY := LatLonToTile(bbox.North, bbox.West, zoom)
	maxX, maxY := LatLonToTile(bbox.South, bbox.East, zoom)

	width := maxX - minX + 1
	height := maxY - minY + 1

	return width * height
}

// EstimateDownloadSize estimates the download size in MB
// Based on average tile size of ~15KB for imagery tiles
func EstimateDownloadSize(tileCount int) float64 {
	avgTileSizeKB := 15.0
	return float64(tileCount) * avgTileSizeKB / 1024.0
}

// TileProgressCallback is called during tile operations to report progress
type TileProgressCallback func(completed, total int)

// BatchTiles groups tiles into batches for concurrent processing
func BatchTiles(tiles []TileCoord, batchSize int) [][]TileCoord {
	if batchSize <= 0 {
		batchSize = 10
	}

	batches := make([][]TileCoord, 0)
	for i := 0; i < len(tiles); i += batchSize {
		end := i + batchSize
		if end > len(tiles) {
			end = len(tiles)
		}
		batches = append(batches, tiles[i:end])
	}

	return batches
}
