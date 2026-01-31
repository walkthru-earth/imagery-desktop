package common

import "fmt"

// TileBounds represents the min/max row and column bounds of a tile set
type TileBounds struct {
	MinCol int
	MaxCol int
	MinRow int
	MaxRow int
}

// Cols returns the number of columns in the bounds
func (tb TileBounds) Cols() int {
	return tb.MaxCol - tb.MinCol + 1
}

// Rows returns the number of rows in the bounds
func (tb TileBounds) Rows() int {
	return tb.MaxRow - tb.MinRow + 1
}

// Tile represents the minimal interface needed for bounds calculation
type Tile interface {
	GetRow() int
	GetColumn() int
}

// CalculateTileBounds calculates the min/max row and column bounds from a slice of tiles
func CalculateTileBounds(tiles []Tile) (TileBounds, error) {
	if len(tiles) == 0 {
		return TileBounds{}, fmt.Errorf("no tiles provided")
	}

	bounds := TileBounds{
		MinCol: tiles[0].GetColumn(),
		MaxCol: tiles[0].GetColumn(),
		MinRow: tiles[0].GetRow(),
		MaxRow: tiles[0].GetRow(),
	}

	for _, tile := range tiles[1:] {
		col := tile.GetColumn()
		row := tile.GetRow()

		if col < bounds.MinCol {
			bounds.MinCol = col
		}
		if col > bounds.MaxCol {
			bounds.MaxCol = col
		}
		if row < bounds.MinRow {
			bounds.MinRow = row
		}
		if row > bounds.MaxRow {
			bounds.MaxRow = row
		}
	}

	return bounds, nil
}
