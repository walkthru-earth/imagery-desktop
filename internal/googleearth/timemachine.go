package googleearth

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// TimeMachine URL patterns
const (
	TimeMachineDatabaseURL   = "https://khmdb.google.com/dbRoot.v5?db=tm&hl=en&gl=us&output=proto"
	TimeMachinePacketURL     = "https://khmdb.google.com/flatfile?db=tm&qp-%s-q.%d"
	TimeMachineHistoricalURL = "https://khmdb.google.com/flatfile?db=tm&f1-%s-i.%d-%s"
)

// Layer types in quadtree packets
const (
	LayerTypeImagery        = 0
	LayerTypeTerrain        = 1
	LayerTypeVector         = 2
	LayerTypeImageryHistory = 3
)

// DatedTile represents a historical imagery tile with its date and epoch
type DatedTile struct {
	Date       time.Time
	Epoch      int    // The epoch to use for fetching (from quadtree traversal)
	TileEpoch  int    // The DatedTileEpoch from the metadata
	Provider   int
	HexDate    string
}

// TimeMachinePacket represents a protobuf quadtree packet from TimeMachine database
type TimeMachinePacket struct {
	PacketEpoch int32
	Nodes       []*TimeMachineNode
}

// TimeMachineNode represents a node in the TimeMachine quadtree
type TimeMachineNode struct {
	Index          int32
	CacheNodeEpoch int32
	Layers         []*TimeMachineLayer
}

// TimeMachineLayer represents a layer in a quadtree node
type TimeMachineLayer struct {
	Type       int32
	LayerEpoch int32
	Provider   int32
	DatesLayer *ImageryDates
}

// ImageryDates contains the historical dates for a tile
type ImageryDates struct {
	DatedTiles []*ImageryDatedTile
}

// ImageryDatedTile represents a single dated tile entry
type ImageryDatedTile struct {
	Date           int32 // Packed date format: (year<<9)|(month<<5)|day
	DatedTileEpoch int32
	Provider       int32
}

// DecodeGEDate decodes a packed Google Earth date to year, month, day
func DecodeGEDate(packed int32) (year, month, day int) {
	year = int(packed >> 9)
	month = int((packed >> 5) & 0xF)
	day = int(packed & 0x1F)
	return
}

// EncodeGEDate encodes a date to Google Earth packed format
func EncodeGEDate(year, month, day int) int32 {
	return int32(((year & 0x7FF) << 9) | ((month & 0xF) << 5) | (day & 0x1F))
}

// DateToHex converts a date to hex string for URL
func DateToHex(year, month, day int) string {
	return fmt.Sprintf("%x", EncodeGEDate(year, month, day))
}

// GetAvailableDates returns available historical imagery dates for a tile
func (c *Client) GetAvailableDates(tile *Tile) ([]DatedTile, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	// Fetch TimeMachine quadtree packet
	packet, err := c.FetchTimeMachinePacket(tile)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch TimeMachine packet: %w", err)
	}

	// Find the node for this tile
	subIndex := GetSubIndex(tile.Path)
	var targetNode *TimeMachineNode
	for _, node := range packet.Nodes {
		if int(node.Index) == subIndex {
			targetNode = node
			break
		}
	}

	if targetNode == nil {
		return nil, fmt.Errorf("node not found for tile %s", tile.Path)
	}

	// Find ImageryHistory layer
	var historyLayer *TimeMachineLayer
	for _, layer := range targetNode.Layers {
		if layer.Type == LayerTypeImageryHistory {
			historyLayer = layer
			break
		}
	}

	if historyLayer == nil || historyLayer.DatesLayer == nil {
		return nil, fmt.Errorf("no historical imagery available for tile %s", tile.Path)
	}

	// Log epoch sources for debugging
	log.Printf("[TimeMachine] Epoch sources for tile %s: LayerEpoch=%d, PacketEpoch=%d",
		tile.Path, historyLayer.LayerEpoch, packet.PacketEpoch)

	// Log DatedTileEpoch values from the first few dated tiles
	if len(historyLayer.DatesLayer.DatedTiles) > 0 {
		sampleEpochs := make([]int32, 0, 3)
		for i, dt := range historyLayer.DatesLayer.DatedTiles {
			if i >= 3 {
				break
			}
			sampleEpochs = append(sampleEpochs, dt.DatedTileEpoch)
		}
		log.Printf("[TimeMachine] Sample DatedTileEpoch values: %v", sampleEpochs)
	}

	// Extract dated tiles
	var dates []DatedTile
	const minValidDate = 545 // Minimum valid date as per GEHistoricalImagery

	for _, dt := range historyLayer.DatesLayer.DatedTiles {
		if dt.Date <= minValidDate {
			continue
		}

		year, month, day := DecodeGEDate(dt.Date)
		if month < 1 || month > 12 || day < 1 || day > 31 {
			continue // Invalid date
		}

		dates = append(dates, DatedTile{
			Date:      time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC),
			Epoch:     int(dt.DatedTileEpoch), // Use DatedTileEpoch - this is the TimeMachine tile version
			TileEpoch: int(dt.DatedTileEpoch),
			Provider:  int(dt.Provider),
			HexDate:   fmt.Sprintf("%x", dt.Date),
		})
	}

	return dates, nil
}

// FetchTimeMachinePacket fetches and parses a protobuf quadtree packet from TimeMachine database
func (c *Client) FetchTimeMachinePacket(tile *Tile) (*TimeMachinePacket, error) {
	log.Printf("[TimeMachine] FetchTimeMachinePacket called for tile: %s", tile.Path)

	// Initialize TimeMachine database (separate from default database)
	if !c.tmInitialized {
		log.Printf("[TimeMachine] TimeMachine not initialized, initializing...")
		if err := c.InitializeTimeMachine(); err != nil {
			log.Printf("[TimeMachine] TimeMachine initialization failed: %v", err)
			return nil, err
		}
		log.Printf("[TimeMachine] TimeMachine initialized successfully (key length: %d, dbVersion: %d)",
			len(c.tmEncryptionKey), c.tmDbVersion)
	}

	// Start with root packet and traverse using TimeMachine dbVersion
	dbVersion := c.tmDbVersion
	if dbVersion == 0 {
		dbVersion = 1
	}
	log.Printf("[TimeMachine] Using tmDbVersion: %d", dbVersion)

	// For traversal, we need to go through parent packets first
	rootPath := "0"
	rootTile := &Tile{Path: rootPath}

	log.Printf("[TimeMachine] Fetching root packet at path '%s' with epoch %d", rootPath, dbVersion)
	packet, err := c.fetchSingleTimeMachinePacket(rootTile, dbVersion)
	if err != nil {
		log.Printf("[TimeMachine] Failed to fetch root packet: %v", err)
		return nil, fmt.Errorf("failed to fetch root packet: %w", err)
	}
	log.Printf("[TimeMachine] Root packet fetched successfully, nodes: %d", len(packet.Nodes))

	// Traverse down to the target tile
	traversalPaths := tile.TraversalPaths()
	log.Printf("[TimeMachine] Traversal paths to target: %v", traversalPaths)

	for _, pathStr := range traversalPaths {
		subIndex := GetSubIndex(pathStr)
		log.Printf("[TimeMachine] Traversing path '%s', subIndex: %d", pathStr, subIndex)

		var node *TimeMachineNode
		for _, n := range packet.Nodes {
			if int(n.Index) == subIndex {
				node = n
				break
			}
		}

		if node == nil {
			log.Printf("[TimeMachine] Node not found for subIndex %d at path %s", subIndex, pathStr)
			log.Printf("[TimeMachine] Available nodes: %v", func() []int32 {
				var indices []int32
				for _, n := range packet.Nodes {
					indices = append(indices, n.Index)
				}
				return indices
			}())
			return nil, fmt.Errorf("traversal failed at %s", pathStr)
		}

		log.Printf("[TimeMachine] Found node at index %d, CacheNodeEpoch: %d, Layers: %d",
			node.Index, node.CacheNodeEpoch, len(node.Layers))

		if node.CacheNodeEpoch != 0 {
			log.Printf("[TimeMachine] Fetching child packet at path '%s' with epoch %d", pathStr, node.CacheNodeEpoch)
			pathTile := &Tile{Path: pathStr}
			packet, err = c.fetchSingleTimeMachinePacket(pathTile, int(node.CacheNodeEpoch))
			if err != nil {
				log.Printf("[TimeMachine] Failed to fetch child packet: %v", err)
				return nil, fmt.Errorf("failed to fetch child packet at %s: %w", pathStr, err)
			}
			log.Printf("[TimeMachine] Child packet fetched, nodes: %d", len(packet.Nodes))
		}
	}

	log.Printf("[TimeMachine] Traversal complete, final packet has %d nodes", len(packet.Nodes))
	return packet, nil
}

// fetchSingleTimeMachinePacket downloads and parses a single TimeMachine protobuf packet
func (c *Client) fetchSingleTimeMachinePacket(tile *Tile, epoch int) (*TimeMachinePacket, error) {
	url := fmt.Sprintf(TimeMachinePacketURL, tile.Path, epoch)
	log.Printf("[TimeMachine] Fetching packet URL: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[TimeMachine] Failed to create request: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[TimeMachine] HTTP request failed: %v", err)
		return nil, fmt.Errorf("failed to fetch TimeMachine packet: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[TimeMachine] Response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		// Read body for error details
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[TimeMachine] Request failed. Status: %d, Body: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("TimeMachine packet request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[TimeMachine] Failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read TimeMachine packet: %w", err)
	}
	log.Printf("[TimeMachine] Received %d bytes", len(data))

	// Decrypt using TimeMachine-specific encryption key
	log.Printf("[TimeMachine] Decrypting data (TimeMachine encryption key length: %d)", len(c.tmEncryptionKey))
	c.decryptWithKey(data, c.tmEncryptionKey)

	// Decompress
	log.Printf("[TimeMachine] Decompressing data...")
	decompressed, err := c.decompress(data)
	if err != nil {
		log.Printf("[TimeMachine] Decompression failed: %v", err)
		return nil, fmt.Errorf("failed to decompress TimeMachine packet: %w", err)
	}
	log.Printf("[TimeMachine] Decompressed to %d bytes", len(decompressed))

	// Parse protobuf
	log.Printf("[TimeMachine] Parsing protobuf packet...")
	packet, err := ParseTimeMachinePacket(decompressed)
	if err != nil {
		log.Printf("[TimeMachine] Protobuf parsing failed: %v", err)
		return nil, fmt.Errorf("failed to parse TimeMachine packet: %w", err)
	}
	log.Printf("[TimeMachine] Parsed packet with epoch %d and %d nodes", packet.PacketEpoch, len(packet.Nodes))

	return packet, nil
}

// FetchHistoricalTile downloads a historical imagery tile for a specific date
func (c *Client) FetchHistoricalTile(tile *Tile, epoch int, hexDate string) ([]byte, error) {
	// Historical tiles require TimeMachine initialization
	if !c.tmInitialized {
		if err := c.InitializeTimeMachine(); err != nil {
			return nil, err
		}
	}

	url := fmt.Sprintf(TimeMachineHistoricalURL, tile.Path, epoch, hexDate)
	log.Printf("[TimeMachine] Fetching historical tile: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch historical tile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[TimeMachine] Historical tile request failed. Status: %d, Body: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("historical tile request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read historical tile data: %w", err)
	}

	log.Printf("[TimeMachine] Received historical tile: %d bytes", len(data))

	// Decrypt the tile using TimeMachine encryption key
	c.decryptWithKey(data, c.tmEncryptionKey)

	return data, nil
}

// ParseTimeMachinePacket parses a protobuf-encoded quadtree packet
// Note: This uses the GROUP wire type (deprecated in modern protobuf but used by Google Earth)
func ParseTimeMachinePacket(data []byte) (*TimeMachinePacket, error) {
	packet := &TimeMachinePacket{}
	offset := 0

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		switch fieldNum {
		case 1: // packet_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				packet.PacketEpoch = int32(val)
			}
		case 2: // sparsequadtreenode - can be GROUP (wireType 3) or length-delimited (wireType 2)
			if wireType == 3 { // Start group
				// Parse the group content until we hit the end group tag
				node := &TimeMachineNode{}
				offset = parseTimeMachineNodeGroup(data, offset, fieldNum, node)
				packet.Nodes = append(packet.Nodes, node)
			} else if wireType == 2 { // Length-delimited (fallback)
				length, n := decodeVarint(data[offset:])
				offset += n
				nodeData := data[offset : offset+int(length)]
				offset += int(length)

				node, err := parseTimeMachineNode(nodeData)
				if err == nil {
					packet.Nodes = append(packet.Nodes, node)
				}
			}
		default:
			// Skip unknown fields
			off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
			if err != nil {
				break
			}
			offset = off
		}
	}

	log.Printf("[TimeMachine] Parsed packet epoch %d with %d nodes", packet.PacketEpoch, len(packet.Nodes))
	return packet, nil
}

// parseTimeMachineNodeGroup parses a SparseQuadtreeNode using group encoding
func parseTimeMachineNodeGroup(data []byte, offset int, groupFieldNum int, node *TimeMachineNode) int {
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		// Check for end group
		if wireType == 4 { // End group
			if fieldNum == groupFieldNum {
				return offset
			}
			// Wrong end group tag, something is wrong
			break
		}

		switch fieldNum {
		case 3: // index (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				node.Index = int32(val)
			}
		case 4: // Node (can be group or length-delimited)
			if wireType == 3 { // Start group
				offset = parseQuadtreeNodeGroupInto(data, offset, fieldNum, node)
			} else if wireType == 2 { // Length-delimited
				length, n := decodeVarint(data[offset:])
				offset += n
				nodeData := data[offset : offset+int(length)]
				offset += int(length)
				parseQuadtreeNodeInto(nodeData, node)
			}
		default:
			off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
			if err != nil {
				break
			}
			offset = off
		}
	}
	return offset
}

// parseQuadtreeNodeGroupInto parses the inner QuadtreeNode using group encoding
func parseQuadtreeNodeGroupInto(data []byte, offset int, groupFieldNum int, node *TimeMachineNode) int {
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		// Check for end group
		if wireType == 4 { // End group
			if fieldNum == groupFieldNum {
				return offset
			}
			break
		}

		switch fieldNum {
		case 2: // cache_node_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				node.CacheNodeEpoch = int32(val)
			}
		case 3: // layer (can be group or length-delimited, repeated)
			if wireType == 3 { // Start group
				layer := &TimeMachineLayer{}
				offset = parseLayerGroup(data, offset, fieldNum, layer)
				node.Layers = append(node.Layers, layer)
			} else if wireType == 2 { // Length-delimited
				length, n := decodeVarint(data[offset:])
				offset += n
				layerData := data[offset : offset+int(length)]
				offset += int(length)
				layer := parseLayer(layerData)
				if layer != nil {
					node.Layers = append(node.Layers, layer)
				}
			}
		default:
			off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
			if err != nil {
				break
			}
			offset = off
		}
	}
	return offset
}

// parseLayerGroup parses a Layer using group encoding
func parseLayerGroup(data []byte, offset int, groupFieldNum int, layer *TimeMachineLayer) int {
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		// Check for end group
		if wireType == 4 { // End group
			if fieldNum == groupFieldNum {
				return offset
			}
			break
		}

		switch fieldNum {
		case 1: // type (varint/enum)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				layer.Type = int32(val)
			}
		case 2: // layer_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				layer.LayerEpoch = int32(val)
			}
		case 3: // provider (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				layer.Provider = int32(val)
			}
		case 4: // dates_layer (can be group or length-delimited)
			if wireType == 3 { // Start group
				layer.DatesLayer = &ImageryDates{}
				offset = parseDatesLayerGroup(data, offset, fieldNum, layer.DatesLayer)
			} else if wireType == 2 { // Length-delimited
				length, n := decodeVarint(data[offset:])
				offset += n
				datesData := data[offset : offset+int(length)]
				offset += int(length)
				layer.DatesLayer = parseDatesLayer(datesData)
			}
		default:
			off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
			if err != nil {
				break
			}
			offset = off
		}
	}
	return offset
}

// parseDatesLayerGroup parses ImageryDates using group encoding
func parseDatesLayerGroup(data []byte, offset int, groupFieldNum int, dates *ImageryDates) int {
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		// Check for end group
		if wireType == 4 { // End group
			if fieldNum == groupFieldNum {
				return offset
			}
			break
		}

		switch fieldNum {
		case 1: // dated_tile (repeated, can be group or length-delimited)
			if wireType == 3 { // Start group
				dt := &ImageryDatedTile{}
				offset = parseDatedTileGroup(data, offset, fieldNum, dt)
				dates.DatedTiles = append(dates.DatedTiles, dt)
			} else if wireType == 2 { // Length-delimited
				length, n := decodeVarint(data[offset:])
				offset += n
				tileData := data[offset : offset+int(length)]
				offset += int(length)
				dt := parseDatedTile(tileData)
				if dt != nil {
					dates.DatedTiles = append(dates.DatedTiles, dt)
				}
			}
		default:
			off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
			if err != nil {
				break
			}
			offset = off
		}
	}
	return offset
}

// parseDatedTileGroup parses a DatedTile using group encoding
func parseDatedTileGroup(data []byte, offset int, groupFieldNum int, dt *ImageryDatedTile) int {
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		// Check for end group
		if wireType == 4 { // End group
			if fieldNum == groupFieldNum {
				return offset
			}
			break
		}

		switch fieldNum {
		case 1: // date (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				dt.Date = int32(val)
			}
		case 2: // dated_tile_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				dt.DatedTileEpoch = int32(val)
			}
		case 3: // provider (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				dt.Provider = int32(val)
			}
		default:
			off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
			if err != nil {
				break
			}
			offset = off
		}
	}
	return offset
}

// skipFieldWithGroup skips a field, handling group wire types
func skipFieldWithGroup(data []byte, offset int, wireType int, fieldNum int) (int, error) {
	if offset >= len(data) {
		return offset, nil
	}
	switch wireType {
	case 0: // Varint
		_, n := decodeVarint(data[offset:])
		return offset + n, nil
	case 1: // 64-bit
		return offset + 8, nil
	case 2: // Length-delimited
		length, n := decodeVarint(data[offset:])
		return offset + n + int(length), nil
	case 3: // Start group - need to skip until matching end group
		return skipGroup(data, offset, fieldNum)
	case 4: // End group - should have been handled by caller
		return offset, nil
	case 5: // 32-bit
		return offset + 4, nil
	default:
		return offset, fmt.Errorf("unknown wire type: %d", wireType)
	}
}

// skipGroup skips all fields until the matching end group tag
func skipGroup(data []byte, offset int, groupFieldNum int) (int, error) {
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		if wireType == 4 && fieldNum == groupFieldNum {
			// Found matching end group
			return offset, nil
		}

		// Skip this field
		off, err := skipFieldWithGroup(data, offset, wireType, fieldNum)
		if err != nil {
			return off, err
		}
		offset = off
	}
	return offset, fmt.Errorf("end group not found for field %d", groupFieldNum)
}

func parseTimeMachineNode(data []byte) (*TimeMachineNode, error) {
	node := &TimeMachineNode{}
	offset := 0

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		switch fieldNum {
		case 3: // index (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				node.Index = int32(val)
			}
		case 4: // Node (length-delimited)
			if wireType == 2 {
				length, n := decodeVarint(data[offset:])
				offset += n
				nodeData := data[offset : offset+int(length)]
				offset += int(length)

				parseQuadtreeNodeInto(nodeData, node)
			}
		default:
			off, err := skipField(data, offset, wireType)
			if err != nil {
				break
			}
			offset = off
		}
	}

	return node, nil
}

func parseQuadtreeNodeInto(data []byte, node *TimeMachineNode) {
	offset := 0

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		switch fieldNum {
		case 2: // cache_node_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				node.CacheNodeEpoch = int32(val)
			}
		case 3: // layer (length-delimited, repeated)
			if wireType == 2 {
				length, n := decodeVarint(data[offset:])
				offset += n
				layerData := data[offset : offset+int(length)]
				offset += int(length)

				layer := parseLayer(layerData)
				if layer != nil {
					node.Layers = append(node.Layers, layer)
				}
			}
		default:
			off, err := skipField(data, offset, wireType)
			if err != nil {
				break
			}
			offset = off
		}
	}
}

func parseLayer(data []byte) *TimeMachineLayer {
	layer := &TimeMachineLayer{}
	offset := 0

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		switch fieldNum {
		case 1: // type (varint/enum)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				layer.Type = int32(val)
			}
		case 2: // layer_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				layer.LayerEpoch = int32(val)
			}
		case 3: // provider (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				layer.Provider = int32(val)
			}
		case 4: // dates_layer (length-delimited)
			if wireType == 2 {
				length, n := decodeVarint(data[offset:])
				offset += n
				datesData := data[offset : offset+int(length)]
				offset += int(length)

				layer.DatesLayer = parseDatesLayer(datesData)
			}
		default:
			off, err := skipField(data, offset, wireType)
			if err != nil {
				break
			}
			offset = off
		}
	}

	return layer
}

func parseDatesLayer(data []byte) *ImageryDates {
	dates := &ImageryDates{}
	offset := 0

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		switch fieldNum {
		case 1: // dated_tile (length-delimited, repeated)
			if wireType == 2 {
				length, n := decodeVarint(data[offset:])
				offset += n
				tileData := data[offset : offset+int(length)]
				offset += int(length)

				dt := parseDatedTile(tileData)
				if dt != nil {
					dates.DatedTiles = append(dates.DatedTiles, dt)
				}
			}
		default:
			off, err := skipField(data, offset, wireType)
			if err != nil {
				break
			}
			offset = off
		}
	}

	return dates
}

func parseDatedTile(data []byte) *ImageryDatedTile {
	dt := &ImageryDatedTile{}
	offset := 0

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		switch fieldNum {
		case 1: // date (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				dt.Date = int32(val)
			}
		case 2: // dated_tile_epoch (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				dt.DatedTileEpoch = int32(val)
			}
		case 3: // provider (varint)
			if wireType == 0 {
				val, n := decodeVarint(data[offset:])
				offset += n
				dt.Provider = int32(val)
			}
		default:
			off, err := skipField(data, offset, wireType)
			if err != nil {
				break
			}
			offset = off
		}
	}

	return dt
}
