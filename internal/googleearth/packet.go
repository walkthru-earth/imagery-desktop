package googleearth

import (
	"encoding/binary"
	"fmt"
)

// QuadtreePacket represents a packet of quadtree nodes
type QuadtreePacket struct {
	PacketEpoch         int32
	SparseQuadtreeNodes []*SparseQuadtreeNode
}

// SparseQuadtreeNode maps a sub-index to a node
type SparseQuadtreeNode struct {
	Index int32
	Node  *QuadtreeNode
}

// QuadtreeNode contains information about a specific tile node
type QuadtreeNode struct {
	Flags          int32 // Unused in Go logic usually, but kept for parity
	CacheNodeEpoch int32
	Layers         []*QuadtreeLayer
	Channels       []*QuadtreeChannel

	// Direct properties from Quantum for easier access
	ImageVersion   int16
	TerrainVersion int16
}

// QuadtreeLayer represents a layer
type QuadtreeLayer struct {
	Type       int32
	LayerEpoch int32
	Provider   int32
}

// QuadtreeChannel represents a channel
type QuadtreeChannel struct {
	Type         int32
	ChannelEpoch int32
}

// binary structs (internal)

type packetHeader struct {
	MagicId          uint32
	DataTypeId       uint32
	Version          int32
	NumInstances     int32
	DataInstanceSize int32
	DataBufferOffset int32
	DataBufferSize   int32
	MetaBufferSize   int32
}

type quantum struct {
	Children            uint16 // BTG: 2 bytes
	CacheNodeVersion    int16
	ImageVersion        int16
	TerrainVersion      int16
	NumChannels         int16
	Junk16              uint16
	TypeOffset          int32
	VersionOffset       int32
	ImageNeighbors      int64
	ImageDataProvider   byte
	TerrainDataProvider byte
	Junk16_2            uint16
}

const (
	headerSize  = 32
	quantumSize = 32
	magicId     = 32301
)

// ParseQuadtreePacket parses the custom binary format
func ParseQuadtreePacket(data []byte, isRoot bool) (*QuadtreePacket, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("data too short for header")
	}

	// Parse Header
	h := packetHeader{
		MagicId:          binary.LittleEndian.Uint32(data[0:4]),
		DataTypeId:       binary.LittleEndian.Uint32(data[4:8]),
		Version:          int32(binary.LittleEndian.Uint32(data[8:12])),
		NumInstances:     int32(binary.LittleEndian.Uint32(data[12:16])),
		DataInstanceSize: int32(binary.LittleEndian.Uint32(data[16:20])),
		DataBufferOffset: int32(binary.LittleEndian.Uint32(data[20:24])),
		DataBufferSize:   int32(binary.LittleEndian.Uint32(data[24:28])),
		MetaBufferSize:   int32(binary.LittleEndian.Uint32(data[28:32])),
	}

	if h.MagicId != magicId {
		return nil, fmt.Errorf("invalid magic id: %d", h.MagicId)
	}

	// Read Quanta
	quanta := make([]quantum, h.NumInstances)
	offset := headerSize
	for i := 0; i < int(h.NumInstances); i++ {
		if offset+quantumSize > len(data) {
			return nil, fmt.Errorf("data truncated reading quantum %d", i)
		}
		qDat := data[offset : offset+quantumSize]
		quanta[i] = quantum{
			Children:            binary.LittleEndian.Uint16(qDat[0:2]),
			CacheNodeVersion:    int16(binary.LittleEndian.Uint16(qDat[2:4])),
			ImageVersion:        int16(binary.LittleEndian.Uint16(qDat[4:6])),
			TerrainVersion:      int16(binary.LittleEndian.Uint16(qDat[6:8])),
			NumChannels:         int16(binary.LittleEndian.Uint16(qDat[8:10])),
			Junk16:              binary.LittleEndian.Uint16(qDat[10:12]),
			TypeOffset:          int32(binary.LittleEndian.Uint32(qDat[12:16])),
			VersionOffset:       int32(binary.LittleEndian.Uint32(qDat[16:20])),
			ImageNeighbors:      int64(binary.LittleEndian.Uint64(qDat[20:28])),
			ImageDataProvider:   qDat[28],
			TerrainDataProvider: qDat[29],
			Junk16_2:            binary.LittleEndian.Uint16(qDat[30:32]),
		}
		offset += quantumSize
	}

	// Channel data
	channelDataStart := int(h.DataBufferOffset)
	if channelDataStart > len(data) {
		// Just clamp or error? Logic says check.
		return nil, fmt.Errorf("channel data start out of bounds")
	}
	// The rest of data is channels (and meta).
	// We pass the full buffer and let traverse slice it using offsets.

	packet := &QuadtreePacket{
		PacketEpoch:         h.Version,
		SparseQuadtreeNodes: make([]*SparseQuadtreeNode, 0, h.NumInstances),
	}

	// Traverse
	traverse(quanta, data, channelDataStart, &packet.SparseQuadtreeNodes, 0, "", isRoot)

	return packet, nil
}

func traverse(quanta []quantum, data []byte, channelBaseOffset int, nodes *[]*SparseQuadtreeNode, nodeIndex int, path string, isRoot bool) int {
	if nodeIndex >= len(quanta) {
		return nodeIndex
	}

	q := quanta[nodeIndex]

	// Create Node
	node := &QuadtreeNode{
		CacheNodeEpoch: int32(q.CacheNodeVersion),
		ImageVersion:   q.ImageVersion,
		TerrainVersion: q.TerrainVersion,
	}

	// Parse Channels
	if q.NumChannels > 0 {
		// type_offset and version_offset are bytes from channelBaseOffset
		// They are arrays of shorts (int16)
		typeStart := channelBaseOffset + int(q.TypeOffset)
		verStart := channelBaseOffset + int(q.VersionOffset)

		// Validate bounds
		// Each channel is 2 bytes for type + 2 bytes for version?
		// No, type array and version array are separate.
		// num_channels * 2 bytes for each array.

		byteLen := int(q.NumChannels) * 2
		if typeStart+byteLen <= len(data) && verStart+byteLen <= len(data) {
			for i := 0; i < int(q.NumChannels); i++ {
				cType := int16(binary.LittleEndian.Uint16(data[typeStart+i*2 : typeStart+i*2+2]))
				cVer := int16(binary.LittleEndian.Uint16(data[verStart+i*2 : verStart+i*2+2]))

				node.Channels = append(node.Channels, &QuadtreeChannel{
					Type:         int32(cType),
					ChannelEpoch: int32(cVer),
				})
			}
		}
	}

	// Build Layers (simplified logic mapping C# logic)
	// Check bits in q.Children (BTG)
	// Bit 6 = HasImage
	if (q.Children & 0x40) != 0 {
		node.Layers = append(node.Layers, &QuadtreeLayer{
			Type:       2, // Imagery, commonly 2
			LayerEpoch: int32(q.ImageVersion),
			Provider:   int32(q.ImageDataProvider),
		})
	}
	// Bit 7 = HasTerrain
	if (q.Children & 0x80) != 0 {
		node.Layers = append(node.Layers, &QuadtreeLayer{
			Type:       1, // Terrain, commonly 1
			LayerEpoch: int32(q.TerrainVersion),
			Provider:   int32(q.TerrainDataProvider),
		})
	}

	// Calculate SubIndex
	var subIndex int32
	if isRoot {
		subIndex = int32(GetRootSubIndex("0" + path))
	} else if nodeIndex > 0 {
		subIndex = int32(GetTreeSubIndex(path))
	} else {
		subIndex = 0 // "0" or handled? If nodeIndex==0 and !isRoot, it's the packet root (usually not stored in sparse array? C# stores it at 0?)
		// C#: node_index > 0 ? GetTreeSubIndex : 0.
		// So first node has index 0.
	}

	*nodes = append(*nodes, &SparseQuadtreeNode{
		Index: subIndex,
		Node:  node,
	})

	nextNodeIndex := nodeIndex + 1

	// Recurse children
	// Bit 0, 1, 2, 3
	for i := 0; i < 4; i++ {
		mask := uint16(1 << i)
		if (q.Children & mask) != 0 {
			newPath := fmt.Sprintf("%s%d", path, i)
			nextNodeIndex = traverse(quanta, data, channelBaseOffset, nodes, nextNodeIndex, newPath, isRoot)
		}
	}

	return nextNodeIndex
}
