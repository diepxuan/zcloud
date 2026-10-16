package api

// Admin endpoints cho SyncV2 — T22.5.
// Cho phép Sếp trigger thủ công từ UI / curl, xem status, reset state.
// Endpoints:
//   POST /api/sync/v2/start?accountId=X — start RequestSync (reset state nếu cần)
//   POST /api/sync/v2/stop?accountId=X  — cancel PullBatch loop
//   POST /api/sync/v2/reset?accountId=X — reset state về init (xóa keypair + lastSeqID)
//   GET  /api/sync/v2/status?accountId=X — trả state hiện tại

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
)

// syncV2Action là action chung cho start/stop/reset.
type syncV2Action struct {
	AccountID string `json:"accountId"`
}

// HandleSyncV2Start bắt đầu SyncV2 flow cho account: nếu chưa có client
// thì tạo mới; gọi RequestSync (REST cmd 12888) — server sẽ push WS event
// 590/591/592 về client. Worker trong handleSyncV2Event sẽ tự chạy
// PullBatch loop khi nhận user_confirm.
func (s *Server) HandleSyncV2Start(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	sessRec, err := s.Store.LoadSessionByAccountID(accountID)
	if err != nil || sessRec == nil {
		fail(w, 404, "no session for account")
		return
	}
	clientObj, err := clientFromSession(sessRec)
	if err != nil {
		fail(w, 500, "build client: "+err.Error())
		return
	}
	entry, err := s.ensureSyncV2Client(accountID, clientObj.Session)
	if err != nil {
		fail(w, 500, "ensure syncv2 client: "+err.Error())
		return
	}
	// Reset phase nếu đang error/done để cho phép start lại.
	if p := entry.client.State().Phase; p == "error" || p == "done" {
		entry.client.SetPhase("init")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := entry.client.RequestSync(ctx); err != nil {
			s.Logger.Error("syncv2_manual_start_failed", "account_id", accountID, "error", err)
		} else {
			s.Logger.Info("syncv2_manual_start_sent", "account_id", accountID)
		}
		_ = persistSyncV2State(s.Store, accountID, entry.client)
	}()
	ok(w, map[string]any{"started": true, "accountId": accountID})
}

// HandleSyncV2Stop dừng PullBatch loop (nếu đang chạy) và set phase=stopped.
// WS hook vẫn nhận event nhưng không trigger pull nữa.
func (s *Server) HandleSyncV2Stop(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	s.syncV2Mu.Lock()
	entry, hasEntry := s.syncV2Registry[accountID]
	s.syncV2Mu.Unlock()
	if !hasEntry || entry == nil {
		fail(w, 404, "no syncv2 client for account")
		return
	}
	entry.client.SetPhase("stopped")
	if entry.cancel != nil {
		entry.cancel()
	}
	_ = persistSyncV2State(s.Store, accountID, entry.client)
	ok(w, map[string]any{"stopped": true, "accountId": accountID})
}

// HandleSyncV2Reset xóa hoàn toàn state (keypair + lastSeqID + phase) về
// init. Dùng khi muốn sync lại từ đầu.
func (s *Server) HandleSyncV2Reset(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	empty := core.SyncV2State{Phase: "init"}
	b, _ := json.Marshal(empty)
	if err := s.Store.SetAccountSyncV2State(accountID, string(b)); err != nil {
		fail(w, 500, "reset state: "+err.Error())
		return
	}
	// Drop client khỏi registry để lần start kế tiếp generate keypair mới.
	s.syncV2Mu.Lock()
	delete(s.syncV2Registry, accountID)
	s.syncV2Mu.Unlock()
	ok(w, map[string]any{"reset": true, "accountId": accountID})
}

// HandleSyncV2Status trả state JSON hiện tại của account (load từ DB).
// Trả {"phase":"...","lastSeqId":N,"pcName":"...","hasTempKey":bool,...}
func (s *Server) HandleSyncV2Status(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		fail(w, 400, "missing accountId")
		return
	}
	raw, err := s.Store.GetAccountSyncV2State(accountID)
	if err != nil {
		fail(w, 500, "load state: "+err.Error())
		return
	}
	out := map[string]any{"raw": raw, "accountId": accountID}
	if raw != "" && raw != "{}" {
		var st core.SyncV2State
		if err := json.Unmarshal([]byte(raw), &st); err == nil {
			out["phase"] = st.Phase
			out["lastSeqId"] = st.LastSeqID
			out["pcName"] = st.PCName
			out["imei"] = st.IMEI
			out["hasTempKey"] = st.TempKey != ""
			out["hasPublicKey"] = st.PublicKey != ""
			out["lastError"] = st.LastError
		}
	}
	// Check runtime client.
	s.syncV2Mu.Lock()
	entry, hasEntry := s.syncV2Registry[accountID]
	s.syncV2Mu.Unlock()
	if hasEntry && entry != nil {
		entry.pullMu.Lock()
		out["pullRunning"] = entry.pullRunning
		entry.pullMu.Unlock()
	}
	ok(w, out)
}
