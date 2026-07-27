package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diepxuan/zcloud/internal/store"
)

// SetupRouter gắn tất cả routes vào mux
func SetupRouter(mux *http.ServeMux, s *Server, db *store.Store) {
	// ====================================
	// Static pages
	// ====================================
	mux.HandleFunc("GET /", s.HandleLoginPage)
	mux.HandleFunc("GET /chat", s.HandleChatPage)

	// ====================================
	// API — Auth
	// ====================================
	mux.HandleFunc("GET /api/health", s.HandleHealth)
	mux.HandleFunc("GET /api/account", s.HandleAccount)
	mux.HandleFunc("GET /api/qr/create", s.HandleCreateQR)
	mux.HandleFunc("POST /api/qr/poll", s.HandlePollQR)
	mux.HandleFunc("POST /api/login/cookie", s.HandleCookieLogin)

	// ====================================
	// API — Conversations & Messages
	// ====================================
	mux.HandleFunc("GET /api/conversations", s.HandleConversations)
	mux.HandleFunc("GET /api/conversations/sync", s.HandleSyncConversations)
	mux.HandleFunc("GET /api/messages", s.HandleMessages)
	mux.HandleFunc("POST /api/messages/send", s.HandleSendMessage)
	mux.HandleFunc("POST /api/messages/sync", s.HandleSyncMessages)
	mux.HandleFunc("GET /api/friends", s.HandleFriends)
	mux.HandleFunc("POST /api/logout", s.HandleLogout)
	mux.HandleFunc("GET /ws", s.HandleWS)

	// ====================================
	// API — Media serving
	// ====================================
	mux.Handle("GET /media/", http.StripPrefix("/media/", http.HandlerFunc(s.HandleMedia)))
	mux.HandleFunc("POST /api/media/download", s.HandleMediaDownload)
}

// HandleMediaDownload tải media từ Zalo URL về local
func (s *Server) HandleMediaDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		ConvID    string `json:"convId"`
		URL       string `json:"url"`
		FileName  string `json:"fileName"`
		FileExt   string `json:"fileExt"`
		MsgID     string `json:"msgId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { fail(w, 400, "invalid body"); return }
	if req.AccountID == "" || req.URL == "" { fail(w, 400, "missing fields"); return }
	if req.FileExt == "" { req.FileExt = "bin" }

	// Tải file từ Zalo
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second); defer cancel()
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", req.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil { fail(w, 500, "download: "+err.Error()); return }
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil { fail(w, 500, "read: "+err.Error()); return }

	// Lưu file và metadata
	mediaDir := s.Store.MediaDir(req.AccountID, req.ConvID)
	fileID := req.MsgID
	if fileID == "" { fileID = fmt.Sprintf("%d", time.Now().UnixMilli()) }
	filePath := filepath.Join(mediaDir, fileID+"."+req.FileExt)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		fail(w, 500, "save: "+err.Error()); return
	}

	// Save metadata vào DB
	fileInfo, _ := os.Stat(filePath)
	savedPath, err := s.Store.SaveMedia(&store.MediaFile{
		ID: fileID, AccountID: req.AccountID, ConvID: req.ConvID, MsgID: req.MsgID,
		FileName: req.FileName, FileExt: req.FileExt, FileSize: fileInfo.Size(),
		SourceURL: req.URL, IsDownloaded: 1,
	})
	if err != nil { s.Logger.Printf("media save meta: %v", err) }

	ok(w, map[string]interface{}{
		"path": savedPath, "fileId": fileID,
		"url": "/media/" + req.AccountID + "/" + req.ConvID + "/" + fileID + "." + req.FileExt,
	})
}

// HandleMedia phục vụ file media từ disk
func (s *Server) HandleMedia(w http.ResponseWriter, r *http.Request) {
	// Path: /media/{accountID}/{convID}/{fileID}.{ext}
	path := r.URL.Path
	if path == "" || path == "/" {
		fail(w, http.StatusNotFound, "not found")
		return
	}

	// Kiểm tra file tồn tại
	fullPath := s.Store.MediaPath() + "/" + path
	if !strings.Contains(path, "..") { // basic path traversal check
		http.ServeFile(w, r, fullPath)
	} else {
		fail(w, http.StatusForbidden, "invalid path")
	}
}

// StartServer khởi tạo và chạy HTTP server
func StartServer(port int, db *store.Store) error {
	s := NewServer(db, nil)

	mux := http.NewServeMux()
	SetupRouter(mux, s, db)

	// Logging middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		mux.ServeHTTP(w, r)
		// Simple request log
		fmt.Printf("[zcloud] %s %s — %v\n", r.Method, r.URL.Path, time.Since(start))
	})

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("[zcloud] Server listening on %s\n", addr)
	return http.ListenAndServe(addr, handler)
}

// ====================================
// Enhanced router — hỗ trợ JSON response cho API
// ====================================

// JSONMiddleware parse request body nếu là JSON API call
func JSONMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") && (r.Method == "POST" || r.Method == "PUT") {
			// Body đã được đọc trong handler
		}
		next.ServeHTTP(w, r)
	})
}

// RenderError trả về error page HTML
func RenderError(w http.ResponseWriter, status int, title string, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;background:#f5f5f5}
.card{background:#fff;border-radius:12px;padding:32px;text-align:center;max-width:400px}
h1{color:#e74c3c;font-size:24px;margin-bottom:8px}
p{color:#666;font-size:14px}
</style></head><body><div class="card"><h1>%d</h1><h2>%s</h2><p>%s</p><a href="/" style="display:inline-block;margin-top:16px;padding:8px 20px;background:#0068ff;color:#fff;border-radius:6px;text-decoration:none">Quay lại</a></div></body></html>`, title, status, title, detail)
}

// Helper để parse JSON body một lần
func ParseJSONBody(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
