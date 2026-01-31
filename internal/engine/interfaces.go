package engine

import (
	"context"

	"github.com/mohaanymo/veld/models"
)

// ProgressUpdate represents a download progress update.
type ProgressUpdate struct {
	SegmentIndex int
	TrackID      string
	BytesLoaded  int64
	Completed    bool
	Error        error
}

// Decryptor interface for pluggable decryption.
type Decryptor interface {
	CanDecrypt(encryptionType string) bool
	Decrypt(data []byte, key []byte, iv []byte) ([]byte, error)
	ParseKey(keyString string) (key []byte, iv []byte, err error)
}

// NoOpDecryptor is a placeholder that passes data through unchanged.
type NoOpDecryptor struct{}

func (d *NoOpDecryptor) CanDecrypt(encryptionType string) bool       { return false }
func (d *NoOpDecryptor) Decrypt(data, key, iv []byte) ([]byte, error) { return data, nil }
func (d *NoOpDecryptor) ParseKey(keyString string) ([]byte, []byte, error) {
	return nil, nil, nil
}

// Muxer interface for final file assembly.
type Muxer interface {
	Mux(ctx context.Context, tracks []*models.Track, outputPath string, format ContainerFormat) error
	SupportedFormats() []ContainerFormat
}

// ContainerFormat represents output container formats.
type ContainerFormat string

const (
	FormatMP4  ContainerFormat = "mp4"
	FormatMKV  ContainerFormat = "mkv"
	FormatTS   ContainerFormat = "ts"
	FormatWebM ContainerFormat = "webm"
)
