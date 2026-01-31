package naming

import (
	"fmt"
	"math"
	"strings"
)

// GenerateQuadkey generates a quadkey string for a tile at zoom level z covering a bbox
// Uses the center tile as reference
func GenerateQuadkey(south, west, north, east float64, zoom int) string {
	centerLat := (south + north) / 2
	centerLon := (west + east) / 2

	// Convert to tile coordinates (Web Mercator)
	n := math.Pow(2, float64(zoom))
	x := int((centerLon + 180.0) / 360.0 * n)
	y := int((1.0 - math.Log(math.Tan(centerLat*math.Pi/180.0)+1.0/math.Cos(centerLat*math.Pi/180.0))/math.Pi) / 2.0 * n)

	// Generate quadkey from x, y, z
	var quadkey strings.Builder
	for i := zoom; i > 0; i-- {
		digit := 0
		mask := 1 << (i - 1)
		if (x & mask) != 0 {
			digit++
		}
		if (y & mask) != 0 {
			digit += 2
		}
		quadkey.WriteByte(byte('0' + digit))
	}
	return quadkey.String()
}

// GenerateBBoxString creates a human-readable bbox string for filenames
func GenerateBBoxString(south, west, north, east float64) string {
	return fmt.Sprintf("%.4f_%.4f_%.4f_%.4f", south, west, north, east)
}

// SanitizeCoordinate formats a coordinate for use in filenames (removes minus sign, uses N/S/E/W)
// Replaces decimal point with 'p' for Windows compatibility
func SanitizeCoordinate(coord float64, isLat bool) string {
	dir := "E"
	if isLat {
		if coord < 0 {
			dir = "S"
		} else {
			dir = "N"
		}
	} else {
		if coord < 0 {
			dir = "W"
		} else {
			dir = "E"
		}
	}
	// Format and replace decimal point with 'p'
	coordStr := fmt.Sprintf("%.4f", math.Abs(coord))
	coordStr = strings.Replace(coordStr, ".", "p", 1)
	return coordStr + dir
}
