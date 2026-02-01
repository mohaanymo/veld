package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Checkpoint tracks download progress for resume capability.
type Checkpoint struct {
	URL           string           `json:"url"`
	TempDir       string           `json:"temp_dir"`
	CompletedSegs map[string][]int `json:"completed"` // trackID -> segment indices
	CreatedAt     time.Time        `json:"created_at"`
	mu            sync.Mutex
}

// CheckpointPath returns the checkpoint file path for an output file.
func CheckpointPath(outputPath string) string {
	return outputPath + ".veld.json"
}

// LoadCheckpoint loads a checkpoint from disk if it exists.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No checkpoint exists
		}
		return nil, err
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// NewCheckpoint creates a new checkpoint for a download.
func NewCheckpoint(url, tempDir string) *Checkpoint {
	return &Checkpoint{
		URL:           url,
		TempDir:       tempDir,
		CompletedSegs: make(map[string][]int),
		CreatedAt:     time.Now(),
	}
}

// Save writes the checkpoint to disk atomically.
func (c *Checkpoint) Save(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Write atomically via temp file
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// MarkDone marks a segment as completed.
func (c *Checkpoint) MarkDone(trackID string, index int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.CompletedSegs[trackID] = append(c.CompletedSegs[trackID], index)
}

// IsSegmentDone checks if a segment has been downloaded.
func (c *Checkpoint) IsSegmentDone(trackID string, index int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	indices, ok := c.CompletedSegs[trackID]
	if !ok {
		return false
	}
	for _, i := range indices {
		if i == index {
			return true
		}
	}
	return false
}

// Delete removes the checkpoint file.
func (c *Checkpoint) Delete(path string) error {
	return os.Remove(path)
}

// CleanupTempDir removes the temp directory and its contents.
func (c *Checkpoint) CleanupTempDir() error {
	if c.TempDir == "" {
		return nil
	}
	return os.RemoveAll(c.TempDir)
}

// Matches checks if this checkpoint is for the same URL.
func (c *Checkpoint) Matches(url string) bool {
	return c.URL == url
}

// SegmentPath returns the expected path for a segment in the temp dir.
func (c *Checkpoint) SegmentPath(trackID string, index int) string {
	return filepath.Join(c.TempDir, trackID+"_"+formatIndex(index)+".seg")
}

func formatIndex(i int) string {
	return string(rune('0'+i/10000%10)) + string(rune('0'+i/1000%10)) + string(rune('0'+i/100%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
}
