package common

import "fmt"

// DownloadFormat represents the output format options for imagery downloads
type DownloadFormat struct {
	SaveTiles   bool // Save individual tiles in OGC ZXY structure
	SaveGeoTIFF bool // Save merged GeoTIFF raster
}

// ParseDownloadFormat converts a format string to DownloadFormat struct
// Accepted values: "tiles", "geotiff", "both"
func ParseDownloadFormat(format string) (DownloadFormat, error) {
	switch format {
	case "tiles":
		return DownloadFormat{SaveTiles: true, SaveGeoTIFF: false}, nil
	case "geotiff":
		return DownloadFormat{SaveTiles: false, SaveGeoTIFF: true}, nil
	case "both":
		return DownloadFormat{SaveTiles: true, SaveGeoTIFF: true}, nil
	default:
		return DownloadFormat{}, fmt.Errorf("invalid format: %s (must be 'tiles', 'geotiff', or 'both')", format)
	}
}

// String returns the string representation of the download format
func (df DownloadFormat) String() string {
	if df.SaveTiles && df.SaveGeoTIFF {
		return "both"
	} else if df.SaveTiles {
		return "tiles"
	} else if df.SaveGeoTIFF {
		return "geotiff"
	}
	return "none"
}
