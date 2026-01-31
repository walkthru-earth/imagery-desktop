package common

// TileDownloadResult represents the result of downloading a single tile
// This unified type replaces multiple scattered tileResult structs throughout the codebase
type TileDownloadResult struct {
	// Tile holds the tile reference (can be *esri.EsriTile or *googleearth.Tile)
	Tile interface{}

	// Data contains the raw tile image data (usually JPEG bytes)
	Data []byte

	// Success indicates whether the download succeeded
	Success bool

	// Error contains any error that occurred during download
	Error error

	// Index preserves the original order for async operations
	Index int
}
