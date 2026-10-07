package recognition

import (
	"bytes"
	"encoding/binary"
)

// Read only IFD0's orientation, with bounds checked before every offset. The
// same TIFF EXIF structure occurs in JPEG APP1, PNG eXIf and WebP EXIF chunks.
// Malformed/absent metadata is ignored; the image decoder still validates pixels.
func exifOrientation(data []byte) int {
	data = bytes.TrimPrefix(data, []byte("Exif\x00\x00"))
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset < 8 || offset+2 > uint64(len(data)) {
		return 1
	}
	count := int(order.Uint16(data[offset : offset+2]))
	offset += 2
	for i := 0; i < count; i++ {
		if offset+12 > uint64(len(data)) {
			return 1
		}
		entry := data[offset : offset+12]
		if order.Uint16(entry[:2]) == 0x0112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			value := int(order.Uint16(entry[8:10]))
			if value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
		offset += 12
	}
	return 1
}

func imageOrientation(data []byte) int {
	if bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
		for pos := 2; pos < len(data); {
			if data[pos] != 0xff {
				break
			}
			for pos < len(data) && data[pos] == 0xff {
				pos++
			}
			if pos >= len(data) {
				break
			}
			marker := data[pos]
			pos++
			if marker == 0xda || marker == 0xd9 {
				break
			}
			if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
				continue
			}
			if pos+2 > len(data) {
				break
			}
			size := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			if size < 2 || size > len(data)-pos {
				break
			}
			payload := data[pos+2 : pos+size]
			if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				return exifOrientation(payload)
			}
			pos += size
		}
	} else if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		for pos := 8; pos+12 <= len(data); {
			size := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
			if size > uint64(len(data)-pos-12) {
				break
			}
			if string(data[pos+4:pos+8]) == "eXIf" {
				return exifOrientation(data[pos+8 : pos+8+int(size)])
			}
			pos += int(size) + 12
		}
	} else if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		for pos := 12; pos+8 <= len(data); {
			size := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
			if size > uint64(len(data)-pos-8) {
				break
			}
			if string(data[pos:pos+4]) == "EXIF" {
				return exifOrientation(data[pos+8 : pos+8+int(size)])
			}
			pos += 8 + int(size) + (int(size) & 1)
		}
	} else {
		return exifOrientation(data)
	}
	return 1
}
