package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mohaanymo/veld/internal/models"
)

// SegmentTask represents a download task for the worker pool.
type SegmentTask struct {
	Segment *models.Segment
	Track   *models.Track
	Headers map[string]string
}

// WorkerPool manages concurrent segment downloads.
type WorkerPool struct {
	workers    int
	client     *http.Client
	progressCh chan<- ProgressUpdate

	taskQueue  chan *SegmentTask
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc

	// Stats
	completed  atomic.Int64
	totalBytes atomic.Int64
	failed     atomic.Int64
	startTime  time.Time
	errors     []error
	errorsMu   sync.Mutex

	// Config
	maxRetries int
	verbose    bool
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(workers int, client *http.Client, progressCh chan<- ProgressUpdate) *WorkerPool {
	return &WorkerPool{
		workers:    workers,
		client:     client,
		progressCh: progressCh,
		taskQueue:  make(chan *SegmentTask, workers*4),
		maxRetries: 5,
	}
}

// SetVerbose enables verbose error logging.
func (p *WorkerPool) SetVerbose(v bool) {
	p.verbose = v
}

// Start launches the worker goroutines.
func (p *WorkerPool) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.startTime = time.Now()

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// worker is the main download loop for each worker goroutine.
func (p *WorkerPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case task, ok := <-p.taskQueue:
			if !ok {
				return
			}
			p.downloadSegment(task)
		}
	}
}

// downloadSegment performs the actual HTTP download with retries.
func (p *WorkerPool) downloadSegment(task *SegmentTask) {
	var lastErr error

	for attempt := 0; attempt < p.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s, 4s, 8s
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-p.ctx.Done():
				p.sendProgress(task, 0, p.ctx.Err())
				return
			}
		}

		data, err := p.doRequest(task)
		if err == nil {
			task.Segment.Data = data
			task.Segment.Size = int64(len(data))
			p.completed.Add(1)
			p.totalBytes.Add(task.Segment.Size)
			p.sendProgress(task, task.Segment.Size, nil)
			return
		}

		lastErr = err
		if p.verbose {
			fmt.Printf("Segment %d attempt %d failed: %v\n", task.Segment.Index, attempt+1, err)
		}
	}

	p.failed.Add(1)
	p.errorsMu.Lock()
	p.errors = append(p.errors, lastErr)
	p.errorsMu.Unlock()

	err := fmt.Errorf("segment %d: %w (after %d attempts)", task.Segment.Index, lastErr, p.maxRetries)
	p.sendProgress(task, 0, err)
}

// doRequest performs a single HTTP request.
func (p *WorkerPool) doRequest(task *SegmentTask) ([]byte, error) {
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, task.Segment.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}

	if task.Segment.ByteRange != nil {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d",
			task.Segment.ByteRange.Start, task.Segment.ByteRange.End))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// sendProgress sends a progress update.
func (p *WorkerPool) sendProgress(task *SegmentTask, bytes int64, err error) {
	select {
	case p.progressCh <- ProgressUpdate{
		SegmentIndex: task.Segment.Index,
		TrackID:      task.Track.ID,
		BytesLoaded:  bytes,
		Completed:    err == nil,
		Error:        err,
	}:
	case <-p.ctx.Done():
	}
}

// Submit adds a task to the queue.
func (p *WorkerPool) Submit(task *SegmentTask) {
	p.taskQueue <- task
}

// Wait blocks until all tasks are complete.
func (p *WorkerPool) Wait() error {
	close(p.taskQueue)
	p.wg.Wait()

	failed := p.failed.Load()
	completed := p.completed.Load()
	total := failed + completed

	if failed > 0 && total > 0 {
		failRate := float64(failed) / float64(total)
		if failRate > 0.01 { // Allow up to 1% failure rate
			return fmt.Errorf("%d/%d segments failed (%.1f%%)", failed, total, failRate*100)
		}
		if p.verbose {
			fmt.Printf("Warning: %d segments failed (%.2f%% fail rate)\n", failed, failRate*100)
		}
	}

	return nil
}

// Stop gracefully shuts down the pool.
func (p *WorkerPool) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Stats returns current download statistics.
func (p *WorkerPool) Stats() (completed int64, totalBytes int64, elapsed time.Duration) {
	return p.completed.Load(), p.totalBytes.Load(), time.Since(p.startTime)
}
