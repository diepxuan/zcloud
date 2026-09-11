package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// MediaWorker processes persisted media download jobs in the background.
type MediaWorker struct {
	store    *store.Store
	logger   *log.Logger
	interval time.Duration
	batch    int

	stop chan struct{}
	done chan struct{}
}

const (
	defaultMediaWorkerInterval = 5 * time.Second
	defaultMediaWorkerBatch    = 20
)

func NewMediaWorker(st *store.Store, logger *log.Logger, interval time.Duration, batch int) *MediaWorker {
	if interval <= 0 {
		interval = defaultMediaWorkerInterval
	}
	if batch <= 0 {
		batch = defaultMediaWorkerBatch
	}
	return &MediaWorker{
		store:    st,
		logger:   logger,
		interval: interval,
		batch:    batch,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (w *MediaWorker) Start(ctx context.Context) {
	go w.loop(ctx)
}

func (w *MediaWorker) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
}

func (w *MediaWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *MediaWorker) tick(ctx context.Context) {
	accounts, err := w.store.ListActiveAccountIDs()
	if err != nil {
		w.logger.Printf("media-worker: list accounts: %v", err)
		return
	}
	for _, accountID := range accounts {
		select {
		case <-ctx.Done():
			return
		default:
		}
		w.processAccount(ctx, accountID)
	}
}

func (w *MediaWorker) processAccount(ctx context.Context, accountID string) {
	jobs, err := w.store.ListPendingMediaJobs(accountID, w.batch)
	if err != nil {
		w.logger.Printf("media-worker: list jobs %s: %v", accountID, err)
		return
	}
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		w.processJob(ctx, job)
	}
}

func (w *MediaWorker) processJob(ctx context.Context, job store.MediaJob) {
	attempt := job.Attempts + 1
	_ = w.store.MarkMediaJobRunning(job.ID, job.AccountID, attempt, "")
	filePath := w.store.MediaFilePath(job.AccountID, job.ConvID, job.ID, job.FileExt)
	if _, err := os.Stat(filePath); err == nil {
		rel, _ := filepath.Rel(w.store.MediaPath(), filePath)
		_ = w.store.MarkMediaJobDone(job.ID, job.AccountID, rel)
		w.broadcastDone(job, filePath)
		return
	}

	data, err := w.download(ctx, job.SourceURL, job.MaxAttempts-attempt+1)
	if err != nil {
		if attempt >= job.MaxAttempts {
			_ = w.store.MarkMediaJobFailed(job.ID, job.AccountID, err.Error())
			w.logger.Printf("media-worker: failed %s: %v", job.ID, err)
		} else {
			_ = w.store.RetryMediaJob(job.ID, job.AccountID, attempt, err.Error())
			w.logger.Printf("media-worker: retry later %s: %v", job.ID, err)
		}
		return
	}
	if len(data) == 0 {
		_ = w.store.MarkMediaJobFailed(job.ID, job.AccountID, "empty response")
		return
	}

	dir := w.store.MediaDir(job.AccountID, job.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		_ = w.store.MarkMediaJobFailed(job.ID, job.AccountID, err.Error())
		return
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		_ = w.store.MarkMediaJobFailed(job.ID, job.AccountID, err.Error())
		return
	}
	rel, _ := filepath.Rel(w.store.MediaPath(), filePath)
	if _, err := w.store.SaveMedia(&store.MediaFile{
		ID: job.ID, AccountID: job.AccountID, ConvID: job.ConvID, MsgID: job.MsgID,
		FileName: job.FileName, FileExt: job.FileExt, SourceURL: job.SourceURL,
		IsDownloaded: 1,
	}); err != nil {
		w.logger.Printf("media-worker: save media meta %s: %v", job.ID, err)
	}
	_ = w.store.MarkMediaJobDone(job.ID, job.AccountID, rel)
	w.broadcastDone(job, filePath)
}

func (w *MediaWorker) download(ctx context.Context, url string, attempts int) ([]byte, error) {
	var lastErr error
	client := &http.Client{Timeout: 30 * time.Second}
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < attempts {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = &httpStatusError{status: resp.StatusCode}
			_ = resp.Body.Close()
			if attempt < attempts {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < attempts {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			continue
		}
		// Phát hiện URL die trả về HTML/text (vd photo không tòn tại).
		// Không phải media binary → tránh lưu file HTML thành .bin 185KB.
		if !core.IsMediaContent(data) {
			ctype := resp.Header.Get("Content-Type")
			lastErr = fmt.Errorf("non-media response (Content-Type=%q, len=%d)", ctype, len(data))
			if attempt < attempts {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

func (w *MediaWorker) broadcastDone(job store.MediaJob, filePath string) {
	urlPath := "/media/" + job.AccountID + "/" + job.ConvID + "/" + job.ID + "." + job.FileExt
	globalWS.Broadcast(job.AccountID, BrowserMessage{
		Type: "media_downloaded",
		Data: map[string]interface{}{
			"msgId": job.MsgID,
			"convId": job.ConvID,
			"url": urlPath,
			"path": filePath,
		},
	})
}

type httpStatusError struct {
	status int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http status %d", e.status)
}
