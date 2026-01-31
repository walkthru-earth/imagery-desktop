package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// QueueState represents the persistent queue state
type QueueState struct {
	TaskOrder  []string `json:"taskOrder"`  // Ordered list of task IDs
	IsRunning  bool     `json:"isRunning"`  // Whether queue is processing
	IsPaused   bool     `json:"isPaused"`   // Whether queue is paused
}

// QueueStatus represents the current queue status for events
type QueueStatus struct {
	IsRunning      bool   `json:"isRunning"`
	IsPaused       bool   `json:"isPaused"`
	CurrentTaskID  string `json:"currentTaskID"`
	TotalTasks     int    `json:"totalTasks"`
	CompletedTasks int    `json:"completedTasks"`
	PendingTasks   int    `json:"pendingTasks"`
}

// TaskExecutor is the interface for task execution (implemented by App)
type TaskExecutor interface {
	ExecuteExportTask(ctx context.Context, task *ExportTask, progressChan chan<- TaskProgress) error
}

// QueueManager manages the export task queue
type QueueManager struct {
	tasks       map[string]*ExportTask
	taskOrder   []string // maintains queue order
	mu          sync.RWMutex
	storagePath string   // ~/.walkthru-earth/imagery-desktop/queue/

	// State
	isRunning bool
	isPaused  bool
	currentTask *ExportTask

	// Channels
	stopWorker  chan struct{}
	pauseWorker chan struct{}
	taskAdded   chan struct{}

	// Context for cancellation
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Executor
	executor TaskExecutor

	// Event emission callback
	onQueueUpdate func(status QueueStatus)
	onTaskProgress func(taskID string, progress TaskProgress)
	onTaskComplete func(taskID string, success bool, err error)
	onNotification func(title, message, notifType string)

	// Concurrency
	maxConcurrent int
	workerWg      sync.WaitGroup
}

// NewQueueManager creates a new queue manager
func NewQueueManager(storagePath string, maxConcurrent int) *QueueManager {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if maxConcurrent > 5 {
		maxConcurrent = 5
	}

	ctx, cancel := context.WithCancel(context.Background())

	qm := &QueueManager{
		tasks:         make(map[string]*ExportTask),
		taskOrder:     make([]string, 0),
		storagePath:   storagePath,
		maxConcurrent: maxConcurrent,
		stopWorker:    make(chan struct{}),
		pauseWorker:   make(chan struct{}),
		taskAdded:     make(chan struct{}, 1),
		ctx:           ctx,
		cancelFunc:    cancel,
	}

	// Load persisted state
	if err := qm.loadState(); err != nil {
		log.Printf("[TaskQueue] Failed to load queue state: %v", err)
	}

	return qm
}

// SetExecutor sets the task executor
func (qm *QueueManager) SetExecutor(executor TaskExecutor) {
	qm.executor = executor
}

// SetCallbacks sets event callbacks
func (qm *QueueManager) SetCallbacks(
	onQueueUpdate func(QueueStatus),
	onTaskProgress func(string, TaskProgress),
	onTaskComplete func(string, bool, error),
	onNotification func(string, string, string),
) {
	qm.onQueueUpdate = onQueueUpdate
	qm.onTaskProgress = onTaskProgress
	qm.onTaskComplete = onTaskComplete
	qm.onNotification = onNotification
}

// getStoragePaths returns paths for queue storage
func (qm *QueueManager) getStoragePaths() (queueFile, tasksDir string) {
	queueFile = filepath.Join(qm.storagePath, "queue.json")
	tasksDir = filepath.Join(qm.storagePath, "tasks")
	return
}

// loadState loads the queue state from disk
func (qm *QueueManager) loadState() error {
	queueFile, tasksDir := qm.getStoragePaths()

	// Load queue state
	if data, err := os.ReadFile(queueFile); err == nil {
		var state QueueState
		if err := json.Unmarshal(data, &state); err == nil {
			qm.taskOrder = state.TaskOrder
			qm.isPaused = state.IsPaused
			// Don't restore isRunning - let user explicitly start
		}
	}

	// Load individual tasks
	if entries, err := os.ReadDir(tasksDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
				taskPath := filepath.Join(tasksDir, entry.Name())
				if task, err := LoadFromFile(taskPath); err == nil {
					qm.tasks[task.ID] = task
				} else {
					log.Printf("[TaskQueue] Failed to load task %s: %v", entry.Name(), err)
				}
			}
		}
	}

	// Clean up taskOrder - remove any IDs that don't have corresponding tasks
	validOrder := make([]string, 0)
	for _, id := range qm.taskOrder {
		if _, exists := qm.tasks[id]; exists {
			validOrder = append(validOrder, id)
		}
	}
	qm.taskOrder = validOrder

	// Add any tasks not in order (shouldn't happen, but safety)
	for id := range qm.tasks {
		found := false
		for _, orderId := range qm.taskOrder {
			if orderId == id {
				found = true
				break
			}
		}
		if !found {
			qm.taskOrder = append(qm.taskOrder, id)
		}
	}

	log.Printf("[TaskQueue] Loaded %d tasks from disk", len(qm.tasks))
	return nil
}

// saveState saves the queue state to disk
func (qm *QueueManager) saveState() error {
	queueFile, _ := qm.getStoragePaths()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(queueFile), 0755); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}

	state := QueueState{
		TaskOrder: qm.taskOrder,
		IsRunning: qm.isRunning,
		IsPaused:  qm.isPaused,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal queue state: %w", err)
	}

	if err := os.WriteFile(queueFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write queue state: %w", err)
	}

	return nil
}

// saveTask saves a single task to disk
func (qm *QueueManager) saveTask(task *ExportTask) error {
	_, tasksDir := qm.getStoragePaths()
	return task.SaveToFile(tasksDir)
}

// AddTask adds a new task to the queue
func (qm *QueueManager) AddTask(task *ExportTask) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if task.ID == "" {
		task.ID = generateTaskID()
	}

	qm.tasks[task.ID] = task
	qm.taskOrder = append(qm.taskOrder, task.ID)

	// Save to disk
	if err := qm.saveTask(task); err != nil {
		return err
	}
	if err := qm.saveState(); err != nil {
		return err
	}

	// Notify
	qm.emitQueueUpdate()

	// Signal worker
	select {
	case qm.taskAdded <- struct{}{}:
	default:
	}

	log.Printf("[TaskQueue] Added task: %s (%s)", task.Name, task.ID)
	return nil
}

// GetTask returns a task by ID
func (qm *QueueManager) GetTask(id string) (*ExportTask, error) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	task, exists := qm.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	return task, nil
}

// GetAllTasks returns all tasks in order
func (qm *QueueManager) GetAllTasks() []*ExportTask {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	result := make([]*ExportTask, 0, len(qm.taskOrder))
	for _, id := range qm.taskOrder {
		if task, exists := qm.tasks[id]; exists {
			result = append(result, task)
		}
	}

	return result
}

// GetPendingTasks returns tasks that are pending (not started, not completed)
func (qm *QueueManager) GetPendingTasks() []*ExportTask {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	result := make([]*ExportTask, 0)
	for _, id := range qm.taskOrder {
		if task, exists := qm.tasks[id]; exists {
			if task.Status == TaskStatusPending {
				result = append(result, task)
			}
		}
	}

	return result
}

// UpdateTask updates a task's properties
func (qm *QueueManager) UpdateTask(id string, updates map[string]interface{}) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	task, exists := qm.tasks[id]
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	// Only allow updates to pending tasks
	if task.Status != TaskStatusPending {
		return fmt.Errorf("cannot update task that is not pending")
	}

	// Apply updates
	if name, ok := updates["name"].(string); ok {
		task.Name = name
	}
	if priority, ok := updates["priority"].(float64); ok {
		task.Priority = int(priority)
	}
	if format, ok := updates["format"].(string); ok {
		task.Format = format
	}
	if videoExport, ok := updates["videoExport"].(bool); ok {
		task.VideoExport = videoExport
	}

	// Save to disk
	if err := qm.saveTask(task); err != nil {
		return err
	}

	qm.emitQueueUpdate()
	return nil
}

// DeleteTask removes a task from the queue
func (qm *QueueManager) DeleteTask(id string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	task, exists := qm.tasks[id]
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	// Can't delete running task
	if task.Status == TaskStatusRunning {
		return fmt.Errorf("cannot delete running task - cancel it first")
	}

	// Remove from order
	newOrder := make([]string, 0, len(qm.taskOrder)-1)
	for _, taskId := range qm.taskOrder {
		if taskId != id {
			newOrder = append(newOrder, taskId)
		}
	}
	qm.taskOrder = newOrder

	// Delete from map
	delete(qm.tasks, id)

	// Delete from disk
	_, tasksDir := qm.getStoragePaths()
	task.DeleteFile(tasksDir)

	// Save state
	qm.saveState()

	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Deleted task: %s", id)
	return nil
}

// ReorderTask moves a task to a new position in the queue
func (qm *QueueManager) ReorderTask(id string, newIndex int) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Find current index
	currentIndex := -1
	for i, taskId := range qm.taskOrder {
		if taskId == id {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return fmt.Errorf("task not found: %s", id)
	}

	// Validate new index
	if newIndex < 0 {
		newIndex = 0
	}
	if newIndex >= len(qm.taskOrder) {
		newIndex = len(qm.taskOrder) - 1
	}

	// Remove from current position
	newOrder := make([]string, 0, len(qm.taskOrder))
	for i, taskId := range qm.taskOrder {
		if i != currentIndex {
			newOrder = append(newOrder, taskId)
		}
	}

	// Insert at new position
	finalOrder := make([]string, 0, len(qm.taskOrder))
	for i, taskId := range newOrder {
		if i == newIndex {
			finalOrder = append(finalOrder, id)
		}
		finalOrder = append(finalOrder, taskId)
	}
	if newIndex >= len(newOrder) {
		finalOrder = append(finalOrder, id)
	}

	qm.taskOrder = finalOrder

	// Save state
	qm.saveState()

	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Reordered task %s to position %d", id, newIndex)
	return nil
}

// CancelTask cancels a running or pending task
func (qm *QueueManager) CancelTask(id string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	task, exists := qm.tasks[id]
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	if task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed || task.Status == TaskStatusCancelled {
		return fmt.Errorf("task already finished")
	}

	task.MarkCancelled()

	// If this is the current task, cancel its context
	if qm.currentTask != nil && qm.currentTask.ID == id {
		qm.cancelFunc()
		// Create new context for next task
		qm.ctx, qm.cancelFunc = context.WithCancel(context.Background())
	}

	// Save to disk
	qm.saveTask(task)

	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Cancelled task: %s", id)
	return nil
}

// StartQueue begins processing tasks
func (qm *QueueManager) StartQueue() error {
	qm.mu.Lock()

	if qm.isRunning && !qm.isPaused {
		qm.mu.Unlock()
		return fmt.Errorf("queue is already running")
	}

	qm.isRunning = true
	qm.isPaused = false
	qm.saveState()
	qm.mu.Unlock()

	// Start worker if not already running
	go qm.worker()

	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Queue started")
	return nil
}

// PauseQueue pauses the queue after the current task completes
func (qm *QueueManager) PauseQueue() error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if !qm.isRunning {
		return fmt.Errorf("queue is not running")
	}

	qm.isPaused = true
	qm.saveState()

	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Queue paused (will stop after current task)")
	return nil
}

// StopQueue stops the queue immediately
func (qm *QueueManager) StopQueue() {
	qm.mu.Lock()
	qm.isRunning = false
	qm.isPaused = false
	qm.saveState()
	qm.mu.Unlock()

	// Cancel current task
	qm.cancelFunc()
	qm.ctx, qm.cancelFunc = context.WithCancel(context.Background())

	// Signal worker to stop
	select {
	case qm.stopWorker <- struct{}{}:
	default:
	}

	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Queue stopped")
}

// GetStatus returns the current queue status
func (qm *QueueManager) GetStatus() QueueStatus {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	completed := 0
	pending := 0
	for _, task := range qm.tasks {
		switch task.Status {
		case TaskStatusCompleted:
			completed++
		case TaskStatusPending:
			pending++
		}
	}

	currentTaskID := ""
	if qm.currentTask != nil {
		currentTaskID = qm.currentTask.ID
	}

	return QueueStatus{
		IsRunning:      qm.isRunning,
		IsPaused:       qm.isPaused,
		CurrentTaskID:  currentTaskID,
		TotalTasks:     len(qm.tasks),
		CompletedTasks: completed,
		PendingTasks:   pending,
	}
}

// worker processes tasks in the background
func (qm *QueueManager) worker() {
	log.Printf("[TaskQueue] Worker started")
	defer log.Printf("[TaskQueue] Worker stopped")

	for {
		// Check if we should stop
		select {
		case <-qm.stopWorker:
			return
		default:
		}

		qm.mu.Lock()
		if !qm.isRunning || qm.isPaused {
			qm.mu.Unlock()
			return
		}

		// Find next pending task (respecting priority)
		var nextTask *ExportTask
		for _, id := range qm.taskOrder {
			task := qm.tasks[id]
			if task.Status == TaskStatusPending {
				if nextTask == nil || task.Priority > nextTask.Priority {
					nextTask = task
				}
			}
		}

		if nextTask == nil {
			// No more tasks
			qm.isRunning = false
			qm.saveState()
			qm.mu.Unlock()

			// Send completion notification
			if qm.onNotification != nil {
				completed := 0
				for _, t := range qm.tasks {
					if t.Status == TaskStatusCompleted {
						completed++
					}
				}
				qm.onNotification("Export Queue Complete",
					fmt.Sprintf("%d tasks finished", completed), "success")
			}

			qm.emitQueueUpdate()
			return
		}

		qm.currentTask = nextTask
		nextTask.MarkStarted()
		qm.saveTask(nextTask)
		qm.mu.Unlock()

		qm.emitQueueUpdate()

		// Execute task
		log.Printf("[TaskQueue] Executing task: %s (%s)", nextTask.Name, nextTask.ID)

		progressChan := make(chan TaskProgress, 10)
		go func() {
			for progress := range progressChan {
				qm.mu.Lock()
				nextTask.Progress = progress
				qm.saveTask(nextTask)
				qm.mu.Unlock()

				if qm.onTaskProgress != nil {
					qm.onTaskProgress(nextTask.ID, progress)
				}
			}
		}()

		var execErr error
		if qm.executor != nil {
			execErr = qm.executor.ExecuteExportTask(qm.ctx, nextTask, progressChan)
		} else {
			execErr = fmt.Errorf("no executor configured")
		}
		close(progressChan)

		qm.mu.Lock()
		if execErr != nil {
			if qm.ctx.Err() != nil {
				// Context was cancelled
				nextTask.MarkCancelled()
			} else {
				nextTask.MarkFailed(execErr)
				log.Printf("[TaskQueue] Task failed: %s - %v", nextTask.ID, execErr)

				if qm.onNotification != nil {
					qm.onNotification("Export Failed",
						fmt.Sprintf("Task '%s' failed: %v", nextTask.Name, execErr), "error")
				}
			}
		} else {
			nextTask.MarkCompleted(nextTask.OutputPath)
			log.Printf("[TaskQueue] Task completed: %s", nextTask.ID)
		}
		qm.saveTask(nextTask)
		qm.currentTask = nil
		qm.mu.Unlock()

		if qm.onTaskComplete != nil {
			qm.onTaskComplete(nextTask.ID, execErr == nil, execErr)
		}

		qm.emitQueueUpdate()

		// Reset context for next task
		qm.ctx, qm.cancelFunc = context.WithCancel(context.Background())
	}
}

// emitQueueUpdate emits a queue update event
func (qm *QueueManager) emitQueueUpdate() {
	if qm.onQueueUpdate != nil {
		qm.onQueueUpdate(qm.GetStatus())
	}
}

// SortByPriority sorts tasks by priority (higher first)
func (qm *QueueManager) SortByPriority() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Get pending tasks
	pendingTasks := make([]*ExportTask, 0)
	nonPendingOrder := make([]string, 0)

	for _, id := range qm.taskOrder {
		task := qm.tasks[id]
		if task.Status == TaskStatusPending {
			pendingTasks = append(pendingTasks, task)
		} else {
			nonPendingOrder = append(nonPendingOrder, id)
		}
	}

	// Sort pending by priority
	sort.Slice(pendingTasks, func(i, j int) bool {
		return pendingTasks[i].Priority > pendingTasks[j].Priority
	})

	// Rebuild order: non-pending first (maintain order), then pending (sorted)
	newOrder := nonPendingOrder
	for _, task := range pendingTasks {
		newOrder = append(newOrder, task.ID)
	}
	qm.taskOrder = newOrder

	qm.saveState()
	qm.emitQueueUpdate()
}

// ClearCompleted removes all completed tasks
func (qm *QueueManager) ClearCompleted() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	_, tasksDir := qm.getStoragePaths()

	newOrder := make([]string, 0)
	for _, id := range qm.taskOrder {
		task := qm.tasks[id]
		if task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed || task.Status == TaskStatusCancelled {
			task.DeleteFile(tasksDir)
			delete(qm.tasks, id)
		} else {
			newOrder = append(newOrder, id)
		}
	}
	qm.taskOrder = newOrder

	qm.saveState()
	qm.emitQueueUpdate()
	log.Printf("[TaskQueue] Cleared completed/failed/cancelled tasks")
}

// Close shuts down the queue manager
func (qm *QueueManager) Close() {
	qm.StopQueue()
	qm.workerWg.Wait()
}
