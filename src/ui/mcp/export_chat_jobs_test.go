package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestChatExportJobManagerStartAndCompleteLifecycle(t *testing.T) {
	manager := newChatExportJobManager(t.TempDir())
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	job, err := manager.Start(map[string]any{
		"chat_jid": "120363424157959439@g.us",
	}, func(_ string, report chatExportProgressReporter) (map[string]any, string, error) {
		report(chatExportProgressSnapshot{
			Phase:         "collecting_messages",
			StatusMessage: "collecting messages",
			Counters: map[string]any{
				"messages_collected": 10,
			},
		})
		started <- struct{}{}
		<-release
		report(chatExportProgressSnapshot{
			Phase:         "completed",
			StatusMessage: "chat export bundle completed",
			Counters: map[string]any{
				"messages_collected": 10,
			},
			Files: map[string]any{
				"archive_zip": "/tmp/chat_export_bundle.zip",
			},
		})
		return map[string]any{
			"files": map[string]any{
				"archive_zip": "/tmp/chat_export_bundle.zip",
			},
		}, "export done", nil
	})
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	if job.Status != chatExportJobStatusQueued {
		t.Fatalf("expected queued job status, got %q", job.Status)
	}

	manifestPath := filepath.Join(manager.baseDir, job.JobID+".json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected manifest %s to exist: %v", manifestPath, err)
	}

	<-started
	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusRunning)

	runningJob, err := manager.Get(job.JobID)
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if runningJob.Progress["phase"] != "collecting_messages" {
		t.Fatalf("expected collecting_messages phase, got %#v", runningJob.Progress["phase"])
	}

	close(release)
	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusCompleted)

	completedJob, err := manager.Get(job.JobID)
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if completedJob.Status != chatExportJobStatusCompleted {
		t.Fatalf("expected completed job status, got %q", completedJob.Status)
	}
	if completedJob.Summary != "export done" {
		t.Fatalf("expected summary to be preserved, got %q", completedJob.Summary)
	}

	files, ok := completedJob.Result["files"].(map[string]any)
	if !ok {
		t.Fatalf("expected result.files map, got %#v", completedJob.Result["files"])
	}
	if files["archive_zip"] != "/tmp/chat_export_bundle.zip" {
		t.Fatalf("expected archive zip path to be preserved, got %#v", files["archive_zip"])
	}
}

func TestChatExportJobManagerRejectsConcurrentRunningJob(t *testing.T) {
	manager := newChatExportJobManager(t.TempDir())
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	firstJob, err := manager.Start(map[string]any{"chat_jid": "first@g.us"}, func(_ string, _ chatExportProgressReporter) (map[string]any, string, error) {
		started <- struct{}{}
		<-release
		return map[string]any{"ok": true}, "done", nil
	})
	if err != nil {
		t.Fatalf("Start(first) unexpected error: %v", err)
	}

	<-started
	waitForJobStatus(t, manager, firstJob.JobID, chatExportJobStatusRunning)

	_, err = manager.Start(map[string]any{"chat_jid": "second@g.us"}, func(_ string, _ chatExportProgressReporter) (map[string]any, string, error) {
		return map[string]any{"ok": true}, "done", nil
	})
	if err == nil {
		t.Fatal("expected second Start() to fail while first job is running")
	}
	if !strings.Contains(err.Error(), "another export job is already running") {
		t.Fatalf("unexpected concurrent job error: %v", err)
	}

	close(release)
	waitForJobStatus(t, manager, firstJob.JobID, chatExportJobStatusCompleted)
}

func TestChatExportJobManagerRecoversInterruptedRunningJob(t *testing.T) {
	baseDir := t.TempDir()
	manifestPath := filepath.Join(baseDir, "job-running.json")
	payload := chatExportAsyncJob{
		JobID:         "job-running",
		Status:        chatExportJobStatusRunning,
		StatusMessage: "job running",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Request: map[string]any{
			"chat_jid": "120363424157959439@g.us",
		},
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() unexpected error: %v", err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatalf("os.WriteFile() unexpected error: %v", err)
	}

	manager := newChatExportJobManager(baseDir)
	job, err := manager.Get("job-running")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}

	if job.Status != chatExportJobStatusFailed {
		t.Fatalf("expected recovered job to be marked as failed, got %q", job.Status)
	}
	if !strings.Contains(job.Error, "server restarted before job completion") {
		t.Fatalf("expected restart failure reason, got %q", job.Error)
	}
}

func TestHandleExportChatAsyncStatusReturnsProgress(t *testing.T) {
	manager := newChatExportJobManager(t.TempDir())
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	job, err := manager.Start(map[string]any{"chat_jid": "120363424157959439@g.us"}, func(_ string, report chatExportProgressReporter) (map[string]any, string, error) {
		report(chatExportProgressSnapshot{
			Phase:         "downloading_media",
			StatusMessage: "processed 3 media items",
			Counters: map[string]any{
				"messages_collected":        15,
				"media_found":               3,
				"media_included":            2,
				"media_failed":              1,
				"media_recovered_via_retry": 1,
			},
		})
		started <- struct{}{}
		<-release
		return map[string]any{"done": true}, "done", nil
	})
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	<-started
	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusRunning)

	handler := &QueryHandler{exportJobs: manager}
	result, err := handler.handleExportChatAsyncStatus(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"job_id": job.JobID,
			},
		},
	})
	if err != nil {
		t.Fatalf("handleExportChatAsyncStatus() unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected async status result to be non-error")
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	if structured["status"] != chatExportJobStatusRunning {
		t.Fatalf("expected envelope status %q, got %#v", chatExportJobStatusRunning, structured["status"])
	}

	resultPayload, ok := structured["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured result payload map, got %T", structured["result"])
	}
	progress, ok := resultPayload["progress"].(map[string]any)
	if !ok {
		t.Fatalf("expected result.progress map, got %T", resultPayload["progress"])
	}
	if progress["phase"] != "downloading_media" {
		t.Fatalf("expected progress phase downloading_media, got %#v", progress["phase"])
	}

	close(release)
	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusCompleted)
}

func TestHandleExportChatAsyncResultReturnsStructuredErrorWhileRunning(t *testing.T) {
	manager := newChatExportJobManager(t.TempDir())
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	job, err := manager.Start(map[string]any{"chat_jid": "120363424157959439@g.us"}, func(_ string, report chatExportProgressReporter) (map[string]any, string, error) {
		report(chatExportProgressSnapshot{
			Phase:         "collecting_messages",
			StatusMessage: "collecting messages",
			Counters: map[string]any{
				"messages_collected": 25,
			},
		})
		started <- struct{}{}
		<-release
		return map[string]any{"done": true}, "done", nil
	})
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	<-started
	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusRunning)

	handler := &QueryHandler{exportJobs: manager}
	result, err := handler.handleExportChatAsyncResult(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"job_id": job.JobID,
			},
		},
	})
	if err != nil {
		t.Fatalf("handleExportChatAsyncResult() unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected async result to be an error while job is running")
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	if structured["status"] != chatExportJobStatusRunning {
		t.Fatalf("expected envelope status %q, got %#v", chatExportJobStatusRunning, structured["status"])
	}
	if !strings.Contains(structured["summary"].(string), "job not completed yet") {
		t.Fatalf("expected pending summary, got %#v", structured["summary"])
	}

	close(release)
	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusCompleted)
}

func TestHandleExportChatAsyncResultReturnsFinalPayloadWhenCompleted(t *testing.T) {
	manager := newChatExportJobManager(t.TempDir())

	job, err := manager.Start(map[string]any{"chat_jid": "120363424157959439@g.us"}, func(_ string, _ chatExportProgressReporter) (map[string]any, string, error) {
		return map[string]any{
			"files": map[string]any{
				"archive_zip": "/tmp/chat_export_bundle.zip",
			},
			"stats": map[string]any{
				"messages_exported": 42,
			},
		}, "async export complete", nil
	})
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	waitForJobStatus(t, manager, job.JobID, chatExportJobStatusCompleted)

	handler := &QueryHandler{exportJobs: manager}
	result, err := handler.handleExportChatAsyncResult(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"job_id": job.JobID,
			},
		},
	})
	if err != nil {
		t.Fatalf("handleExportChatAsyncResult() unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected completed async result to be non-error")
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	resultPayload, ok := structured["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured result payload map, got %T", structured["result"])
	}
	stats, ok := resultPayload["stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected final result stats map, got %T", resultPayload["stats"])
	}
	if stats["messages_exported"] != float64(42) {
		t.Fatalf("expected final result payload to be preserved, got %#v", stats["messages_exported"])
	}
}

func waitForJobStatus(t *testing.T, manager *chatExportJobManager, jobID string, expectedStatus string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(jobID)
		if err == nil && job != nil && job.Status == expectedStatus {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	job, err := manager.Get(jobID)
	if err != nil {
		t.Fatalf("Get() unexpected error while waiting for status %q: %v", expectedStatus, err)
	}
	t.Fatalf("timed out waiting for job %s to reach status %q; last status=%q", jobID, expectedStatus, job.Status)
}
