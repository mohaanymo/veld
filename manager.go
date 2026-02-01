package veld

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TaskState represents the current state of a download task.
type TaskState int

const (
	TaskPending TaskState = iota
	TaskParsing
	TaskDownloading
	TaskMuxing
	TaskCompleted
	TaskFailed
	TaskCanceled
)

func (s TaskState) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskParsing:
		return "parsing"
	case TaskDownloading:
		return "downloading"
	case TaskMuxing:
		return "muxing"
	case TaskCompleted:
		return "completed"
	case TaskFailed:
		return "failed"
	case TaskCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// Task represents a download task in the queue.
type Task struct {
	ID            string
	URL           string
	FileName    string
	Options       []Option
	State         TaskState
	Error         error
	Progress      TaskProgress
	CreatedAt     time.Time
	StartedAt     time.Time
	CompletedAt   time.Time
	Tracks        []*Track
	SelectedTracks []*Track

	// Internal
	downloader *Downloader
	cancel     context.CancelFunc
	mu         sync.RWMutex
}

// TaskProgress holds progress information for a task.
type TaskProgress struct {
	TotalSegments     int
	CompletedSegments int
	TotalBytes        int64
	DownloadedBytes   int64
	Speed             float64 // bytes per second
	ETA               time.Duration
	CurrentTrack      string
}

// Percent returns the download progress as a percentage.
func (p TaskProgress) Percent() float64 {
	if p.TotalSegments == 0 {
		return 0
	}
	return float64(p.CompletedSegments) / float64(p.TotalSegments) * 100
}

// Manager handles queued downloads with concurrency control.
type Manager struct {
	title string
	maxConcurrent int
	tasks         sync.Map // map[string]*Task
	taskOrder     []string
	orderMu       sync.RWMutex

	queue      chan *Task
	active     atomic.Int32
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	running    atomic.Bool

	// Callbacks
	onStateChange func(task *Task)
	onProgress    func(task *Task)
	onComplete    func(task *Task)
	onError       func(task *Task, err error)

	// Default options applied to all tasks
	defaultOptions []Option

	mu sync.RWMutex
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithTitle sets the dowmloader manger title.
func WithTitle(t string) ManagerOption {
	return func(m *Manager) {
		m.title = t
	}
}

// WithMaxConcurrent sets the maximum number of concurrent downloads.
func WithMaxConcurrent(n int) ManagerOption {
	return func(m *Manager) {
		if n < 1 {
			n = 1
		}
		if n > 20 {
			n = 20
		}
		m.maxConcurrent = n
	}
}

// WithDefaultOptions sets default options applied to all tasks.
func WithDefaultOptions(opts ...Option) ManagerOption {
	return func(m *Manager) {
		m.defaultOptions = opts
	}
}

// WithOnStateChange sets a callback for task state changes.
func WithOnStateChange(fn func(task *Task)) ManagerOption {
	return func(m *Manager) {
		m.onStateChange = fn
	}
}

// WithOnProgress sets a callback for progress updates.
func WithOnProgress(fn func(task *Task)) ManagerOption {
	return func(m *Manager) {
		m.onProgress = fn
	}
}

// WithOnComplete sets a callback for task completion.
func WithOnComplete(fn func(task *Task)) ManagerOption {
	return func(m *Manager) {
		m.onComplete = fn
	}
}

// WithOnError sets a callback for task errors.
func WithOnError(fn func(task *Task, err error)) ManagerOption {
	return func(m *Manager) {
		m.onError = fn
	}
}

// NewManager creates a new download manager.
func NewManager(opts ...ManagerOption) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		maxConcurrent: 3,
		queue:         make(chan *Task, 1000),
		ctx:           ctx,
		cancel:        cancel,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Start begins processing the download queue.
func (m *Manager) Start() {
	if m.running.Swap(true) {
		return // Already running
	}

	// Start worker goroutines
	for i := 0; i < m.maxConcurrent; i++ {
		m.wg.Add(1)
		go m.worker()
	}
}

// Stop gracefully stops the manager and waits for active downloads.
func (m *Manager) Stop() {
	if !m.running.Swap(false) {
		return // Not running
	}

	close(m.queue)
	m.cancel()
	m.wg.Wait()
}

// worker processes tasks from the queue.
func (m *Manager) worker() {
	defer m.wg.Done()

	for task := range m.queue {
		select {
		case <-m.ctx.Done():
			return
		default:
			m.active.Add(1)
			m.processTask(task)
			m.active.Add(-1)
		}
	}
}

// AddTask adds a new download task to the queue.
func (m *Manager) AddTask(id, url, filename string, opts ...Option) (*Task, error) {
	if !m.running.Load() {
		return nil, fmt.Errorf("manager not started, call Start() first")
	}

	// Check for duplicate ID
	if _, exists := m.tasks.Load(id); exists {
		return nil, fmt.Errorf("task with ID %q already exists", id)
	}

	// Combine default options with task-specific options
	allOpts := append(m.defaultOptions, opts...)

	task := &Task{
		ID:         id,
		URL:        url,
		FileName: filename,
		Options:    allOpts,
		State:      TaskPending,
		CreatedAt:  time.Now(),
	}

	m.tasks.Store(id, task)
	m.orderMu.Lock()
	m.taskOrder = append(m.taskOrder, id)
	m.orderMu.Unlock()

	// Queue the task
	select {
	case m.queue <- task:
	default:
		return nil, fmt.Errorf("queue is full")
	}

	return task, nil
}

// GetTask returns a task by ID.
func (m *Manager) GetTask(id string) *Task {
	if t, ok := m.tasks.Load(id); ok {
		return t.(*Task)
	}
	return nil
}

// GetAllTasks returns all tasks in order.
func (m *Manager) GetAllTasks() []*Task {
	m.orderMu.RLock()
	defer m.orderMu.RUnlock()

	tasks := make([]*Task, 0, len(m.taskOrder))
	for _, id := range m.taskOrder {
		if t, ok := m.tasks.Load(id); ok {
			tasks = append(tasks, t.(*Task))
		}
	}
	return tasks
}

// GetActiveTasks returns currently downloading tasks.
func (m *Manager) GetActiveTasks() []*Task {
	var active []*Task
	m.tasks.Range(func(_, value any) bool {
		task := value.(*Task)
		if task.State == TaskDownloading || task.State == TaskParsing || task.State == TaskMuxing {
			active = append(active, task)
		}
		return true
	})
	return active
}

// GetPendingCount returns the number of pending tasks.
func (m *Manager) GetPendingCount() int {
	count := 0
	m.tasks.Range(func(_, value any) bool {
		if value.(*Task).State == TaskPending {
			count++
		}
		return true
	})
	return count
}

// CancelTask cancels a specific task.
func (m *Manager) CancelTask(id string) error {
	t, ok := m.tasks.Load(id)
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	task := t.(*Task)
	task.mu.Lock()
	defer task.mu.Unlock()

	if task.State == TaskCompleted || task.State == TaskFailed {
		return fmt.Errorf("task already finished")
	}

	if task.cancel != nil {
		task.cancel()
	}
	task.State = TaskCanceled

	if m.onStateChange != nil {
		m.onStateChange(task)
	}

	return nil
}

// RemoveTask removes a completed/failed/canceled task.
func (m *Manager) RemoveTask(id string) error {
	t, ok := m.tasks.Load(id)
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	task := t.(*Task)
	if task.State == TaskDownloading || task.State == TaskParsing || task.State == TaskMuxing {
		return fmt.Errorf("cannot remove active task")
	}

	m.tasks.Delete(id)

	m.orderMu.Lock()
	for i, tid := range m.taskOrder {
		if tid == id {
			m.taskOrder = append(m.taskOrder[:i], m.taskOrder[i+1:]...)
			break
		}
	}
	m.orderMu.Unlock()

	return nil
}

// Stats returns current manager statistics.
func (m *Manager) Stats() ManagerStats {
	stats := ManagerStats{}
	m.tasks.Range(func(_, value any) bool {
		task := value.(*Task)
		stats.Total++
		switch task.State {
		case TaskPending:
			stats.Pending++
		case TaskDownloading, TaskParsing, TaskMuxing:
			stats.Active++
		case TaskCompleted:
			stats.Completed++
		case TaskFailed:
			stats.Failed++
		case TaskCanceled:
			stats.Canceled++
		}
		return true
	})
	return stats
}

// ManagerStats holds manager statistics.
type ManagerStats struct {
	Total     int
	Pending   int
	Active    int
	Completed int
	Failed    int
	Canceled  int
}

// processTask handles downloading a single task.
func (m *Manager) processTask(task *Task) {
	ctx, cancel := context.WithCancel(m.ctx)
	task.cancel = cancel
	defer cancel()

	task.mu.Lock()
	task.StartedAt = time.Now()
	task.State = TaskParsing
	task.mu.Unlock()

	m.notifyStateChange(task)

	// Create downloader with task options
	opts := append([]Option{
		WithURL(task.URL),
		WithFileName(task.FileName),
	}, task.Options...)

	d, err := New(opts...)
	if err != nil {
		m.failTask(task, fmt.Errorf("create downloader: %w", err))
		return
	}
	task.downloader = d
	defer d.Close()

	// Parse manifest
	if err := d.Parse(ctx); err != nil {
		m.failTask(task, fmt.Errorf("parse manifest: %w", err))
		return
	}

	// Store tracks info
	task.mu.Lock()
	task.Tracks = d.Tracks()
	task.mu.Unlock()

	// Select tracks
	if err := d.SelectTracks(); err != nil {
		m.failTask(task, fmt.Errorf("select tracks: %w", err))
		return
	}

	task.mu.Lock()
	task.SelectedTracks = d.SelectedTracks()
	task.State = TaskDownloading

	// Calculate total segments
	totalSegs := 0
	for _, t := range task.SelectedTracks {
		totalSegs += t.SegmentCount()
	}
	task.Progress.TotalSegments = totalSegs
	task.mu.Unlock()

	m.notifyStateChange(task)

	// Monitor progress
	startTime := time.Now()
	go func() {
		completed := 0
		var totalBytes int64
		for p := range d.Progress() {
			if p.Completed {
				completed++
				totalBytes += p.BytesLoaded

				task.mu.Lock()
				task.Progress.CompletedSegments = completed
				task.Progress.DownloadedBytes = totalBytes

				elapsed := time.Since(startTime).Seconds()
				if elapsed > 0 {
					task.Progress.Speed = float64(totalBytes) / elapsed
				}

				remaining := task.Progress.TotalSegments - completed
				if task.Progress.Speed > 0 && completed > 0 {
					avgSize := float64(totalBytes) / float64(completed)
					task.Progress.ETA = time.Duration(float64(remaining) * avgSize / task.Progress.Speed * float64(time.Second))
				}
				task.Progress.CurrentTrack = p.TrackID
				task.mu.Unlock()

				if m.onProgress != nil {
					m.onProgress(task)
				}
			}
		}
	}()

	// Download
	if err := d.Download(ctx); err != nil {
		if ctx.Err() != nil {
			task.mu.Lock()
			task.State = TaskCanceled
			task.mu.Unlock()
			m.notifyStateChange(task)
			return
		}
		m.failTask(task, fmt.Errorf("download: %w", err))
		return
	}

	// Success
	task.mu.Lock()
	task.State = TaskCompleted
	task.CompletedAt = time.Now()
	task.mu.Unlock()

	m.notifyStateChange(task)

	if m.onComplete != nil {
		m.onComplete(task)
	}
}

func (m *Manager) failTask(task *Task, err error) {
	task.mu.Lock()
	task.State = TaskFailed
	task.Error = err
	task.CompletedAt = time.Now()
	task.mu.Unlock()

	m.notifyStateChange(task)

	if m.onError != nil {
		m.onError(task, err)
	}
}

func (m *Manager) notifyStateChange(task *Task) {
	if m.onStateChange != nil {
		m.onStateChange(task)
	}
}

// WaitForTask blocks until a specific task completes.
func (m *Manager) WaitForTask(id string) error {
	for {
		task := m.GetTask(id)
		if task == nil {
			return fmt.Errorf("task %q not found", id)
		}

		task.mu.RLock()
		state := task.State
		err := task.Error
		task.mu.RUnlock()

		switch state {
		case TaskCompleted:
			return nil
		case TaskFailed:
			return err
		case TaskCanceled:
			return fmt.Errorf("task canceled")
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// WaitAll blocks until all tasks complete.
func (m *Manager) WaitAll() {
	for {
		stats := m.Stats()
		if stats.Pending == 0 && stats.Active == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}