package naming

import (
	"fmt"
)

// GenerateGeoTIFFFilename creates a standardized GeoTIFF filename with metadata
// Format: {source}_{date}_{quadkey}_z{zoom}_{bbox}.tif
func GenerateGeoTIFFFilename(source, date string, south, west, north, east float64, zoom int) string {
	quadkey := GenerateQuadkey(south, west, north, east, zoom)

	// Short bbox representation for filename
	bboxStr := fmt.Sprintf("%s-%s_%s-%s",
		SanitizeCoordinate(south, true),
		SanitizeCoordinate(north, true),
		SanitizeCoordinate(west, false),
		SanitizeCoordinate(east, false))

	return fmt.Sprintf("%s_%s_%s_z%d_%s.tif", source, date, quadkey, zoom, bboxStr)
}

// GenerateTilesDirName creates a standardized tiles directory name
// Format: {source}_{date}_z{zoom}_tiles
func GenerateTilesDirName(source, date string, zoom int) string {
	return fmt.Sprintf("%s_%s_z%d_tiles", source, date, zoom)
}
