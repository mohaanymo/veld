// Package decryptor handles decryption of encrypted DASH segments.
package decryptor

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
	
)

// Decryptor handles decryption of encrypted media segments.
type Decryptor struct {
	kid []byte
	key []byte
}

// New creates a new Decryptor with the given decryption key.
// keyString should be in format "KID:KEY" where both are 32 hex characters.
// Empty keyString creates a no-op decryptor.
func New(keyString string) (*Decryptor, error) {
	// if the dec key is empty then we return an empty decryptor
	if keyString == "" {
		return &Decryptor{}, nil
	}

	// Parse KID:KEY format
	parts := strings.Split(keyString, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid key format, expected KID:KEY")
	}

	// Decode only the KEY part (mp4ff handles KID from init segment)
	key, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid KEY hex: %w", err)
	}
	kid, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid KID hex: %w", err)
	}

	if len(key) != 16 {
		return nil, fmt.Errorf("KEY must be 16 bytes")
	}

	return &Decryptor{key: key, kid: kid}, nil
}


// Enabled returns true if decryption is configured.
func (d *Decryptor) Enabled() bool {
	return len(d.key) > 0
}

// Decrypt decrypts combined init+segment data.
// If decryption is not enabled, returns the original data unchanged.
// The combined slice should contain the init segment followed by the media segment.
func (d *Decryptor) Decrypt(combined []byte) ([]byte, error) {
	if !d.Enabled() {
		return combined, nil
	}

	// Find where the media segment begins
	segStart := findSegmentStart(combined)
	if segStart < 0 {
		return nil, fmt.Errorf("no media segment found in combined data")
	}

	initData := combined[:segStart]
	segData := combined[segStart:]

	// Parse init segment to get encryption info (tenc box)
	initSeg, err := mp4.DecodeFile(bytes.NewReader(initData))
	if err != nil {
		return nil, fmt.Errorf("parse init segment: %w", err)
	}

	if initSeg.Init == nil {
		return nil, fmt.Errorf("no init segment found")
	}

	// Extract encryption info from init
	tencInfo, err := extractTencInfo(initSeg.Init)
	if err != nil {
		// Not encrypted
		return combined, nil
	}
	
	if !bytes.Equal(tencInfo.defaultKID, d.kid) {
		return nil, fmt.Errorf("invalid decryption key, the KID does not match the init KID")
	}

	// Decrypt the segment data in place
	decryptedSeg, err := d.decryptSegmentData(segData, tencInfo)
	if err != nil {
		return nil, fmt.Errorf("decrypt segment: %w", err)
	}

	// Combine init + decrypted segment
	result := make([]byte, len(initData)+len(decryptedSeg))
	copy(result, initData)
	copy(result[len(initData):], decryptedSeg)

	return result, nil
}

// decryptSegmentData decrypts the media segment data
func (d *Decryptor) decryptSegmentData(segData []byte, tenc *tencInfo) ([]byte, error) {
	// Make a copy to modify
	result := make([]byte, len(segData))
	copy(result, segData)

	// Parse the segment structure to find moof and mdat boxes
	offset := 0
	var moofData, mdatData []byte
	var mdatOffset int

	for offset+8 <= len(result) {
		size := getBoxSize(result, offset)
		if size < 8 || offset+size > len(result) {
			break
		}

		boxType := string(result[offset+4 : offset+8])

		switch boxType {
		case "moof":
			moofData = result[offset : offset+size]
		case "mdat":
			mdatOffset = offset
			mdatData = result[offset : offset+size]
		}

		offset += size
	}

	if moofData == nil || mdatData == nil {
		return result, nil // No encryption data found
	}

	// Parse moof to get traf/trun/senc info
	sencInfo, trunInfo, err := parseMoofForDecryption(moofData, tenc.defaultPerSampleIV)
	if err != nil {
		return nil, fmt.Errorf("parse moof: %w", err)
	}

	if sencInfo == nil || len(sencInfo.ivs) == 0 {
		// No encryption info, might use constant IV
		if len(tenc.defaultConstantIV) == 0 {
			return result, nil // Not encrypted
		}
	}

	// Decrypt samples in mdat
	// mdat structure: 8 bytes header (size + "mdat") + data
	mdatHeaderSize := 8
	if len(mdatData) >= 8 && binary.BigEndian.Uint32(mdatData[0:4]) == 1 {
		mdatHeaderSize = 16 // extended size
	}

	sampleOffset := 0
	for i, sample := range trunInfo.samples {
		if sampleOffset+int(sample.size) > len(mdatData)-mdatHeaderSize {
			break
		}

		// Get IV for this sample
		var iv []byte
		if sencInfo != nil && i < len(sencInfo.ivs) {
			iv = sencInfo.ivs[i]
		}
		if len(iv) == 0 {
			iv = tenc.defaultConstantIV
		}
		if len(iv) == 0 {
			sampleOffset += int(sample.size)
			continue // No IV, skip
		}

		// Pad IV to 16 bytes
		if len(iv) == 8 {
			padded := make([]byte, 16)
			copy(padded, iv)
			iv = padded
		}

		// Get subsample info
		var subsamples []subsampleEntry
		if sencInfo != nil && i < len(sencInfo.subsamples) {
			subsamples = sencInfo.subsamples[i]
		}

		// Decrypt the sample
		sampleData := result[mdatOffset+mdatHeaderSize+sampleOffset : mdatOffset+mdatHeaderSize+sampleOffset+int(sample.size)]
		if err := d.decryptSample(sampleData, iv, subsamples); err != nil {
			return nil, fmt.Errorf("decrypt sample %d: %w", i, err)
		}

		sampleOffset += int(sample.size)
	}

	return result, nil
}

// decryptSample decrypts a single sample in-place using AES-CTR
func (d *Decryptor) decryptSample(sample []byte, iv []byte, subsamples []subsampleEntry) error {
	if len(sample) == 0 || len(iv) == 0 {
		return nil
	}

	block, err := aes.NewCipher(d.key)
	if err != nil {
		return err
	}

	// Make a working copy of IV
	ivCopy := make([]byte, 16)
	copy(ivCopy, iv)

	if len(subsamples) == 0 {
		// Full sample encryption
		stream := cipher.NewCTR(block, ivCopy)
		stream.XORKeyStream(sample, sample)
		return nil
	}

	// Subsample encryption
	offset := 0
	for _, sub := range subsamples {
		// Skip clear bytes
		offset += int(sub.clearBytes)

		// Decrypt protected bytes
		if offset+int(sub.protectedBytes) > len(sample) {
			break
		}

		stream := cipher.NewCTR(block, ivCopy)
		stream.XORKeyStream(sample[offset:offset+int(sub.protectedBytes)], sample[offset:offset+int(sub.protectedBytes)])

		// Increment IV counter for next subsample
		blocks := (int(sub.protectedBytes) + 15) / 16
		incrementIV(ivCopy, blocks)

		offset += int(sub.protectedBytes)
	}

	return nil
}
