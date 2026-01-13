package geotiff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"math"
	"sort"
)

// We use constants from chai2010/tiff or define our own minimal set.
// Defining minimal set is safer to avoid dependency weirdness if we only need a few.

const (
	DataType_Byte     = 1
	DataType_ASCII    = 2
	DataType_Short    = 3
	DataType_Long     = 4
	DataType_Rational = 5
	DataType_Double   = 12
	DataType_IFD      = 13

	TagType_ImageWidth                = 256
	TagType_ImageLength               = 257
	TagType_BitsPerSample             = 258
	TagType_Compression               = 259
	TagType_PhotometricInterpretation = 262
	TagType_StripOffsets              = 273
	TagType_SamplesPerPixel           = 277
	TagType_RowsPerStrip              = 278
	TagType_StripByteCounts           = 279
	TagType_XResolution               = 282
	TagType_YResolution               = 283
	TagType_ResolutionUnit            = 296

	// GeoTIFF Tags
	TagType_ModelPixelScaleTag = 33550
	TagType_ModelTiepointTag   = 33922
	TagType_GeoKeyDirectoryTag = 34735
	TagType_GeoDoubleParamsTag = 34736
	TagType_GeoAsciiParamsTag  = 34737
)

var enc = binary.LittleEndian

type ifdEntry struct {
	tag      uint16
	datatype uint16
	count    uint32
	data     []byte
}

type byTag []ifdEntry

func (d byTag) Len() int           { return len(d) }
func (d byTag) Less(i, j int) bool { return d[i].tag < d[j].tag }
func (d byTag) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

// Encode writes the image m to w as an uncompressed RGBA TIFF.
// extraTags is a map of TagID -> value.
// Supported value types: []uint16 (SHORT), []float64 (DOUBLE), string (ASCII).
func Encode(w io.Writer, m image.Image, extraTags map[uint16]interface{}) error {
	bounds := m.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// 1. Write Header
	// LittleEndian (II), Version 42 (0x2A), First IFD Offset (8)
	header := []byte{'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}
	if _, err := w.Write(header); err != nil {
		return err
	}

	// 2. Prepare Image Data (Uncompressed RGBA)
	// We assume RGBA for simplicity as per requirements (merged tiles are likely RGBA).
	// If input is not RGBA, we convert.
	// TIFF RGBA: 8 bits per sample, 4 samples (R,G,B,A).

	// Buffer for pixel data
	pixelData := new(bytes.Buffer)

	// Write pixels
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := m.At(x, y).RGBA()
			// RGBA() returns 16-bit values. Convert to 8-bit.
			pixelData.WriteByte(uint8(r >> 8))
			pixelData.WriteByte(uint8(g >> 8))
			pixelData.WriteByte(uint8(b >> 8))
			pixelData.WriteByte(uint8(a >> 8))
		}
	}

	pixels := pixelData.Bytes()
	imageLen := uint32(len(pixels))

	// 3. Write Image Data immediately after header?
	// Header says First IFD is at 8. So we write IFD at 8.
	// Then pixels come after IFD (or before? Standard allows anywhere).
	// Usually: Header -> IFD -> Pixels.
	// Let's write IFD at offset 8.

	// We need to know IFD size to determine Pixel Offset.
	// IFD Size = 2 (count) + 12*entries + 4 (next offset).
	// + Data for values > 4 bytes.

	// Let's prepare entries first.
	var entries []ifdEntry

	addEntry := func(tag uint16, datatype uint16, count uint32, data []byte) {
		entries = append(entries, ifdEntry{tag, datatype, count, data})
	}

	// Standard Tags
	addEntry(TagType_ImageWidth, DataType_Short, 1, enc16(uint16(width)))
	addEntry(TagType_ImageLength, DataType_Short, 1, enc16(uint16(height)))
	addEntry(TagType_BitsPerSample, DataType_Short, 4, enc16s([]uint16{8, 8, 8, 8}))
	addEntry(TagType_Compression, DataType_Short, 1, enc16(1))               // None
	addEntry(TagType_PhotometricInterpretation, DataType_Short, 1, enc16(2)) // RGB
	addEntry(TagType_SamplesPerPixel, DataType_Short, 1, enc16(4))
	addEntry(TagType_RowsPerStrip, DataType_Short, 1, enc16(uint16(height)))
	addEntry(TagType_XResolution, DataType_Rational, 1, encRational(72, 1))
	addEntry(TagType_YResolution, DataType_Rational, 1, encRational(72, 1))
	addEntry(TagType_ResolutionUnit, DataType_Short, 1, enc16(2)) // Inch

	// Placeholder for StripOffsets and StripByteCounts (calculated later)
	// We'll update them once we know where pixels start.
	addEntry(TagType_StripOffsets, DataType_Long, 1, make([]byte, 4))
	addEntry(TagType_StripByteCounts, DataType_Long, 1, make([]byte, 4)) // we know count though

	// Extra Tags (GeoTags)
	for tag, val := range extraTags {
		switch v := val.(type) {
		case []uint16:
			addEntry(tag, DataType_Short, uint32(len(v)), enc16s(v))
		case []float64:
			addEntry(tag, DataType_Double, uint32(len(v)), encDoubles(v))
		case string:
			// ASCII needs null terminator
			b := append([]byte(v), 0)
			addEntry(tag, DataType_ASCII, uint32(len(b)), b)
		default:
			return fmt.Errorf("unsupported tag value type for tag %d", tag)
		}
	}

	sort.Sort(byTag(entries))

	// Calculate offsets
	// Header: 8 bytes
	// IFD Start: 8
	// IFD Entries: 2 + 12*N + 4
	ifdSize := 2 + 12*len(entries) + 4

	// Value Data Area (for values > 4 bytes) starts after IFD Table
	valueDataOffset := 8 + ifdSize

	// We collect all "large" data to write it sequentially
	var largeDataBuf bytes.Buffer

	// Fix up offsets in entries
	for i := range entries {
		e := &entries[i]
		dataLen := len(e.data)
		if dataLen <= 4 {
			// Fits in value field, pad with zeros
			// Right-padded? No, TIFF value is left-aligned in the 4 bytes?
			// "If the value fits into 4 bytes, the value is stored in the Value Offset."
			// Since we are LittleEndian, a SHORT (2 bytes) v is stored as [v_low, v_high, 0, 0].
			// Our e.data is already byte slice. We just need to assert it's <= 4.
			// Padded automatically when writing? No, we must pad e.data to 4 bytes if we write it directly.
			// But wait, if it fits, we write it INSTEAD of offset.
			// So we leave e.data as is, and when writing we check length.
		} else {
			// Does not fit, needs to go to data area
			currentOffset := uint32(valueDataOffset + largeDataBuf.Len())
			// The Entry ValueOffset field will hold this offset.
			// We temporarily store the offset in e.data? No, e.data holds the actual data.
			// We need a way to mark it.
			// We'll write the data to largeDataBuf now.
			largeDataBuf.Write(e.data)
			// And replace e.data with the offset
			e.data = enc32(currentOffset)
		}
	}

	// Now we know the end of Value Data Area.
	pixelsOffset := uint32(valueDataOffset + largeDataBuf.Len())

	// Update StripOffsets
	for i := range entries {
		if entries[i].tag == TagType_StripOffsets {
			entries[i].data = enc32(pixelsOffset) // Only 1 strip, so 1 offset.
			// If fits in 4 bytes (it does), we are good.
		}
		if entries[i].tag == TagType_StripByteCounts {
			entries[i].data = enc32(imageLen)
		}
	}

	// 4. Write IFD
	// Count
	if err := binary.Write(w, enc, uint16(len(entries))); err != nil {
		return err
	}

	// Entries
	for _, e := range entries {
		if err := binary.Write(w, enc, e.tag); err != nil {
			return err
		}
		if err := binary.Write(w, enc, e.datatype); err != nil {
			return err
		}
		if err := binary.Write(w, enc, e.count); err != nil {
			return err
		}

		// Offset/Value field (4 bytes)
		var val [4]byte
		copy(val[:], e.data) // Helper to copy available bytes (2 or 4) to 4-byte array
		if _, err := w.Write(val[:]); err != nil {
			return err
		}
	}

	// Next IFD Offset (0)
	if err := binary.Write(w, enc, uint32(0)); err != nil {
		return err
	}

	// 5. Write Large Data
	if _, err := largeDataBuf.WriteTo(w); err != nil {
		return err
	}

	// 6. Write Pixels
	if _, err := w.Write(pixels); err != nil {
		return err
	}

	return nil
}

// Helpers

func enc16(v uint16) []byte {
	b := make([]byte, 2)
	enc.PutUint16(b, v)
	return b
}

func enc32(v uint32) []byte {
	b := make([]byte, 4)
	enc.PutUint32(b, v)
	return b
}

func enc16s(vs []uint16) []byte {
	b := make([]byte, 2*len(vs))
	for i, v := range vs {
		enc.PutUint16(b[i*2:], v)
	}
	return b
}

func encDoubles(vs []float64) []byte {
	b := make([]byte, 8*len(vs))
	for i, v := range vs {
		// defined in math, but we need binary write float64
		// binary.BigEndian.PutUint64(..., math.Float64bits(v))
		// We use LittleEndian
		u := hostFloat64ToUint64(v)
		enc.PutUint64(b[i*8:], u)
	}
	return b
}

func encRational(num, den uint32) []byte {
	b := make([]byte, 8)
	enc.PutUint32(b[:4], num)
	enc.PutUint32(b[4:], den)
	return b
}

func hostFloat64ToUint64(f float64) uint64 {
	return math.Float64bits(f)
}
