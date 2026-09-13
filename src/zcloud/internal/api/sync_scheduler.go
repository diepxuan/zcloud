package api

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// SyncScheduler tự động kéo tin nhắn cũ cho mọi conversation của mọi account
// đang có session active. Chạy nền trong goroutine; idempotent — chỉ chạy
// khi tới interval và skip nếu lần trước chưa xong.
type SyncScheduler struct {
	store    *store.Store
	logger   *log.Logger
	interval time.Duration

	mu       sync.Mutex
	running  bool
	lastTick map[string]time.Time // accountID → thời điểm tick gần nhất

	stop chan struct{}
	done chan struct{}
}

const (
	defaultAutoSyncInterval = 10 * time.Minute
	autoSyncMinInterval     = 30 * time.Second
	autoSyncConvDelay       = 500 * time.Millisecond // throttle giữa các conv
)

// NewSyncScheduler tạo scheduler. interval <= 0 → dùng mặc định 10 phút.
func NewSyncScheduler(st *store.Store, logger *log.Logger, interval time.Duration) *SyncScheduler {
	if interval < autoSyncMinInterval {
		interval = defaultAutoSyncInterval
	}
	return &SyncScheduler{
		store:    st,
		logger:   logger,
		interval: interval,
		lastTick: make(map[string]time.Time),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start chạy scheduler trong goroutine nền. Gọi Stop để dừng.
func (s *SyncScheduler) Start(ctx context.Context) {
	go s.loop(ctx)
}

// Stop tín hiệu dừng và chờ goroutine thoát.
func (s *SyncScheduler) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
}

func (s *SyncScheduler) loop(ctx context.Context) {
	defer close(s.done)
	// Chạy ngay lần đầu sau 5 giây để không block boot.
	first := time.NewTimer(5 * time.Second)
	defer first.Stop()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-first.C:
			s.tick(ctx)
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick quét 1 lượt mọi account active. Bỏ qua nếu đang có lần chạy trước.
func (s *SyncScheduler) tick(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	accounts, err := s.store.ListActiveAccountIDs()
	if err != nil {
		s.logger.Printf("auto-sync: list accounts: %v", err)
		return
	}
	for _, accID := range accounts {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		default:
		}
		s.syncAccount(ctx, accID)
	}
}

// syncAccount đồng bộ tất cả conversation của 1 account.
func (s *SyncScheduler) syncAccount(ctx context.Context, accountID string) {
	// Listener phải chạy thì WS sync mới có hiệu lực.
	StartZaloListener(s.store, accountID, s.logger)

	convs, err := s.store.GetConversations(accountID)
	if err != nil {
		s.logger.Printf("auto-sync %s: list conv: %v", accountID, err)
		return
	}
	if len(convs) == 0 {
		return
	}
	s.logger.Printf("auto-sync %s: %d conversations", accountID, len(convs))

	for _, c := range convs {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		default:
		}
		ok := syncConvViaListener(accountID, c.ID, c.ConvType, c.LastMsgID)
		if ok {
			s.logger.Printf("auto-sync %s: requested conv=%s type=%d lastId=%s",
				accountID, c.ID, c.ConvType, c.LastMsgID)
		}
		// Throttle giữa các conv tránh spam Zalo.
		select {
		case <-time.After(autoSyncConvDelay):
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		}
	}
	// Resume SyncV2 pull loop nếu state trong DB đang ở phase=pulling.
	resumeSyncV2(s.store, accountID, s.logger)

	s.mu.Lock()
	s.lastTick[accountID] = time.Now()
	s.mu.Unlock()
}

// syncConvViaListener gửi WS cmd 510/511 với lastId cụ thể.
// Trả về true nếu listener gửi request thành công.
func syncConvViaListener(accountID, convID string, convType int, lastID string) bool {
	zaloListenerMu.Lock()
	entry := zaloListeners[accountID]
	zaloListenerMu.Unlock()
	if entry == nil || entry.client == nil || entry.client.WS == nil {
		return false
	}
	tt := core.ThreadUser
	if convType == 1 {
		tt = core.ThreadGroup
	}
	return entry.client.WS.RequestOldMessages(context.Background(), tt, lastID) == nil
}


// resumeSyncV2 kiểm tra accounts.syncv2_state của account; nếu phase=pulling
// thì đảm bảo SyncV2Client đang chạy background PullBatch loop. Resume
// giúp restart không phải bắt đầu lại từ đầu.
func resumeSyncV2(st *store.Store, accountID string, logger *log.Logger) {
	raw, err := st.GetAccountSyncV2State(accountID)
	if err != nil {
		return
	}
	if raw == "" || raw == "{}" {
		return
	}
	var st2 core.SyncV2State
	if err := json.Unmarshal([]byte(raw), &st2); err != nil {
		return
	}
	if st2.Phase != "pulling" {
		return
	}
	// Đảm bảo session load được (cần cho cipher session + REST call).
	sessRec, err := st.LoadSessionByAccountID(accountID)
	if err != nil || sessRec == nil {
		return
	}
	clientObj, err := clientFromSession(sessRec)
	if err != nil {
		return
	}
	entry, err := ensureSyncV2Client(st, accountID, clientObj.Session, logger)
	if err != nil {
		return
	}
	// Nếu loop đang chạy thì bỏ qua; nếu chưa thì bắt đầu.
	// Dùng phase + check goroutine qua biến runningPull.
	if !tryStartSyncV2PullLoop(entry, accountID) {
		return
	}
	go runSyncV2PullLoop(entry, st, accountID, logger)
}
// LastTick trả về thời điểm tick gần nhất của account (zero time nếu chưa chạy).
func (s *SyncScheduler) LastTick(accountID string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTick[accountID]
}
