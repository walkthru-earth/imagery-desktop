package imagery

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"bytes"
	"sync"
	"sync/atomic"

	"imagery-desktop/internal/cache"
)

// TileDownloader provides unified tile download and stitching logic
type TileDownloader struct {
	workers int
	cache   *cache.PersistentTileCache
}

// NewTileDownloader creates a new tile downloader
func NewTileDownloader(workers int, cache *cache.PersistentTileCache) *TileDownloader {
	return &TileDownloader{
		workers: workers,
		cache:   cache,
	}
}

// Tile represents a generic tile with position information
type Tile interface {
	GetColumn() int
	GetRow() int
}

// TileFetcher defines the interface for fetching tile data
type TileFetcher interface {
	FetchTile(tile Tile) ([]byte, error)
	GetCacheKey(tile Tile) string
}

// Bounds represents geographic bounds in Web Mercator
type Bounds struct {
	MinX, MinY, MaxX, MaxY float64
}

// TileResult represents a downloaded tile
type tileResult struct {
	tile    Tile
	data    []byte
	success bool
}

// DownloadAndStitch downloads tiles using a worker pool and stitches them into a single image
// This unified function eliminates duplication across Esri, Google Earth current, and Google Earth historical
func (d *TileDownloader) DownloadAndStitch(
	tiles []Tile,
	fetcher TileFetcher,
	onProgress func(current, total int),
) (image.Image, int, int, int, int, error) {
	if len(tiles) == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("no tiles to download")
	}

	total := len(tiles)
	var downloaded int64

	// Channels for worker pool
	tileChan := make(chan Tile, total)
	resultChan := make(chan tileResult, total)

	// Determine worker count (min of workers setting and total tiles)
	workerCount := d.workers
	if total < workerCount {
		workerCount = total
	}

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tile := range tileChan {
				// Fetch tile data
				// Note: Caching is handled in app.go at a higher level
				data, err := fetcher.FetchTile(tile)
				if err != nil {
					resultChan <- tileResult{tile: tile, success: false}
					atomic.AddInt64(&downloaded, 1)
					if onProgress != nil {
						onProgress(int(atomic.LoadInt64(&downloaded)), total)
					}
					continue
				}

				resultChan <- tileResult{tile: tile, data: data, success: true}
				atomic.AddInt64(&downloaded, 1)
				if onProgress != nil {
					onProgress(int(atomic.LoadInt64(&downloaded)), total)
				}
			}
		}()
	}

	// Send tiles to workers
	go func() {
		for _, tile := range tiles {
			tileChan <- tile
		}
		close(tileChan)
	}()

	// Wait for workers in background
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Calculate bounds
	minCol, maxCol := tiles[0].GetColumn(), tiles[0].GetColumn()
	minRow, maxRow := tiles[0].GetRow(), tiles[0].GetRow()

	for _, tile := range tiles {
		col := tile.GetColumn()
		row := tile.GetRow()
		if col < minCol {
			minCol = col
		}
		if col > maxCol {
			maxCol = col
		}
		if row < minRow {
			minRow = row
		}
		if row > maxRow {
			maxRow = row
		}
	}

	cols := maxCol - minCol + 1
	rows := maxRow - minRow + 1
	tileSize := 256
	outputWidth := cols * tileSize
	outputHeight := rows * tileSize

	// Create output image
	outputImg := image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))

	// Collect and stitch results
	successCount := 0
	for result := range resultChan {
		if !result.success {
			continue
		}

		// Decode JPEG
		img, err := jpeg.Decode(bytes.NewReader(result.data))
		if err != nil {
			continue
		}

		// Calculate position in output image
		col := result.tile.GetColumn()
		row := result.tile.GetRow()
		xOffset := (col - minCol) * tileSize
		yOffset := (maxRow - row) * tileSize // Y-axis inversion for Google Earth

		// Draw tile
		destRect := image.Rect(xOffset, yOffset, xOffset+tileSize, yOffset+tileSize)
		draw.Draw(outputImg, destRect, img, image.Point{0, 0}, draw.Src)

		successCount++
	}

	if successCount == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("no tiles downloaded successfully")
	}

	return outputImg, minCol, minRow, maxCol, maxRow, nil
}
