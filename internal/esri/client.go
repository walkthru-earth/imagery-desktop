package esri

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// WayBack WMTS capabilities URL
	WayBackCapabilitiesURL = "https://wayback.maptiles.arcgis.com/arcgis/rest/services/world_imagery/mapserver/wmts/1.0.0/wmtscapabilities.xml"

	// User agent
	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36"
)

// Layer represents an Esri World Imagery Wayback layer
type Layer struct {
	ID          int
	Title       string
	Date        time.Time
	Identifier  string
	Format      string
	ResourceURL string
	MatrixSets  []string
}

// DatedTile represents a tile with its capture date
type DatedTile struct {
	Tile        *EsriTile
	Layer       *Layer
	CaptureDate time.Time
	LayerDate   time.Time
}

// Client handles communication with Esri World Imagery Wayback
type Client struct {
	httpClient  *http.Client
	layers      map[int]*Layer
	layerList   []*Layer // Ordered by date (newest first)
	mu          sync.RWMutex
	initialized bool
}

// NewClient creates a new Esri Wayback client with system proxy support
func NewClient() *Client {
	// Use http.ProxyFromEnvironment to respect system proxy settings
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	return &Client{
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		layers: make(map[int]*Layer),
	}
}

// Initialize fetches the WMTS capabilities and parses available layers
func (c *Client) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	req, err := http.NewRequest("GET", WayBackCapabilitiesURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch capabilities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("capabilities request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read capabilities: %w", err)
	}

	layers, err := parseCapabilities(data)
	if err != nil {
		return fmt.Errorf("failed to parse capabilities: %w", err)
	}

	for _, layer := range layers {
		c.layers[layer.ID] = layer
	}
	c.layerList = layers

	c.initialized = true
	return nil
}

// GetLayers returns all available layers ordered by date (newest first)
func (c *Client) GetLayers() ([]*Layer, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*Layer, len(c.layerList))
	copy(result, c.layerList)
	return result, nil
}

// GetLayerByID returns a specific layer
func (c *Client) GetLayerByID(id int) (*Layer, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	layer, ok := c.layers[id]
	if !ok {
		return nil, fmt.Errorf("layer %d not found", id)
	}
	return layer, nil
}

// FetchTile downloads a tile image from a specific layer
func (c *Client) FetchTile(layer *Layer, tile *EsriTile) ([]byte, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	tileURL := layer.GetAssetURL(tile)

	req, err := http.NewRequest("GET", tileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile request failed with status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// GetAvailableDates returns all available dates for a tile
func (c *Client) GetAvailableDates(tile *EsriTile) ([]*DatedTile, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	var result []*DatedTile
	var skipUntil *int
	var lastDate *time.Time
	var lastLayer *Layer

	c.mu.RLock()
	layers := c.layerList
	c.mu.RUnlock()

	for _, layer := range layers {
		if skipUntil != nil {
			if layer.ID == *skipUntil {
				skipUntil = nil
			}
			continue
		}

		// Check tilemap for availability
		tileMapURL := layer.GetTileMapURL(tile)
		available, nextID, err := c.checkTileMap(tileMapURL)
		if err != nil {
			continue
		}

		if nextID > 0 {
			skipUntil = &nextID
			layer = c.layers[nextID]
		}

		if available {
			// Get actual capture date for this tile
			date, err := c.getTileDate(layer, tile)
			if err != nil {
				date = layer.Date
			}

			if lastDate != nil && lastLayer != nil && !lastDate.Equal(date) {
				// Emit previous layer when date changes
				result = append(result, &DatedTile{
					Tile:        tile,
					Layer:       lastLayer,
					CaptureDate: *lastDate,
					LayerDate:   lastLayer.Date,
				})
			}
			lastDate = &date
			lastLayer = layer
		}
	}

	// Emit last layer
	if lastDate != nil && lastLayer != nil {
		result = append(result, &DatedTile{
			Tile:        tile,
			Layer:       lastLayer,
			CaptureDate: *lastDate,
			LayerDate:   lastLayer.Date,
		})
	}

	return result, nil
}

// GetNearestDatedTile finds the closest tile to a desired date
func (c *Client) GetNearestDatedTile(tile *EsriTile, desiredDate time.Time) (*DatedTile, error) {
	dates, err := c.GetAvailableDates(tile)
	if err != nil {
		return nil, err
	}

	if len(dates) == 0 {
		return nil, fmt.Errorf("no imagery available for tile")
	}

	var nearest *DatedTile
	for _, dt := range dates {
		if nearest == nil {
			nearest = dt
			continue
		}

		if dt.CaptureDate.Before(desiredDate) || dt.CaptureDate.Equal(desiredDate) {
			d1 := nearest.CaptureDate.Sub(desiredDate)
			d2 := desiredDate.Sub(dt.CaptureDate)
			if d1 < 0 {
				d1 = -d1
			}
			if d2 < d1 {
				nearest = dt
			}
			break
		}
		nearest = dt
	}

	return nearest, nil
}

// checkTileMap checks if a tile is available and returns the next layer ID to check
func (c *Client) checkTileMap(tileMapURL string) (available bool, nextID int, err error) {
	req, err := http.NewRequest("GET", tileMapURL, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, 0, fmt.Errorf("tilemap request failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Data   []int `json:"data"`
		Select []int `json:"select"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, 0, err
	}

	if len(result.Select) > 0 {
		nextID = result.Select[0]
	}

	available = len(result.Data) > 0 && result.Data[0] == 1
	return available, nextID, nil
}

// getTileDate fetches the actual capture date for a tile
func (c *Client) getTileDate(layer *Layer, tile *EsriTile) (time.Time, error) {
	metadataURL := layer.GetPointQueryURL(tile)

	req, err := http.NewRequest("GET", metadataURL, nil)
	if err != nil {
		return layer.Date, err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return layer.Date, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return layer.Date, nil
	}

	var result struct {
		Features []struct {
			Attributes struct {
				SrcDate2 int64 `json:"SRC_DATE2"`
			} `json:"attributes"`
		} `json:"features"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return layer.Date, err
	}

	if len(result.Features) > 0 && result.Features[0].Attributes.SrcDate2 > 0 {
		return time.UnixMilli(result.Features[0].Attributes.SrcDate2), nil
	}

	return layer.Date, nil
}

// GetAssetURL returns the tile image URL
func (l *Layer) GetAssetURL(tile *EsriTile) string {
	url := l.ResourceURL
	url = strings.Replace(url, "{TileMatrixSet}", l.MatrixSets[0], 1)
	url = strings.Replace(url, "{TileMatrix}", strconv.Itoa(tile.Level), 1)
	url = strings.Replace(url, "{TileRow}", strconv.Itoa(tile.Row), 1)
	url = strings.Replace(url, "{TileCol}", strconv.Itoa(tile.Column), 1)
	return url
}

// GetTileMapURL returns the tilemap URL for checking availability
func (l *Layer) GetTileMapURL(tile *EsriTile) string {
	const keyText = "/World_Imagery"
	idx := strings.Index(l.ResourceURL, keyText)
	if idx == -1 {
		return ""
	}
	base := l.ResourceURL[:idx+len(keyText)]
	return fmt.Sprintf("%s/MapServer/tilemap/%d/%d/%d/%d", base, l.ID, tile.Level, tile.Row, tile.Column)
}

// GetPointQueryURL returns the metadata query URL for a tile center
func (l *Layer) GetPointQueryURL(tile *EsriTile) string {
	const keyText = "/World_Imagery"
	idx := strings.Index(l.ResourceURL, keyText)
	if idx == -1 {
		return ""
	}

	// Get metadata service URL
	resourceStart := strings.Index(l.ResourceURL, "//") + 2
	resourceEnd := strings.Index(l.ResourceURL[resourceStart:], ".") + resourceStart
	newDomain := l.ResourceURL[:resourceStart] + "metadata" + l.ResourceURL[resourceEnd:]
	metaIdx := strings.Index(newDomain, keyText)
	base := newDomain[:metaIdx+len(keyText)]

	// Determine scale level for metadata service
	scale := min(13, 23-tile.Level)

	// Get identifier suffix (remove "WB" prefix)
	suffix := strings.ToLower(strings.Replace(l.Identifier, "WB", "", 1))

	center := tile.Center()

	queryURL := fmt.Sprintf("%s_Metadata%s/MapServer/%d/query?f=json&where=1%%3D1&outFields=SRC_DATE2&returnGeometry=false&geometryType=esriGeometryPoint&spatialRel=esriSpatialRelIntersects&geometry=%%7B%%22spatialReference%%22%%3A%%7B%%22wkid%%22%%3A%d%%7D%%2C%%22x%%22%%3A%f%%2C%%22y%%22%%3A%f%%7D",
		base, suffix, scale, EpsgNumber, center.X, center.Y)

	return queryURL
}

// WMTS Capabilities XML structures
type wmtsCapabilities struct {
	XMLName  xml.Name `xml:"Capabilities"`
	Contents struct {
		Layers []wmtsLayer `xml:"Layer"`
	} `xml:"Contents"`
}

type wmtsLayer struct {
	Title       string `xml:"Title"`
	Identifier  string `xml:"Identifier"`
	Format      string `xml:"Format"`
	ResourceURL struct {
		Template string `xml:"template,attr"`
	} `xml:"ResourceURL"`
	TileMatrixSetLinks []struct {
		TileMatrixSet string `xml:"TileMatrixSet"`
	} `xml:"TileMatrixSetLink"`
}

func parseCapabilities(data []byte) ([]*Layer, error) {
	var caps wmtsCapabilities
	if err := xml.Unmarshal(data, &caps); err != nil {
		return nil, err
	}

	var layers []*Layer
	for _, l := range caps.Contents.Layers {
		layer, err := parseLayer(l)
		if err != nil {
			continue // Skip layers that can't be parsed
		}
		layers = append(layers, layer)
	}

	return layers, nil
}

func parseLayer(l wmtsLayer) (*Layer, error) {
	// Parse date from title: "World Imagery (Wayback 2023-01-15)"
	const keyText = "(Wayback "
	idx := strings.Index(l.Title, keyText)
	if idx == -1 {
		return nil, fmt.Errorf("could not parse date from title: %s", l.Title)
	}

	dateStart := idx + len(keyText)
	dateEnd := strings.Index(l.Title[dateStart:], ")")
	if dateEnd == -1 {
		return nil, fmt.Errorf("could not parse date from title: %s", l.Title)
	}

	dateStr := l.Title[dateStart : dateStart+dateEnd]
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse date %s: %w", dateStr, err)
	}

	// Parse ID from ResourceURL
	id, err := parseIDFromURL(l.ResourceURL.Template)
	if err != nil {
		return nil, err
	}

	var matrixSets []string
	for _, link := range l.TileMatrixSetLinks {
		matrixSets = append(matrixSets, link.TileMatrixSet)
	}

	return &Layer{
		ID:          id,
		Title:       l.Title,
		Date:        date,
		Identifier:  l.Identifier,
		Format:      l.Format,
		ResourceURL: l.ResourceURL.Template,
		MatrixSets:  matrixSets,
	}, nil
}

func parseIDFromURL(resourceURL string) (int, error) {
	const keyText = "/MapServer/tile/"
	idx := strings.Index(resourceURL, keyText)
	if idx == -1 {
		return 0, fmt.Errorf("could not find MapServer in URL")
	}

	start := idx + len(keyText)
	end := strings.Index(resourceURL[start:], "/")
	if end == -1 {
		return 0, fmt.Errorf("could not parse ID from URL")
	}

	idStr := resourceURL[start : start+end]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("could not parse ID %s: %w", idStr, err)
	}

	return id, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Escape URL encodes a value
func escapeURL(s string) string {
	return url.QueryEscape(s)
}
