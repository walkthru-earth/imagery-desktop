package googleearth

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// Database URLs
	DatabaseURL = "https://khmdb.google.com/dbRoot.v5?&hl=en&gl=us&output=proto"

	// Tile URLs
	QuadtreePacketURL = "https://kh.google.com/flatfile?q2-%s-q.%d"
	DefaultTileURL    = "https://kh.google.com/flatfile?f1-%s-i.%d"
	HistoricalTileURL = "https://khmdb.google.com/flatfile?db=tm&f1-%s-i.%d-%s"

	// Compression magic numbers
	PacketMagic     = 0x7468dead
	PacketMagicSwap = 0xadde6874

	// User agent to mimic Google Earth Pro
	UserAgent = "GoogleEarth/7.3.6.10441(Macintosh;Mac OS X (26.2.0);en;kml:2.2;client:Pro;type:default)"
)

// Client handles communication with Google Earth servers
type Client struct {
	httpClient    *http.Client
	encryptionKey []byte
	dbVersion     int
	mu            sync.RWMutex
	initialized   bool

	// TimeMachine-specific fields (separate database with its own encryption)
	tmEncryptionKey  []byte
	tmDbVersion      int
	tmInitialized    bool
}

// NewClient creates a new Google Earth client with system proxy support
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
	}
}

// Initialize fetches the database root and encryption key
func (c *Client) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	// Fetch dbRoot
	req, err := http.NewRequest("GET", DatabaseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch dbRoot: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dbRoot request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read dbRoot: %w", err)
	}

	// Parse the encrypted dbRoot protobuf
	// The structure is: EncryptedDbRootProto with encryption_data and dbrootData fields
	// For now, we'll extract the encryption key from the protobuf manually
	if err := c.parseDbRoot(data); err != nil {
		return fmt.Errorf("failed to parse dbRoot: %w", err)
	}

	c.initialized = true
	return nil
}

// InitializeTimeMachine fetches the TimeMachine database root and its separate encryption key
func (c *Client) InitializeTimeMachine() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tmInitialized {
		return nil
	}

	// Fetch TimeMachine dbRoot
	req, err := http.NewRequest("GET", TimeMachineDatabaseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create TimeMachine request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch TimeMachine dbRoot: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TimeMachine dbRoot request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read TimeMachine dbRoot: %w", err)
	}

	// Parse the encrypted dbRoot protobuf for TimeMachine
	if err := c.parseTimeMachineDbRoot(data); err != nil {
		return fmt.Errorf("failed to parse TimeMachine dbRoot: %w", err)
	}

	c.tmInitialized = true
	return nil
}

// parseTimeMachineDbRoot extracts encryption key and version from the TimeMachine protobuf
func (c *Client) parseTimeMachineDbRoot(data []byte) error {
	// Same structure as regular dbRoot but with different encryption key
	offset := 0
	for offset < len(data) {
		if offset >= len(data) {
			break
		}

		tag := data[offset]
		fieldNum := tag >> 3
		wireType := tag & 0x07
		offset++

		if wireType == 2 { // Length-delimited
			length, n := decodeVarint(data[offset:])
			offset += n

			if fieldNum == 2 {
				// encryption_data
				c.tmEncryptionKey = make([]byte, length)
				copy(c.tmEncryptionKey, data[offset:offset+int(length)])
			} else if fieldNum == 3 {
				// dbrootData - encrypted and compressed
				encryptedData := make([]byte, length)
				copy(encryptedData, data[offset:offset+int(length)])

				// Decrypt using TimeMachine key
				c.decryptWithKey(encryptedData, c.tmEncryptionKey)

				// Decompress
				decompressed, err := c.decompress(encryptedData)
				if err != nil {
					return fmt.Errorf("failed to decompress TimeMachine dbRoot: %w", err)
				}

				// Extract quadtree version from decompressed protobuf
				c.tmDbVersion = c.extractQuadtreeVersion(decompressed)
			}
			offset += int(length)
		} else {
			if wireType == 0 { // Varint
				_, n := decodeVarint(data[offset:])
				offset += n
			} else {
				break
			}
		}
	}

	if len(c.tmEncryptionKey) == 0 {
		return fmt.Errorf("failed to extract TimeMachine encryption key")
	}

	return nil
}

// decryptWithKey XOR decrypts data using a specific encryption key
// This is the core decryption implementation used by both decrypt() and direct calls
func (c *Client) decryptWithKey(data []byte, key []byte) {
	if len(key) == 0 {
		return
	}

	off := 16
	for j := 0; j < len(data); j++ {
		data[j] ^= key[off]
		off++

		if off&7 == 0 {
			off += 16
		}
		// BUG FIX: Use len(key) instead of len(c.encryptionKey)
		if off >= len(key) {
			off = (off + 8) % 24
		}
	}
}

// parseDbRoot extracts encryption key and version from the protobuf
func (c *Client) parseDbRoot(data []byte) error {
	// The EncryptedDbRootProto has:
	// field 1 (bytes): encryption_data
	// field 2 (bytes): dbrootData (compressed and encrypted)

	// Simple protobuf parsing for these two fields
	offset := 0
	for offset < len(data) {
		if offset >= len(data) {
			break
		}

		// Read field tag
		tag := data[offset]
		fieldNum := tag >> 3
		wireType := tag & 0x07
		offset++

		if wireType == 2 { // Length-delimited
			// Read varint length
			length, n := decodeVarint(data[offset:])
			offset += n

			if fieldNum == 2 {
				// encryption_data
				c.encryptionKey = make([]byte, length)
				copy(c.encryptionKey, data[offset:offset+int(length)])
			} else if fieldNum == 3 {
				// dbrootData - encrypted and compressed
				encryptedData := make([]byte, length)
				copy(encryptedData, data[offset:offset+int(length)])

				// Decrypt
				c.decrypt(encryptedData)

				// Decompress
				decompressed, err := c.decompress(encryptedData)
				if err != nil {
					return fmt.Errorf("failed to decompress dbRoot: %w", err)
				}

				// Extract quadtree version from decompressed protobuf
				c.dbVersion = c.extractQuadtreeVersion(decompressed)
			}
			offset += int(length)
		} else {
			// Skip other wire types (like field 1: EncryptionType which is varint)
			// For varint (wireType 0), we need to read it to know how much to skip,
			// but since our simple parser doesn't handle skipping unknown fields correctly,
			// we might get stuck if we don't handle wireType 0.

			if wireType == 0 { // Varint
				_, n := decodeVarint(data[offset:])
				offset += n
			} else {
				// Unknown wire type, better to break than loop infinitely or crash
				break
			}
		}
	}

	if len(c.encryptionKey) == 0 {
		return fmt.Errorf("failed to extract encryption key")
	}

	return nil
}

// extractQuadtreeVersion parses the DbRootProto to get the quadtree version
func (c *Client) extractQuadtreeVersion(data []byte) int {
	offset := 0
	version := 1 // Default fallback

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n == 0 {
			break
		}
		offset += n
		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)

		if fieldNum == 13 && wireType == 2 {
			// Field 13 is Length-Delimited. It contains the version in nested field 1.
			length, n := decodeVarint(data[offset:])
			offset += n

			fieldBytes := data[offset : offset+int(length)]
			offset += int(length)

			// Parse nested message
			nestedOffset := 0
			for nestedOffset < len(fieldBytes) {
				nestedTag, nn := decodeVarint(fieldBytes[nestedOffset:])
				if nn == 0 {
					break
				}
				nestedOffset += nn

				nestedFieldNum := int(nestedTag >> 3)
				nestedWireType := int(nestedTag & 0x07)

				if nestedFieldNum == 1 && nestedWireType == 0 { // Field 1, Varint
					val, nn := decodeVarint(fieldBytes[nestedOffset:])
					nestedOffset += nn
					version = int(val)
					// Found it, we can return early or keep parsing if needed.
					// Let's break inner loop.
					break
				}

				// Skip other nested fields
				off, err := skipField(fieldBytes, nestedOffset, nestedWireType)
				if err != nil {
					break
				}
				nestedOffset = off
			}
			continue
		}

		// Skip other fields
		off, err := skipField(data, offset, wireType)
		if err != nil {
			break
		}
		offset = off
	}

	return version
}

func skipField(data []byte, offset int, wireType int) (int, error) {
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
	case 5: // 32-bit
		return offset + 4, nil
	default:
		return offset, fmt.Errorf("unknown wire type: %d", wireType)
	}
}

// decrypt XOR decrypts data using the client's default encryption key
// This is a convenience wrapper around decryptWithKey
func (c *Client) decrypt(data []byte) {
	c.decryptWithKey(data, c.encryptionKey)
}

// decompress handles the Google Earth compression format
func (c *Client) decompress(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("data too short for decompression")
	}

	// Check magic number
	magic := binary.LittleEndian.Uint32(data[:4])
	var decompSize uint32

	if magic == PacketMagic {
		decompSize = binary.LittleEndian.Uint32(data[4:8])
	} else if magic == PacketMagicSwap {
		decompSize = binary.BigEndian.Uint32(data[4:8])
	} else {
		// Not compressed, return as-is
		return data, nil
	}

	// Decompress using zlib
	reader, err := zlib.NewReader(bytes.NewReader(data[8:]))
	if err != nil {
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer reader.Close()

	result := make([]byte, decompSize)
	_, err = io.ReadFull(reader, result)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress: %w", err)
	}

	return result, nil
}

// FetchTile downloads a tile image
func (c *Client) FetchTile(tile *Tile) ([]byte, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	// 1. Get the QuadtreePacket containing this tile
	packet, err := c.GetQuadtreePacket(tile)
	if err != nil {
		return nil, fmt.Errorf("failed to get quadtree packet: %w", err)
	}
	if packet == nil {
		// Tile info not found in quadtree?
		return nil, fmt.Errorf("tile not found in quadtree")
	}

	// 2. Find the node in the packet
	subIndex := GetSubIndex(tile.Path)
	var node *QuadtreeNode
	for _, sqNode := range packet.SparseQuadtreeNodes {
		if int(sqNode.Index) == subIndex {
			node = sqNode.Node
			break
		}
	}

	if node == nil {
		return nil, fmt.Errorf("node not found in packet for subindex %d", subIndex)
	}

	// 3. Extract imagery epoch
	// We look for channel type 2 (Imagery) or similar.
	// Based on common knowledge/observation, Channel 2 is often Texture/Imagery.
	// Let's try to find channel with Type 2.
	// Also check Layers if Channel isn't present.

	// Default to epoch 1 if not found (though that's what was failing)
	epoch := 1
	found := false

	// Check channels first (common for imagery)
	for _, channel := range node.Channels {
		if channel.Type == 2 { // 2 = Imagery?
			epoch = int(channel.ChannelEpoch)
			found = true
			break
		}
	}

	// If not in channels, check layers
	if !found {
		for _, layer := range node.Layers {
			if layer.Type == 0 { // 0 = LAYER_TYPE_IMAGERY
				epoch = int(layer.LayerEpoch)
				found = true
				break
			}
		}
	}

	// Log what we found for debugging
	// fmt.Printf("Tile %s: Found epoch %d (found=%v)\n", tile.Path, epoch, found)

	url := fmt.Sprintf(DefaultTileURL, tile.Path, epoch)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tile data: %w", err)
	}

	// Decrypt the tile
	c.decrypt(data)

	return data, nil
}

// GetQuadtreePacket traverses the quadtree to find the packet containing the tile
func (c *Client) GetQuadtreePacket(tile *Tile) (*QuadtreePacket, error) {
	// Start with root packet
	dbVersion := c.dbVersion
	if dbVersion == 0 {
		dbVersion = 1 // Fallback
	}

	rootPath := "0"
	rootTile := &Tile{Path: rootPath}
	packet, err := c.FetchQuadtreePacket(rootTile, dbVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch root packet: %w", err)
	}

	// Traverse down
	// KeyholeTile.EnumerateIndices logic:
	// for (int end = SUBINDEX_MAX_SZ; end < Path.Length; end += SUBINDEX_MAX_SZ)
	// yield return new KeyholeTile(Path[..end]);

	traversalPaths := tile.TraversalPaths()

	for _, pathStr := range traversalPaths {
		// Find the node corresponding to this path in the current packet
		// The index used for lookup is the SubIndex of the path
		subIndex := GetSubIndex(pathStr)

		var node *QuadtreeNode
		for _, sqNode := range packet.SparseQuadtreeNodes {
			if int(sqNode.Index) == subIndex {
				node = sqNode.Node
				break
			}
		}

		if node == nil {
			// If we can't find the path node, we can't traverse further?
			// Or maybe this tile doesn't exist at this level?
			// For now, return what we have or error
			return nil, fmt.Errorf("traversal failed at %s", pathStr)
		}

		if node.CacheNodeEpoch != 0 {
			// We need to fetch a new packet
			pathTile := &Tile{Path: pathStr}
			packet, err = c.FetchQuadtreePacket(pathTile, int(node.CacheNodeEpoch))
			if err != nil {
				return nil, fmt.Errorf("failed to fetch child packet at %s: %w", pathStr, err)
			}
		}
		// If CacheNodeEpoch is 0, we stay in the current packet
	}

	return packet, nil
}

// FetchQuadtreePacket downloads and parses a quadtree packet for date availability
func (c *Client) FetchQuadtreePacket(tile *Tile, epoch int) (*QuadtreePacket, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	url := fmt.Sprintf(QuadtreePacketURL, tile.Path, epoch)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch quadtree packet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("quadtree packet request failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read quadtree packet: %w", err)
	}

	// Decrypt
	c.decrypt(data)

	// Decompress
	decompressed, err := c.decompress(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress quadtree packet: %w", err)
	}

	// Parse binary packet
	packet, err := ParseQuadtreePacket(decompressed, false)
	if err != nil {
		return nil, fmt.Errorf("failed to parse quadtree packet: %w", err)
	}

	return packet, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.google-earth.kml+xml, application/vnd.google-earth.kmz, image/*, */*")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept-Language", "en-US,*")
	req.Header.Set("Connection", "Keep-Alive")
}

// decodeVarint decodes a protobuf varint
func decodeVarint(data []byte) (uint64, int) {
	var result uint64
	var shift uint
	for i, b := range data {
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1
		}
		shift += 7
		if i >= 9 {
			break
		}
	}
	return result, len(data)
}
