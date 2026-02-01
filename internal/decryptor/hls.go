package decryptor

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// HLSDecryptor handles AES-128 decryption for HLS streams.
type HLSDecryptor struct {
	keyCache map[string][]byte
	mu       sync.RWMutex
	client   *http.Client
	headers  map[string]string
}

// NewHLSDecryptor creates a new HLS decryptor.
func NewHLSDecryptor(client *http.Client, headers map[string]string) *HLSDecryptor {
	if client == nil {
		client = http.DefaultClient
	}
	return &HLSDecryptor{
		keyCache: make(map[string][]byte),
		client:   client,
		headers:  headers,
	}
}

// FetchKey retrieves the decryption key from the given URI.
// Keys are cached by URI to avoid redundant fetches.
func (d *HLSDecryptor) FetchKey(ctx context.Context, keyURI string) ([]byte, error) {
	d.mu.RLock()
	if key, ok := d.keyCache[keyURI]; ok {
		d.mu.RUnlock()
		return key, nil
	}
	d.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, keyURI, nil)
	if err != nil {
		return nil, fmt.Errorf("create key request: %w", err)
	}

	for k, v := range d.headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("key fetch failed: HTTP %d", resp.StatusCode)
	}

	key, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}

	if len(key) != 16 {
		return nil, fmt.Errorf("invalid key length: expected 16 bytes, got %d", len(key))
	}

	d.mu.Lock()
	d.keyCache[keyURI] = key
	d.mu.Unlock()

	return key, nil
}

// Decrypt decrypts data using AES-128-CBC with the given key and IV.
// If iv is nil, it defaults to the first 16 bytes of the data (or zero IV).
func (d *HLSDecryptor) Decrypt(data, key, iv []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("invalid key length: %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Use provided IV or default to zero IV
	if len(iv) != aes.BlockSize {
		iv = make([]byte, aes.BlockSize)
	}

	// Data must be multiple of block size
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext not multiple of block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted := make([]byte, len(data))
	mode.CryptBlocks(decrypted, data)

	// Remove PKCS7 padding
	decrypted = pkcs7Unpad(decrypted)

	return decrypted, nil
}

// ParseIV parses a hex-encoded IV string (from #EXT-X-KEY IV attribute).
// Format: 0x... or plain hex string
func ParseIV(ivStr string) ([]byte, error) {
	if ivStr == "" {
		return nil, nil
	}

	// Remove 0x prefix if present
	if len(ivStr) >= 2 && ivStr[:2] == "0x" {
		ivStr = ivStr[2:]
	}

	iv, err := hex.DecodeString(ivStr)
	if err != nil {
		return nil, fmt.Errorf("parse IV: %w", err)
	}

	// IV should be 16 bytes, pad with zeros if shorter
	if len(iv) < 16 {
		padded := make([]byte, 16)
		copy(padded[16-len(iv):], iv)
		iv = padded
	}

	return iv[:16], nil
}

// SegmentIV creates a default IV from segment sequence number.
// HLS spec: if no IV is specified, use the segment sequence number as a big-endian 128-bit value.
func SegmentIV(sequenceNumber int) []byte {
	iv := make([]byte, 16)
	// Big-endian representation of sequence number
	for i := 15; i >= 0 && sequenceNumber > 0; i-- {
		iv[i] = byte(sequenceNumber & 0xff)
		sequenceNumber >>= 8
	}
	return iv
}

// pkcs7Unpad removes PKCS7 padding from decrypted data.
func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	padLen := int(data[len(data)-1])
	if padLen > len(data) || padLen > aes.BlockSize {
		return data // Invalid padding, return as-is
	}
	// Verify padding bytes
	for i := 0; i < padLen; i++ {
		if data[len(data)-1-i] != byte(padLen) {
			return data // Invalid padding
		}
	}
	return data[:len(data)-padLen]
}
