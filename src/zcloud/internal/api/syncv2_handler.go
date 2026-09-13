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
	"log"
	"sync"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// syncV2Registry map accountID → *core.SyncV2Client + cancel function.
// Lifecycle: tạo 1 lần khi account start syncv2; cancel khi stop/reset.
var (
	syncV2Mu       sync.Mutex
	syncV2Registry = map[string]*syncV2Entry{}
)

type syncV2Entry struct {
	client *core.SyncV2Client
	cancel context.CancelFunc
}

// ensureSyncV2Client nạp (hoặc tạo) SyncV2Client cho account và lưu state
// vào DB. Trả về entry hiện tại (có thể là cũ nếu đã từng start).
func ensureSyncV2Client(st *store.Store, accountID string, sess *core.Session, logger *log.Logger) (*syncV2Entry, error) {
	syncV2Mu.Lock()
	defer syncV2Mu.Unlock()
	if e, ok := syncV2Registry[accountID]; ok {
		return e, nil
	}
	// Nạp state từ DB (nếu có) để resume sau restart.
	raw, err := st.GetAccountSyncV2State(accountID)
	if err != nil {
		return nil, fmt.Errorf("load syncv2 state: %w", err)
	}
	var state core.SyncV2State
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			logger.Printf("syncv2: parse state for %s fail %v — reset", accountID, err)
			state = core.SyncV2State{Phase: "init"}
		}
	} else {
		state = core.SyncV2State{Phase: "init"}
	}
	client, err := core.NewSyncV2Client(sess, &state)
	if err != nil {
		return nil, fmt.Errorf("new syncv2 client: %w", err)
	}
	if err := persistSyncV2State(st, accountID, client); err != nil {
		return nil, err
	}
	// cancel hook cho future use (vd stop pull loop khi logout).
	_, cancel := context.WithCancel(context.Background())
	entry := &syncV2Entry{client: client, cancel: cancel}
	syncV2Registry[accountID] = entry
	logger.Printf("syncv2: init for %s phase=%s", accountID, state.Phase)
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
func getSyncV2Client(accountID string) *core.SyncV2Client {
	syncV2Mu.Lock()
	defer syncV2Mu.Unlock()
	if e, ok := syncV2Registry[accountID]; ok {
		return e.client
	}
	return nil
}

// handleSyncV2Event xử lý WS event 590/591/592/632 cho SyncV2 flow.
// - event.Data là raw JSON từ server (vd {act: "user_confirm", data: {...}}).
// - Trigger RequestSync (nếu phase=init) hoặc HandleEvent (cập nhật state).
// - Khi user_confirm → chạy PullBatch loop background.
func handleSyncV2Event(ctx context.Context, st *store.Store, accountID string, rawData []byte, logger *log.Logger) {
	if len(rawData) == 0 {
		return
	}
	// Lazy-init: lấy session từ store, tạo client nếu cần.
	sessRec, err := st.LoadSessionByAccountID(accountID)
	if err != nil || sessRec == nil {
		logger.Printf("syncv2: skip event, no session for %s: %v", accountID, err)
		return
	}
	clientObj, err := clientFromSession(sessRec)
	if err != nil {
		logger.Printf("syncv2: build client for %s fail: %v", accountID, err)
		return
	}
	entry, err := ensureSyncV2Client(st, accountID, clientObj.Session, logger)
	if err != nil {
		logger.Printf("syncv2: ensureSyncV2Client fail: %v", err)
		return
	}
	syncV2 := entry.client

	// Nếu state machine chưa start, tự trigger RequestSync lần đầu.
	if syncV2.State().Phase == "init" {
		go func() {
			rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer rcancel()
			if err := syncV2.RequestSync(rctx); err != nil {
				logger.Printf("syncv2: RequestSync for %s fail: %v", accountID, err)
			} else {
				logger.Printf("syncv2: RequestSync sent for %s", accountID)
				_ = persistSyncV2State(st, accountID, syncV2)
			}
		}()
	}

	// Feed event vào state machine.
	if err := syncV2.HandleEvent(rawData); err != nil {
		logger.Printf("syncv2: HandleEvent for %s err: %v", accountID, err)
	}
	_ = persistSyncV2State(st, accountID, syncV2)

	// Nếu phase chuyển sang "pulling" → bắt đầu PullBatch loop.
	if syncV2.State().Phase == "pulling" {
		go runSyncV2PullLoop(entry, st, accountID, logger)
	}
}

// runSyncV2PullLoop kéo batch messages cho đến khi done hoặc lỗi.
// Persist last_seq_id sau mỗi batch để restart resume đúng vị trí.
func runSyncV2PullLoop(entry *syncV2Entry, st *store.Store, accountID string, logger *log.Logger) {
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
			logger.Printf("syncv2: PullBatch for %s err: %v", accountID, err)
			_ = persistSyncV2State(st, accountID, client)
			return
		}
		// Decrypt + SaveMessage.
		msgs, err := client.DecryptMessages(batch)
		if err != nil {
			logger.Printf("syncv2: DecryptMessages for %s err: %v", accountID, err)
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
			logger.Printf("syncv2: %s saved=%d nextSeq=%d done=%v", accountID, saved, client.State().LastSeqID, batch.Done)
		}
		_ = persistSyncV2State(st, accountID, client)
		if batch.Done {
			return
		}
		// Throttle nhẹ giữa các batch.
		time.Sleep(500 * time.Millisecond)
	}
}
