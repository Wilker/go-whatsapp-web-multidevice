package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	chatExportJobStatusQueued    = "queued"
	chatExportJobStatusRunning   = "running"
	chatExportJobStatusCompleted = "completed"
	chatExportJobStatusFailed    = "failed"
)

type chatExportProgressSnapshot struct {
	Phase         string         `json:"phase,omitempty"`
	StatusMessage string         `json:"status_message,omitempty"`
	Chat          map[string]any `json:"chat,omitempty"`
	Counters      map[string]any `json:"counters,omitempty"`
	Files         map[string]any `json:"files,omitempty"`
}

type chatExportProgressReporter func(chatExportProgressSnapshot)

type chatExportAsyncJob struct {
	JobID         string         `json:"job_id"`
	Status        string         `json:"status"`
	StatusMessage string         `json:"status_message,omitempty"`
	CreatedAt     string         `json:"created_at"`
	StartedAt     string         `json:"started_at,omitempty"`
	UpdatedAt     string         `json:"updated_at"`
	FinishedAt    string         `json:"finished_at,omitempty"`
	Request       map[string]any `json:"request,omitempty"`
	Chat          map[string]any `json:"chat,omitempty"`
	Progress      map[string]any `json:"progress,omitempty"`
	Result        map[string]any `json:"result,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Error         string         `json:"error,omitempty"`
}

type chatExportAsyncJobRunner func(jobID string, report chatExportProgressReporter) (map[string]any, string, error)

type chatExportJobManager struct {
	mu      sync.RWMutex
	baseDir string
	jobs    map[string]*chatExportAsyncJob
	nowFn   func() time.Time
}

func newChatExportJobManager(baseDir string) *chatExportJobManager {
	manager := &chatExportJobManager{
		baseDir: filepath.Clean(strings.TrimSpace(baseDir)),
		jobs:    make(map[string]*chatExportAsyncJob),
		nowFn:   time.Now,
	}

	if err := os.MkdirAll(manager.baseDir, 0o700); err != nil {
		logrus.WithError(err).Errorf("Failed to create async export jobs directory %s", manager.baseDir)
		return manager
	}

	manager.loadPersistedJobs()
	return manager
}

func (m *chatExportJobManager) Start(request map[string]any, runner chatExportAsyncJobRunner) (*chatExportAsyncJob, error) {
	if runner == nil {
		return nil, fmt.Errorf("chat export job runner is required")
	}

	m.mu.Lock()
	if running := m.findActiveJobLocked(); running != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("another export job is already running: %s", running.JobID)
	}

	now := m.nowUTC()
	job := &chatExportAsyncJob{
		JobID:         uuid.NewString(),
		Status:        chatExportJobStatusQueued,
		StatusMessage: "job queued",
		CreatedAt:     now,
		UpdatedAt:     now,
		Request:       cloneStringAnyMap(request),
		Progress: map[string]any{
			"phase":          chatExportJobStatusQueued,
			"status_message": "job queued",
		},
	}

	m.jobs[job.JobID] = job
	if err := m.persistJobLocked(job); err != nil {
		delete(m.jobs, job.JobID)
		m.mu.Unlock()
		return nil, err
	}
	jobSnapshot := cloneChatExportAsyncJob(job)
	m.mu.Unlock()

	go m.execute(job.JobID, runner)
	return jobSnapshot, nil
}

func (m *chatExportJobManager) Get(jobID string) (*chatExportAsyncJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	job, ok := m.jobs[strings.TrimSpace(jobID)]
	if !ok {
		return nil, fmt.Errorf("job %q not found", strings.TrimSpace(jobID))
	}
	return cloneChatExportAsyncJob(job), nil
}

func (m *chatExportJobManager) execute(jobID string, runner chatExportAsyncJobRunner) {
	if err := m.markRunning(jobID); err != nil {
		logrus.WithError(err).Warnf("Failed to mark export job %s as running", jobID)
	}

	result, summary, err := runner(jobID, func(progress chatExportProgressSnapshot) {
		if progressErr := m.ReportProgress(jobID, progress); progressErr != nil {
			logrus.WithError(progressErr).Warnf("Failed to persist progress for export job %s", jobID)
		}
	})
	if err != nil {
		if failErr := m.failJob(jobID, err); failErr != nil {
			logrus.WithError(failErr).Warnf("Failed to persist failed state for export job %s", jobID)
		}
		return
	}

	if completeErr := m.completeJob(jobID, result, summary); completeErr != nil {
		logrus.WithError(completeErr).Warnf("Failed to persist completed state for export job %s", jobID)
	}
}

func (m *chatExportJobManager) ReportProgress(jobID string, snapshot chatExportProgressSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}

	if job.Progress == nil {
		job.Progress = map[string]any{}
	}

	if phase := strings.TrimSpace(snapshot.Phase); phase != "" {
		job.Progress["phase"] = phase
	}

	if statusMessage := strings.TrimSpace(snapshot.StatusMessage); statusMessage != "" {
		job.StatusMessage = statusMessage
		job.Progress["status_message"] = statusMessage
	}

	if len(snapshot.Chat) > 0 {
		job.Chat = cloneStringAnyMap(snapshot.Chat)
	}
	if len(snapshot.Counters) > 0 {
		job.Progress["counters"] = cloneStringAnyMap(snapshot.Counters)
	}
	if len(snapshot.Files) > 0 {
		job.Progress["files"] = cloneStringAnyMap(snapshot.Files)
	}

	job.UpdatedAt = m.nowUTC()
	return m.persistJobLocked(job)
}

func (m *chatExportJobManager) markRunning(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}

	now := m.nowUTC()
	job.Status = chatExportJobStatusRunning
	job.StatusMessage = "job running"
	job.StartedAt = now
	job.UpdatedAt = now
	if job.Progress == nil {
		job.Progress = map[string]any{}
	}
	job.Progress["phase"] = chatExportJobStatusRunning
	job.Progress["status_message"] = job.StatusMessage
	return m.persistJobLocked(job)
}

func (m *chatExportJobManager) completeJob(jobID string, result map[string]any, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}

	now := m.nowUTC()
	job.Status = chatExportJobStatusCompleted
	job.StatusMessage = "job completed"
	job.UpdatedAt = now
	job.FinishedAt = now
	job.Result = cloneStringAnyMap(result)
	job.Summary = strings.TrimSpace(summary)
	job.Error = ""
	if job.Progress == nil {
		job.Progress = map[string]any{}
	}
	job.Progress["phase"] = chatExportJobStatusCompleted
	job.Progress["status_message"] = job.StatusMessage
	return m.persistJobLocked(job)
}

func (m *chatExportJobManager) failJob(jobID string, cause error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}

	now := m.nowUTC()
	job.Status = chatExportJobStatusFailed
	job.StatusMessage = "job failed"
	job.UpdatedAt = now
	job.FinishedAt = now
	job.Error = strings.TrimSpace(cause.Error())
	if job.Progress == nil {
		job.Progress = map[string]any{}
	}
	job.Progress["phase"] = chatExportJobStatusFailed
	job.Progress["status_message"] = job.StatusMessage
	return m.persistJobLocked(job)
}

func (m *chatExportJobManager) loadPersistedJobs() {
	pattern := filepath.Join(m.baseDir, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		logrus.WithError(err).Warnf("Failed to list async export jobs in %s", m.baseDir)
		return
	}

	sort.Strings(files)
	for _, filePath := range files {
		payload, err := os.ReadFile(filePath)
		if err != nil {
			logrus.WithError(err).Warnf("Failed to read async export job manifest %s", filePath)
			continue
		}

		var job chatExportAsyncJob
		if err := json.Unmarshal(payload, &job); err != nil {
			logrus.WithError(err).Warnf("Failed to parse async export job manifest %s", filePath)
			continue
		}

		if strings.TrimSpace(job.JobID) == "" {
			logrus.Warnf("Skipping async export job manifest without job_id: %s", filePath)
			continue
		}

		if job.Status == chatExportJobStatusRunning || job.Status == chatExportJobStatusQueued {
			now := m.nowUTC()
			job.Status = chatExportJobStatusFailed
			job.StatusMessage = "server restarted before job completion"
			job.Error = "server restarted before job completion"
			job.UpdatedAt = now
			job.FinishedAt = now
			if job.Progress == nil {
				job.Progress = map[string]any{}
			}
			job.Progress["phase"] = chatExportJobStatusFailed
			job.Progress["status_message"] = job.StatusMessage
		}

		jobCopy := cloneChatExportAsyncJob(&job)
		m.jobs[job.JobID] = jobCopy
		if err := m.persistJob(jobCopy); err != nil {
			logrus.WithError(err).Warnf("Failed to rewrite recovered async export job %s", job.JobID)
		}
	}
}

func (m *chatExportJobManager) findActiveJobLocked() *chatExportAsyncJob {
	for _, job := range m.jobs {
		if job.Status == chatExportJobStatusQueued || job.Status == chatExportJobStatusRunning {
			return job
		}
	}
	return nil
}

func (m *chatExportJobManager) persistJob(job *chatExportAsyncJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.persistJobLocked(job)
}

func (m *chatExportJobManager) persistJobLocked(job *chatExportAsyncJob) error {
	if job == nil {
		return fmt.Errorf("job is nil")
	}

	if err := os.MkdirAll(m.baseDir, 0o700); err != nil {
		return fmt.Errorf("failed to create async export jobs directory: %w", err)
	}

	payload, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode async export job %s: %w", job.JobID, err)
	}

	targetPath := filepath.Join(m.baseDir, job.JobID+".json")
	tempPath := targetPath + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write async export job manifest: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("failed to finalize async export job manifest: %w", err)
	}

	return nil
}

func (m *chatExportJobManager) nowUTC() string {
	nowFn := m.nowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	return nowFn().UTC().Format(time.RFC3339)
}

func cloneChatExportAsyncJob(job *chatExportAsyncJob) *chatExportAsyncJob {
	if job == nil {
		return nil
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return &chatExportAsyncJob{
			JobID:         job.JobID,
			Status:        job.Status,
			StatusMessage: job.StatusMessage,
			CreatedAt:     job.CreatedAt,
			StartedAt:     job.StartedAt,
			UpdatedAt:     job.UpdatedAt,
			FinishedAt:    job.FinishedAt,
			Request:       cloneStringAnyMap(job.Request),
			Chat:          cloneStringAnyMap(job.Chat),
			Progress:      cloneStringAnyMap(job.Progress),
			Result:        cloneStringAnyMap(job.Result),
			Summary:       job.Summary,
			Error:         job.Error,
		}
	}

	var clone chatExportAsyncJob
	if err := json.Unmarshal(payload, &clone); err != nil {
		return &chatExportAsyncJob{
			JobID:         job.JobID,
			Status:        job.Status,
			StatusMessage: job.StatusMessage,
			CreatedAt:     job.CreatedAt,
			StartedAt:     job.StartedAt,
			UpdatedAt:     job.UpdatedAt,
			FinishedAt:    job.FinishedAt,
			Request:       cloneStringAnyMap(job.Request),
			Chat:          cloneStringAnyMap(job.Chat),
			Progress:      cloneStringAnyMap(job.Progress),
			Result:        cloneStringAnyMap(job.Result),
			Summary:       job.Summary,
			Error:         job.Error,
		}
	}
	return &clone
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}

	payload, err := json.Marshal(source)
	if err != nil {
		clone := make(map[string]any, len(source))
		for key, value := range source {
			clone[key] = value
		}
		return clone
	}

	var clone map[string]any
	if err := json.Unmarshal(payload, &clone); err != nil {
		clone = make(map[string]any, len(source))
		for key, value := range source {
			clone[key] = value
		}
	}
	return clone
}
