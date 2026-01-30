package decryptor

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
	
)

// tencInfo holds encryption parameters from the tenc box
type tencInfo struct {
	defaultIsProtected  byte
	defaultPerSampleIV  byte
	defaultKID          []byte
	defaultConstantIV   []byte
}

// extractTencInfo extracts encryption info from init segment
func extractTencInfo(init *mp4.InitSegment) (*tencInfo, error) {
	if init.Moov == nil {
		return nil, fmt.Errorf("no moov box")
	}

	// Find tenc box in the track
	for _, trak := range init.Moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
			continue
		}
		stsd := trak.Mdia.Minf.Stbl.Stsd
		if stsd == nil {
			continue
		}

		// Look for encrypted sample entries
		for _, child := range stsd.Children {
			var sinf *mp4.SinfBox
			switch entry := child.(type) {
			case *mp4.VisualSampleEntryBox:
				sinf = entry.Sinf
			case *mp4.AudioSampleEntryBox:
				sinf = entry.Sinf
			}

			if sinf != nil && sinf.Schi != nil && sinf.Schi.Tenc != nil {
				tenc := sinf.Schi.Tenc
				return &tencInfo{
					defaultIsProtected:  tenc.DefaultIsProtected,
					defaultPerSampleIV:  tenc.DefaultPerSampleIVSize,
					defaultKID:          tenc.DefaultKID,
					defaultConstantIV:   tenc.DefaultConstantIV,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no tenc box found")
}

type sencInfo struct {
	ivs        [][]byte
	subsamples [][]subsampleEntry
}

type subsampleEntry struct {
	clearBytes     uint16
	protectedBytes uint32
}

type trunInfo struct {
	samples []sampleEntry
}

type sampleEntry struct {
	size uint32
}

// parseMoofForDecryption extracts senc and trun info from moof box
func parseMoofForDecryption(moofData []byte, defaultIVSize byte) (*sencInfo, *trunInfo, error) {
	var senc *sencInfo
	trun := &trunInfo{}

	// Parse moof box structure
	offset := 8 // skip moof header

	for offset+8 <= len(moofData) {
		size := int(binary.BigEndian.Uint32(moofData[offset:]))
		if size < 8 || offset+size > len(moofData) {
			break
		}

		boxType := string(moofData[offset+4 : offset+8])

		if boxType == "traf" {
			// Parse traf contents
			trafEnd := offset + size
			trafOffset := offset + 8

			for trafOffset+8 <= trafEnd {
				trafBoxSize := int(binary.BigEndian.Uint32(moofData[trafOffset:]))
				if trafBoxSize < 8 || trafOffset+trafBoxSize > trafEnd {
					break
				}

				trafBoxType := string(moofData[trafOffset+4 : trafOffset+8])

				switch trafBoxType {
				case "trun":
					trun = parseTrun(moofData[trafOffset : trafOffset+trafBoxSize])
				case "senc":
					senc = parseSenc(moofData[trafOffset:trafOffset+trafBoxSize], defaultIVSize)
				}

				trafOffset += trafBoxSize
			}
		}

		offset += size
	}

	return senc, trun, nil
}

// parseTrun extracts sample info from trun box
func parseTrun(data []byte) *trunInfo {
	if len(data) < 16 {
		return &trunInfo{}
	}

	// trun: 8 header + 1 version + 3 flags + 4 sample_count
	flags := binary.BigEndian.Uint32(data[8:12]) & 0x00FFFFFF
	sampleCount := binary.BigEndian.Uint32(data[12:16])

	offset := 16

	// data offset present
	if flags&0x001 != 0 {
		offset += 4
	}
	// first sample flags present
	if flags&0x004 != 0 {
		offset += 4
	}

	samples := make([]sampleEntry, 0, sampleCount)

	for i := uint32(0); i < sampleCount && offset < len(data); i++ {
		var sample sampleEntry

		// sample duration present
		if flags&0x100 != 0 {
			offset += 4
		}
		// sample size present
		if flags&0x200 != 0 {
			if offset+4 <= len(data) {
				sample.size = binary.BigEndian.Uint32(data[offset:])
			}
			offset += 4
		}
		// sample flags present
		if flags&0x400 != 0 {
			offset += 4
		}
		// sample composition time offset present
		if flags&0x800 != 0 {
			offset += 4
		}

		samples = append(samples, sample)
	}

	return &trunInfo{samples: samples}
}

// parseSenc extracts IVs and subsamples from senc box
func parseSenc(data []byte, defaultIVSize byte) *sencInfo {
	if len(data) < 16 {
		return nil
	}

	// senc: 8 header + 1 version + 3 flags + 4 sample_count
	flags := binary.BigEndian.Uint32(data[8:12]) & 0x00FFFFFF
	sampleCount := binary.BigEndian.Uint32(data[12:16])

	hasSubsamples := flags&0x2 != 0
	ivSize := int(defaultIVSize)
	if ivSize == 0 {
		ivSize = 8 // default
	}

	offset := 16
	info := &sencInfo{
		ivs:        make([][]byte, 0, sampleCount),
		subsamples: make([][]subsampleEntry, 0, sampleCount),
	}

	for i := uint32(0); i < sampleCount && offset < len(data); i++ {
		// Read IV
		if offset+ivSize <= len(data) {
			iv := make([]byte, ivSize)
			copy(iv, data[offset:offset+ivSize])
			info.ivs = append(info.ivs, iv)
			offset += ivSize
		} else {
			break
		}

		// Read subsamples if present
		var subs []subsampleEntry
		if hasSubsamples && offset+2 <= len(data) {
			subCount := binary.BigEndian.Uint16(data[offset:])
			offset += 2

			for j := uint16(0); j < subCount && offset+6 <= len(data); j++ {
				sub := subsampleEntry{
					clearBytes:     binary.BigEndian.Uint16(data[offset:]),
					protectedBytes: binary.BigEndian.Uint32(data[offset+2:]),
				}
				subs = append(subs, sub)
				offset += 6
			}
		}
		info.subsamples = append(info.subsamples, subs)
	}

	return info
}

// incrementIV increments the IV counter
func incrementIV(iv []byte, blocks int) {
	for i := 0; i < blocks; i++ {
		for j := len(iv) - 1; j >= 0; j-- {
			iv[j]++
			if iv[j] != 0 {
				break
			}
		}
	}
}

// findSegmentStart finds where the media segment starts in combined init+segment data.
func findSegmentStart(data []byte) int {
	offset := 0
	moovFound := false

	for offset+8 <= len(data) {
		size := getBoxSize(data, offset)
		if size < 8 {
			return -1
		}

		boxType := string(data[offset+4 : offset+8])

		if boxType == "moov" {
			moovFound = true
		}

		if moovFound {
			if boxType == "styp" || boxType == "moof" || boxType == "sidx" || boxType == "emsg" {
				return offset
			}
		}

		offset += size
	}
	return -1
}

// getBoxSize returns the size of an MP4 box
func getBoxSize(data []byte, offset int) int {
	if offset+8 > len(data) {
		return -1
	}

	size := int(binary.BigEndian.Uint32(data[offset:]))

	if size == 1 && offset+16 <= len(data) {
		// Extended size - use lower 32 bits for practical purposes
		size = int(binary.BigEndian.Uint32(data[offset+12:]))
	}

	return size
}

// ValidateKey checks if the decryption key format is valid.
func ValidateKey(key string) error {
	if key == "" {
		return nil
	}

	parts := strings.Split(key, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid key format, expected KID:KEY")
	}

	if len(parts[0]) != 32 || len(parts[1]) != 32 {
		return fmt.Errorf("invalid key length, expected 32 hex chars for both KID and KEY")
	}

	return nil
}