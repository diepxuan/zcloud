package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

func TestEnqueueMessageMediaJobsWritesJobsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateAccount("acc-1", "T", 1); err != nil {
		t.Fatal(err)
	}
	msg := &core.Message{
		ID: "m-1", ConvID: "c-1", Type: core.MsgTypeImage,
		Attachments: []core.Attachment{
			{ID: "f-1", URL: "https://example.com/1.jpg", FileName: "1.jpg"},
			{ID: "f-2", URL: "https://example.com/2.jpg", FileName: "2.jpg"},
		},
	}
	enqueueMessageMediaJobs(st, "acc-1", msg, testLogger())
	jobs, _ := st.ListPendingMediaJobs("acc-1", 10)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	enqueueMessageMediaJobs(st, "acc-1", msg, testLogger())
	jobs, _ = st.ListPendingMediaJobs("acc-1", 10)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs after re-enqueue, got %d", len(jobs))
	}
}

func TestEnqueueMessageMediaJobsSkipsNonMedia(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	defer st.Close()
	_ = st.CreateAccount("acc-1", "T", 1)
	msg := &core.Message{ID: "m-1", ConvID: "c-1", Type: core.MsgTypeText,
		Attachments: []core.Attachment{{ID: "f", URL: "https://example.com/1.jpg", FileName: "1.jpg"}}}
	enqueueMessageMediaJobs(st, "acc-1", msg, testLogger())
	jobs, _ := st.ListPendingMediaJobs("acc-1", 10)
	if len(jobs) != 0 {
		t.Fatalf("text message must not enqueue media, got %d", len(jobs))
	}
}

func TestEnqueueMessageMediaJobsResetsWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	defer st.Close()
	_ = st.CreateAccount("acc-1", "T", 1)
	job := &store.MediaJob{
		ID: "f-1", AccountID: "acc-1", ConvID: "c-1", MsgID: "m-1",
		FileName: "1.jpg", FileExt: "jpg", SourceURL: "https://example.com/1.jpg",
		Status: store.MediaJobDone, MaxAttempts: 3,
	}
	_ = st.SaveMediaJob(job)
	_ = st.MarkMediaJobDone("f-1", "acc-1", "acc-1/c-1/f-1.jpg")
	msg := &core.Message{
		ID: "m-1", ConvID: "c-1", Type: core.MsgTypeImage,
		Attachments: []core.Attachment{{ID: "f-1", URL: "https://example.com/1.jpg", FileName: "1.jpg"}},
	}
	enqueueMessageMediaJobs(st, "acc-1", msg, testLogger())
	got, _ := st.GetMediaJob("f-1", "acc-1")
	if got == nil || got.Status != store.MediaJobPending {
		t.Fatalf("expected reset to pending when file missing, got %+v", got)
	}
}

func TestFileExistsHelper(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	defer st.Close()
	if fileExists(st, "acc-1", "c-1", "missing", "jpg") {
		t.Fatal("missing file should return false")
	}
	mediaDir := st.MediaDir("acc-1", "c-1")
	_ = os.WriteFile(filepath.Join(mediaDir, "present.jpg"), []byte("x"), 0644)
	if !fileExists(st, "acc-1", "c-1", "present", "jpg") {
		t.Fatal("present file should return true")
	}
}

func TestMediaWorkerProcessJob(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			http.Error(w, "server not ready", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateAccount("acc-1", "Test", 1); err != nil {
		t.Fatal(err)
	}
	job := &store.MediaJob{
		ID: "file-1", AccountID: "acc-1", ConvID: "c-1", MsgID: "m-1",
		FileName: "x.jpg", FileExt: "jpg", SourceURL: srv.URL + "/x.jpg",
		MaxAttempts: 3,
	}
	if err := st.SaveMediaJob(job); err != nil {
		t.Fatal(err)
	}

	w := NewMediaWorker(st, testLogger(), time.Millisecond, 10)
	w.processJob(context.Background(), *job)

	if hits < 3 {
		t.Fatalf("expected retries, got %d hits", hits)
	}
	got, err := st.GetMediaJob("file-1", "acc-1")
	if err != nil || got == nil {
		t.Fatalf("GetMediaJob: %v", err)
	}
	if got.Status != store.MediaJobDone {
		t.Fatalf("status = %q, want done", got.Status)
	}
	if _, err := os.Stat(st.MediaFilePath("acc-1", "c-1", "file-1", "jpg")); err != nil {
		t.Fatalf("media file missing: %v", err)
	}
}

func TestMediaWorkerSkipsExistingFile(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	defer st.Close()
	_ = st.CreateAccount("acc-1", "T", 1)
	mediaDir := st.MediaDir("acc-1", "c-1")
	_ = os.WriteFile(filepath.Join(mediaDir, "file-1.jpg"), []byte("existing"), 0644)
	job := &store.MediaJob{
		ID: "file-1", AccountID: "acc-1", ConvID: "c-1", FileName: "x.jpg",
		FileExt: "jpg", SourceURL: srv.URL + "/x.jpg", MaxAttempts: 3,
	}
	_ = st.SaveMediaJob(job)

	w := NewMediaWorker(st, testLogger(), time.Millisecond, 10)
	w.processJob(context.Background(), *job)

	if hits != 0 {
		t.Fatalf("server should not be hit, got %d", hits)
	}
	got, _ := st.GetMediaJob("file-1", "acc-1")
	if got == nil || got.Status != store.MediaJobDone {
		t.Fatalf("job status = %+v", got)
	}
}

func TestMediaWorkerStartStop(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	defer st.Close()
	w := NewMediaWorker(st, testLogger(), time.Hour, 10)
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	cancel()
	w.Stop()
}
