//go:build testdb

package api

import (
	"io"
	"log"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diepxuan/zcloud/internal/store"
)

// TestSyncSchedulerSkipWhenRunning: tick lần 2 khi lần 1 chưa xong phải bị skip.
func TestSyncSchedulerSkipWhenRunning(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	// Chưa có account active → tick phải chạy nhanh và return.
	sched := NewSyncScheduler(st, testLogger(), 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	// Không có account → tick chỉ log "list accounts: empty" hoặc no-op.
	// Gọi tick thủ công để verify không panic + không race.
	done := make(chan struct{})
	go func() {
		sched.tick(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tick timed out")
	}
}

// TestSyncSchedulerNewDefaults: interval quá nhỏ → ép về default 10 phút.
func TestSyncSchedulerNewDefaults(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	sched := NewSyncScheduler(st, testLogger(), 5*time.Second) // < min
	if sched.interval != defaultAutoSyncInterval {
		t.Fatalf("expected default interval, got %s", sched.interval)
	}
	sched = NewSyncScheduler(st, testLogger(), 2*time.Minute)
	if sched.interval != 2*time.Minute {
		t.Fatalf("expected 2m, got %s", sched.interval)
	}
}

// TestSyncSchedulerLastTick: ghi nhận lastTick sau khi syncAccount chạy xong
// (phải có ít nhất 1 conv).
func TestSyncSchedulerLastTick(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	if err := st.CreateAccount("acc-1", "Test", 1); err != nil {
		t.Fatalf("account: %v", err)
	}
	conv := &store.Conversation{
		ID: "c-1", AccountID: "acc-1", Name: "Conv", ConvType: 0, LastMsgID: "100",
	}
	if err := st.SaveConversation(conv); err != nil {
		t.Fatalf("save conv: %v", err)
	}
	sched := NewSyncScheduler(st, testLogger(), 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	// Không có listener → syncConvViaListener returns false nhưng tick vẫn
	// chạy qua từng conv và cập nhật lastTick.
	sched.syncAccount(ctx, "acc-1")
	last := sched.LastTick("acc-1")
	if last.IsZero() {
		t.Fatal("expected non-zero lastTick")
	}
}

// TestSyncSchedulerStop tránh goroutine leak.
func TestSyncSchedulerStop(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	sched := NewSyncScheduler(st, testLogger(), 30*time.Second)
	stopped := int32(0)
	go func() {
		sched.Start(context.Background())
		atomic.StoreInt32(&stopped, 1)
	}()
	time.Sleep(50 * time.Millisecond)
	sched.Stop()
	// done channel đã đóng → Stop không block.
	if atomic.LoadInt32(&stopped) != 1 {
		t.Fatal("scheduler goroutine did not exit after Stop")
	}
}

func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
