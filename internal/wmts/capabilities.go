package wmts

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// WMTS XML structures for parsing capabilities
type Capabilities struct {
	XMLName xml.Name `xml:"Capabilities"`
	Contents Contents `xml:"Contents"`
}

type Contents struct {
	Layers []Layer `xml:"Layer"`
}

type Layer struct {
	Title      string       `xml:"http://www.opengis.net/ows/1.1 Title"`
	Abstract   string       `xml:"http://www.opengis.net/ows/1.1 Abstract"`
	Identifier string       `xml:"http://www.opengis.net/ows/1.1 Identifier"`
	TileMatrixSetLinks []TileMatrixSetLink `xml:"TileMatrixSetLink"`
	ResourceURL []ResourceURL `xml:"ResourceURL"`
}

type TileMatrixSetLink struct {
	TileMatrixSet string `xml:"TileMatrixSet"`
}

type ResourceURL struct {
	Format       string `xml:"format,attr"`
	ResourceType string `xml:"resourceType,attr"`
	Template     string `xml:"template,attr"`
}

// LayerInfo represents parsed WMTS layer information
type LayerInfo struct {
	Name           string
	Title          string
	Description    string
	TileMatrixSet  string
	TemplateURL    string
	Format         string
}

// FetchCapabilities fetches and parses WMTS capabilities from URL
func FetchCapabilities(url string) (*Capabilities, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch capabilities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch capabilities: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var caps Capabilities
	if err := xml.Unmarshal(data, &caps); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	return &caps, nil
}

// GetLayers extracts layer information from capabilities
func GetLayers(caps *Capabilities) []LayerInfo {
	var layers []LayerInfo

	for _, layer := range caps.Contents.Layers {
		info := LayerInfo{
			Name:        layer.Identifier,
			Title:       layer.Title,
			Description: layer.Abstract,
		}

		// Get tile matrix set
		if len(layer.TileMatrixSetLinks) > 0 {
			info.TileMatrixSet = layer.TileMatrixSetLinks[0].TileMatrixSet
		}

		// Get resource URL template
		for _, resource := range layer.ResourceURL {
			if resource.ResourceType == "tile" {
				info.TemplateURL = resource.Template
				info.Format = resource.Format
				break
			}
		}

		layers = append(layers, info)
	}

	return layers
}

// ConvertTemplateToXYZ converts WMTS template URL to XYZ format
// Example: https://tiles.maps.eox.at/wmts?layer=s2cloudless-2020&style=default&tilematrixset=g&Service=WMTS&Request=GetTile&Version=1.0.0&Format=image%2Fjpeg&TileMatrix={TileMatrix}&TileCol={TileCol}&TileRow={TileRow}
// Becomes: https://tiles.maps.eox.at/wmts?layer=s2cloudless-2020&...&TileMatrix={z}&TileCol={x}&TileRow={y}
func ConvertTemplateToXYZ(template string) string {
	// Replace WMTS placeholders with XYZ placeholders
	result := strings.ReplaceAll(template, "{TileMatrix}", "{z}")
	result = strings.ReplaceAll(result, "{TileCol}", "{x}")
	result = strings.ReplaceAll(result, "{TileRow}", "{y}")
	return result
}

// ValidateWMTSURL checks if a URL is a valid WMTS capabilities endpoint
func ValidateWMTSURL(url string) (bool, error) {
	caps, err := FetchCapabilities(url)
	if err != nil {
		return false, err
	}

	// Check if we got at least one layer
	if len(caps.Contents.Layers) == 0 {
		return false, fmt.Errorf("no layers found in capabilities")
	}

	return true, nil
}
