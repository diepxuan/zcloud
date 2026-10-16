package api

// SyncV2 WS handler — T22.3 (Phase B).
// Hook WS event 590/591/592/632 vào `core.SyncV2Client.HandleEvent` +
// persist state JSONB vào `accounts.syncv2_state`. Khi server push
// `user_confirm` (user_action=1|3), trigger background `PullBatch` loop
// để kéo messages từ server về SaveMessage.
//
// Xem `docs/protocol/syncv2.md` + `internal/core/syncv2.go`.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/logging"
	"github.com/diepxuan/zcloud/internal/store"
)

type syncV2Entry struct {
	client      *core.SyncV2Client
	cancel      context.CancelFunc
	pullRunning bool // guard tránh 2 goroutine cùng pull cho 1 account
	pullMu      sync.Mutex
}

// ensureSyncV2Client nạp (hoặc tạo) SyncV2Client cho account và lưu state
// vào DB. Trả về entry hiện tại (có thể là cũ nếu đã từng start).
func (s *Server) ensureSyncV2Client(accountID string, sess *core.Session) (*syncV2Entry, error) {
	s.syncV2Mu.Lock()
	defer s.syncV2Mu.Unlock()
	if s.syncV2Registry == nil {
		s.syncV2Registry = make(map[string]*syncV2Entry)
	}
	if e, ok := s.syncV2Registry[accountID]; ok {
		return e, nil
	}
	// Nạp state từ DB (nếu có) để resume sau restart.
	raw, err := s.Store.GetAccountSyncV2State(accountID)
	if err != nil {
		return nil, fmt.Errorf("load syncv2 state: %w", err)
	}
	var state core.SyncV2State
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			s.Logger.Warn("syncv2_state_parse_failed", "account_id", accountID, "error", err)
			state = core.SyncV2State{Phase: "init"}
		}
	} else {
		state = core.SyncV2State{Phase: "init"}
	}
	client, err := core.NewSyncV2Client(sess, &state)
	if err != nil {
		return nil, fmt.Errorf("new syncv2 client: %w", err)
	}
	if err := persistSyncV2State(s.Store, accountID, client); err != nil {
		return nil, err
	}
	// cancel hook cho future use (vd stop pull loop khi logout).
	_, cancel := context.WithCancel(context.Background())
	entry := &syncV2Entry{client: client, cancel: cancel}
	s.syncV2Registry[accountID] = entry
	s.Logger.Info("syncv2_client_initialized", "account_id", accountID, "phase", state.Phase)
	return entry, nil
}

// persistSyncV2State ghi state hiện tại xuống DB.
func persistSyncV2State(st *store.Store, accountID string, c *core.SyncV2Client) error {
	b, err := json.Marshal(c.State())
	if err != nil {
		return err
	}
	return st.SetAccountSyncV2State(accountID, string(b))
}

// getSyncV2Client trả client hiện tại (không tạo mới). Trả nil nếu chưa
// init — caller xử lý bằng cách bỏ qua event.
func (s *Server) getSyncV2Client(accountID string) *core.SyncV2Client {
	s.syncV2Mu.Lock()
	defer s.syncV2Mu.Unlock()
	if e, ok := s.syncV2Registry[accountID]; ok {
		return e.client
	}
	return nil
}

// handleSyncV2Event xử lý WS event 590/591/592/632 cho SyncV2 flow.
// - event.Data là raw JSON từ server (vd {act: "user_confirm", data: {...}}).
// - Trigger RequestSync (nếu phase=init) hoặc HandleEvent (cập nhật state).
// - Khi user_confirm → chạy PullBatch loop background.
func (s *Server) handleSyncV2Event(ctx context.Context, accountID string, rawData []byte) {
	st := s.Store
	logger := s.Logger

	if len(rawData) == 0 {
		return
	}
	// Lazy-init: lấy session từ store, tạo client nếu cần.
	sessRec, err := st.LoadSessionByAccountID(accountID)
	if err != nil || sessRec == nil {
		logger.Warn("syncv2_event_skipped", "account_id", accountID, "reason", "no_session", "error", err)
		return
	}
	clientObj, err := clientFromSession(sessRec)
	if err != nil {
		logger.Error("syncv2_client_build_failed", "account_id", accountID, "error", err)
		return
	}
	entry, err := s.ensureSyncV2Client(accountID, clientObj.Session)
	if err != nil {
		logger.Error("syncv2_client_ensure_failed", "account_id", accountID, "error", err)
		return
	}
	syncV2 := entry.client

	// Nếu state machine chưa start, tự trigger RequestSync lần đầu.
	if syncV2.State().Phase == "init" {
		go func() {
			rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer rcancel()
			if err := syncV2.RequestSync(rctx); err != nil {
				logger.Error("syncv2_request_failed", "account_id", accountID, "error", err)
			} else {
				logger.Info("syncv2_request_sent", "account_id", accountID)
				_ = persistSyncV2State(st, accountID, syncV2)
			}
		}()
	}

	// Feed event vào state machine.
	if err := syncV2.HandleEvent(rawData); err != nil {
		logger.Error("syncv2_event_handle_failed", "account_id", accountID, "error", err)
	}
	_ = persistSyncV2State(st, accountID, syncV2)

	// Nếu phase chuyển sang "pulling" → bắt đầu PullBatch loop.
	if syncV2.State().Phase == "pulling" {
		go runSyncV2PullLoop(entry, st, accountID, logger)
	}
}

// runSyncV2PullLoop kéo batch messages cho đến khi done hoặc lỗi.
// Persist last_seq_id sau mỗi batch để restart resume đúng vị trí.
func runSyncV2PullLoop(entry *syncV2Entry, st *store.Store, accountID string, logger *logging.Logger) {
	defer finishSyncV2PullLoop(entry)
	client := entry.client
	for {
		select {
		case <-context.Background().Done():
			return
		default:
		}
		// Tránh nhiều goroutine cùng pull — guard đơn giản bằng phase.
		if client.State().Phase != "pulling" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		batch, err := client.PullBatch(ctx)
		cancel()
		if err != nil {
			logger.Error("syncv2_pull_failed", "account_id", accountID, "error", err)
			_ = persistSyncV2State(st, accountID, client)
			return
		}
		// Decrypt + SaveMessage.
		msgs, err := client.DecryptMessages(batch)
		if err != nil {
			logger.Error("syncv2_decrypt_failed", "account_id", accountID, "error", err)
		} else {
			saved := 0
			for i := range msgs {
				m := &msgs[i]
				if m == nil || m.ID == "" {
					continue
				}
				attJSON, _ := json.Marshal(m.Attachments)
				if err := st.SaveMessage(&store.Message{
					ID: m.ID, AccountID: accountID, ConvID: m.ConvID,
					FromID: m.FromID, FromName: m.FromName,
					Content: m.Content, MsgType: int(m.Type),
					Timestamp: m.Timestamp, Attachments: string(attJSON),
				}); err == nil {
					saved++
				}
			}
			logger.Info("syncv2_pull_batch_completed",
				"account_id", accountID,
				"saved", saved,
				"next_sequence_id", client.State().LastSeqID,
				"done", batch.Done,
			)
		}
		_ = persistSyncV2State(st, accountID, client)
		if batch.Done {
			return
		}
		// Throttle nhẹ giữa các batch.
		time.Sleep(500 * time.Millisecond)
	}
}

// tryStartSyncV2PullLoop trả true nếu caller nên bắt đầu PullBatch loop;
// false nếu loop đã chạy rồi (idempotent). Dùng trong cả WS hook lẫn
// scheduler resume.
func tryStartSyncV2PullLoop(entry *syncV2Entry, accountID string) bool {
	entry.pullMu.Lock()
	defer entry.pullMu.Unlock()
	if entry.pullRunning {
		return false
	}
	entry.pullRunning = true
	return true
}

// finishSyncV2PullLoop đánh dấu loop đã kết thúc — gọi khi PullBatch
// return (done=true, error, hoặc context cancel).
func finishSyncV2PullLoop(entry *syncV2Entry) {
	entry.pullMu.Lock()
	entry.pullRunning = false
	entry.pullMu.Unlock()
}
