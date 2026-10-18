package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// mockAESKey là khoá AES-128 (16 byte ASCII) dùng chung cho các test mock
// SendMessage. SecretKey của session = base64(key).
const mockAESKey = "0123456789abcdef"

// newChatMockSession dựng session đã "login" và trỏ ServiceMap[chat] về
// baseURL của mock server (httptest). Trả kèm Client thật qua NewClient để
// chạy đúng vòng đời HTTP request/response.
func newChatMockSession(baseURL string) (*Session, *Client) {
	key := []byte(mockAESKey)
	sess := &Session{
		SecretKey:  base64.StdEncoding.EncodeToString(key),
		IMEI:       "imei-123456",
		UserAgent:  "zcloud-test",
		APIType:    30,
		APIVersion: 688,
		ServiceMap: map[string][]string{ServiceKeyChat: {baseURL}},
	}
	return sess, NewClient(sess)
}

// newProfileMockSession dựng session đã "login" phục vụ GetMyProfile/GetFriends:
// trỏ cả ServiceKeyProfile và ServiceKeyChat về baseURL của mock server và có
// UserID ("12345") để key profile là "12345_0".
func newProfileMockSession(baseURL string) (*Session, *Client) {
	key := []byte(mockAESKey)
	sess := &Session{
		SecretKey:  base64.StdEncoding.EncodeToString(key),
		IMEI:       "imei-123456",
		UserID:     "12345",
		UserAgent:  "zcloud-test",
		APIType:    30,
		APIVersion: 688,
		ServiceMap: map[string][]string{
			ServiceKeyChat:    {baseURL},
			ServiceKeyProfile: {baseURL},
		},
	}
	return sess, NewClient(sess)
}

// TestSendMessage_RoundTrip phủ nhánh thành công: mock server kiểm request
// (POST, path, query zpw_*, form params giải mã được) rồi trả data là JSON
// string base64(EncodeAESCBC(key, envelope thật
// {"error_code":0,"data":{"msgId":"m-1"}})).
func TestSendMessage_RoundTrip(t *testing.T) {
	key := []byte(mockAESKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/message/sms" {
			t.Errorf("path = %s, want /api/message/sms", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("zpw_ver"); got != "688" {
			t.Errorf("zpw_ver = %q, want 688", got)
		}
		if got := q.Get("zpw_type"); got != "30" {
			t.Errorf("zpw_type = %q, want 30", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		params := r.FormValue("params")
		if params == "" {
			t.Error("form params rỗng")
		}
		plain, err := DecodeAESCBC(key, params)
		if err != nil {
			t.Errorf("DecodeAESCBC(params): %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Errorf("params không phải JSON: %v (%s)", err, string(plain))
		}
		if got["message"] != "hello" {
			t.Errorf("params.message = %v, want hello", got["message"])
		}
		if got["toid"] != "uid-42" {
			t.Errorf("params.toid = %v, want uid-42", got["toid"])
		}
		if got["imei"] != "imei-123456" {
			t.Errorf("params.imei = %v, want imei-123456", got["imei"])
		}
		if cid, _ := got["clientId"].(string); cid == "" {
			t.Error("params.clientId rỗng")
		}

		encData, _ := EncodeAESCBC(key, `{"error_code":0,"data":{"msgId":"m-1"}}`)
		out, _ := json.Marshal(map[string]any{"error_code": 0, "data": encData})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	_, c := newChatMockSession(srv.URL)
	msg, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("msg nil")
	}
	if msg.ID != "m-1" {
		t.Errorf("msg.ID = %q, want m-1", msg.ID)
	}
	if msg.Content != "hello" {
		t.Errorf("msg.Content = %q, want hello", msg.Content)
	}
	if msg.Type != MsgTypeText {
		t.Errorf("msg.Type = %v, want %v", msg.Type, MsgTypeText)
	}
}

// TestSendMessage_ErrorCode: error_code != 0 → lỗi chứa "send error".
func TestSendMessage_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error_code":123,"error_message":"nope"}`))
	}))
	t.Cleanup(srv.Close)

	_, c := newChatMockSession(srv.URL)
	msg, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "send error") {
		t.Errorf("err = %v, want contains 'send error'", err)
	}
	if msg != nil {
		t.Errorf("msg = %v, want nil", msg)
	}
}

// TestSendMessage_HTMLResponse: body bắt đầu '<' → lỗi chứa "zalo html".
func TestSendMessage_HTMLResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>rate limit</html>"))
	}))
	t.Cleanup(srv.Close)

	_, c := newChatMockSession(srv.URL)
	_, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "zalo html") {
		t.Errorf("err = %v, want contains 'zalo html'", err)
	}
}

// TestSendMessage_InvalidJSON: body không phải JSON → lỗi chứa "parse".
func TestSendMessage_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	_, c := newChatMockSession(srv.URL)
	_, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("err = %v, want contains 'parse'", err)
	}
}

// TestSendMessage_NotLoggedIn: SecretKey rỗng → ErrNotLoggedIn, không gọi server.
func TestSendMessage_NotLoggedIn(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"error_code":0}`))
	}))
	t.Cleanup(srv.Close)

	sess := &Session{
		IMEI:       "imei-123456",
		APIType:    30,
		APIVersion: 688,
		ServiceMap: map[string][]string{ServiceKeyChat: {srv.URL}},
	}
	c := NewClient(sess)
	_, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Errorf("server calls = %d, want 0", n)
	}
}

// TestSendMessage_DataPlainObject: data là object thô {"msgId":"x-9"} (không
// mã hoá) → vẫn lấy được Message.ID.
func TestSendMessage_DataPlainObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error_code":0,"data":{"msgId":"x-9"}}`))
	}))
	t.Cleanup(srv.Close)

	_, c := newChatMockSession(srv.URL)
	msg, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("msg nil")
	}
	if msg.ID != "x-9" {
		t.Errorf("msg.ID = %q, want x-9", msg.ID)
	}
	if msg.Content != "hello" {
		t.Errorf("msg.Content = %q, want hello", msg.Content)
	}
}

// TestSendMessage_DataTopLevelMsgID: data là JSON string base64(EncodeAESCBC(
// key, {"msgId":"t-7"})) — sau giải mã msgId nằm ở top-level (không envelope).
func TestSendMessage_DataTopLevelMsgID(t *testing.T) {
	key := []byte(mockAESKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encData, _ := EncodeAESCBC(key, `{"msgId":"t-7"}`)
		out, _ := json.Marshal(map[string]any{"error_code": 0, "data": encData})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	_, c := newChatMockSession(srv.URL)
	msg, err := c.SendMessage(context.Background(), "uid-42", "hello", MsgTypeText)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("msg nil")
	}
	if msg.ID != "t-7" {
		t.Errorf("msg.ID = %q, want t-7", msg.ID)
	}
	if msg.Content != "hello" {
		t.Errorf("msg.Content = %q, want hello", msg.Content)
	}
}

// TestGetMyProfile_RoundTrip phủ nhánh thành công: mock assert POST + path
// /api/social/friend/getprofiles/v2 + query zpw_ver/zpw_type + form params giải
// mã được; trả data = string base64(EncodeAESCBC(key, envelope có wrapper data
// {changed_profiles:{"12345_0":{...}}})).
func TestGetMyProfile_RoundTrip(t *testing.T) {
	key := []byte(mockAESKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/social/friend/getprofiles/v2" {
			t.Errorf("path = %s, want /api/social/friend/getprofiles/v2", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("zpw_ver"); got != "688" {
			t.Errorf("zpw_ver = %q, want 688", got)
		}
		if got := q.Get("zpw_type"); got != "30" {
			t.Errorf("zpw_type = %q, want 30", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		params := r.FormValue("params")
		if params == "" {
			t.Error("form params rỗng")
		}
		plain, err := DecodeAESCBC(key, params)
		if err != nil {
			t.Errorf("DecodeAESCBC(params): %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Errorf("params không phải JSON: %v (%s)", err, string(plain))
		}
		if got["imei"] != "imei-123456" {
			t.Errorf("params.imei = %v, want imei-123456", got["imei"])
		}
		if ids, ok := got["friend_pversion_map"].([]any); !ok || len(ids) != 1 || ids[0] != "12345_0" {
			t.Errorf("params.friend_pversion_map = %v, want [12345_0]", got["friend_pversion_map"])
		}

		encData, _ := EncodeAESCBC(key, `{"data":{"changed_profiles":{"12345_0":{"displayName":"Tên Có Dấu","zaloName":"ten.khongdau","avatar":"https://x/a.jpg"}}}}`)
		out, _ := json.Marshal(map[string]any{"error_code": 0, "data": encData})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	name, avatar, err := c.GetMyProfile(context.Background())
	if err != nil {
		t.Fatalf("GetMyProfile: %v", err)
	}
	if name != "Tên Có Dấu" {
		t.Errorf("name = %q, want Tên Có Dấu", name)
	}
	if avatar != "https://x/a.jpg" {
		t.Errorf("avatar = %q, want https://x/a.jpg", avatar)
	}
}

// TestGetMyProfile_FlatFallback: decrypt ra flat JSON không wrapper data
// ({changed_profiles:{...}}) → vẫn ra name/avatar.
func TestGetMyProfile_FlatFallback(t *testing.T) {
	key := []byte(mockAESKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encData, _ := EncodeAESCBC(key, `{"changed_profiles":{"12345_0":{"displayName":"Tên Phẳng","avatar":"https://x/flat.jpg"}}}`)
		out, _ := json.Marshal(map[string]any{"error_code": 0, "data": encData})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	name, avatar, err := c.GetMyProfile(context.Background())
	if err != nil {
		t.Fatalf("GetMyProfile: %v", err)
	}
	if name != "Tên Phẳng" {
		t.Errorf("name = %q, want Tên Phẳng", name)
	}
	if avatar != "https://x/flat.jpg" {
		t.Errorf("avatar = %q, want https://x/flat.jpg", avatar)
	}
}

// TestGetMyProfile_ErrorCode: error_code != 0 → ("","",nil) không lỗi.
func TestGetMyProfile_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error_code":5}`))
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	name, avatar, err := c.GetMyProfile(context.Background())
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if name != "" || avatar != "" {
		t.Errorf("(name, avatar) = (%q, %q), want (\"\", \"\")", name, avatar)
	}
}

// TestGetFriends_RoundTrip phủ nhánh thành công: mock assert GET + path
// /api/social/friend/getfriends + query params/zpw_ver/zpw_type; trả data =
// string base64(EncodeAESCBC(key, {data:[...]})).
func TestGetFriends_RoundTrip(t *testing.T) {
	key := []byte(mockAESKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/social/friend/getfriends" {
			t.Errorf("path = %s, want /api/social/friend/getfriends", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("zpw_ver"); got != "688" {
			t.Errorf("zpw_ver = %q, want 688", got)
		}
		if got := q.Get("zpw_type"); got != "30" {
			t.Errorf("zpw_type = %q, want 30", got)
		}
		params := q.Get("params")
		if params == "" {
			t.Error("query params rỗng")
		}
		plain, err := DecodeAESCBC(key, params)
		if err != nil {
			t.Errorf("DecodeAESCBC(params): %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Errorf("params không phải JSON: %v (%s)", err, string(plain))
		}
		if _, ok := got["count"]; !ok {
			t.Errorf("params thiếu count: %s", string(plain))
		}

		encData, _ := EncodeAESCBC(key, `{"data":[{"userId":"u1","displayName":"Bạn Một","avatar":"a1"},{"userId":"u2","displayName":"Bạn Hai","avatar":"a2"}]}`)
		out, _ := json.Marshal(map[string]any{"error_code": 0, "data": encData})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	users, err := c.GetFriends(context.Background())
	if err != nil {
		t.Fatalf("GetFriends: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].ID != "u1" || users[0].Name != "Bạn Một" || users[0].Avatar != "a1" {
		t.Errorf("users[0] = %+v, want {u1 Bạn Một a1}", users[0])
	}
	if users[1].ID != "u2" || users[1].Name != "Bạn Hai" || users[1].Avatar != "a2" {
		t.Errorf("users[1] = %+v, want {u2 Bạn Hai a2}", users[1])
	}
}

// TestGetFriends_FlatArray: decrypt ra mảng trực tiếp [...] → len==1.
func TestGetFriends_FlatArray(t *testing.T) {
	key := []byte(mockAESKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encData, _ := EncodeAESCBC(key, `[{"userId":"u3","displayName":"Ba","avatar":"a3"}]`)
		out, _ := json.Marshal(map[string]any{"error_code": 0, "data": encData})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	users, err := c.GetFriends(context.Background())
	if err != nil {
		t.Fatalf("GetFriends: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	if users[0].ID != "u3" || users[0].Name != "Ba" || users[0].Avatar != "a3" {
		t.Errorf("users[0] = %+v, want {u3 Ba a3}", users[0])
	}
}

// TestGetFriends_HTML: body bắt đầu '<' → lỗi chứa "html".
func TestGetFriends_HTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>rate</html>"))
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	_, err := c.GetFriends(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "html") {
		t.Errorf("err = %v, want contains 'html'", err)
	}
}

// TestGetFriends_ErrorCode: error_code != 0 → lỗi.
func TestGetFriends_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error_code":9}`))
	}))
	t.Cleanup(srv.Close)

	_, c := newProfileMockSession(srv.URL)
	users, err := c.GetFriends(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if users != nil {
		t.Errorf("users = %v, want nil", users)
	}
}
