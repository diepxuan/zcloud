package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ====================================
// Zalo REST API calls (chat)
// ====================================

// Client đại diện cho một Zalo client đã đăng nhập
type Client struct {
	Session *Session
	WS      *WSClient
	client  *http.Client
}

// NewClient tạo client mới từ session
func NewClient(session *Session) *Client {
	return &Client{
		Session: session,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ConnectWS kết nối WebSocket real-time
func (c *Client) ConnectWS(ctx context.Context) error {
	c.WS = NewWSClient(c.Session)
	return c.WS.Connect(ctx)
}

// ====================================
// Send message
// ====================================

// SendMessage gửi tin nhắn text tới conversation
func (c *Client) SendMessage(ctx context.Context, to, content string, msgType MsgType) (*Message, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return nil, ErrNotLoggedIn
	}

	// Build params
	params := map[string]any{
		"message":   content,
		"clientId":  generateClientID(),
		"imei":      c.Session.IMEI,
		"msgType":   int(msgType),
		"to":        to,
		"ts":        time.Now().UnixMilli(),
	}

	// Encrypt params
	encResult, err := encryptParamsForLogin(c.Session, true, "sendreq")
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	// Build URL
	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}

	apiURL := "https://wpa.chat.zalo.me/api/message/sendreq?" + query.Encode()

	// Send POST
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var sendResp struct {
		ErrorCode int             `json:"error_code"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sendResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if sendResp.ErrorCode != 0 {
		return nil, fmt.Errorf("send error %d", sendResp.ErrorCode)
	}

	msg := &Message{
		ID:        params["clientId"].(string),
		Content:   content,
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
	}

	return msg, nil
}

// ====================================
// Get conversations
// ====================================

// GetConversations lấy danh sách hội thoại
func (c *Client) GetConversations(ctx context.Context) ([]Conversation, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return nil, ErrNotLoggedIn
	}

	// Encrypt params
	encResult, err := encryptParamsForLogin(c.Session, true, "getsmsreq")
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}

	apiURL := "https://wpa.chat.zalo.me/api/conversation/getsmsreq?" + query.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("conversations request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var convResp struct {
		ErrorCode int              `json:"error_code"`
		Data      *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&convResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if convResp.ErrorCode != 0 {
		return nil, fmt.Errorf("conversations error %d", convResp.ErrorCode)
	}

	if convResp.Data == nil {
		return []Conversation{}, nil
	}

	// Parse conversations
	var conversations []Conversation
	if err := json.Unmarshal(*convResp.Data, &conversations); err != nil {
		return nil, fmt.Errorf("parse conversations: %w", err)
	}

	return conversations, nil
}

// ====================================
// Friends
// ====================================

// GetFriends lấy danh sách bạn bè
func (c *Client) GetFriends(ctx context.Context) ([]User, error) {
	if c.Session == nil {
		return nil, ErrNotLoggedIn
	}

	// Encrypt params
	encResult, err := encryptParamsForLogin(c.Session, true, "getfriends")
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}

	apiURL := "https://wpa.chat.zalo.me/api/friend/getfriends?" + query.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("friends request: %w", err)
	}
	defer resp.Body.Close()

	var friendResp struct {
		ErrorCode int              `json:"error_code"`
		Data      *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&friendResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if friendResp.ErrorCode != 0 {
		return nil, fmt.Errorf("friends error %d", friendResp.ErrorCode)
	}

	if friendResp.Data == nil {
		return []User{}, nil
	}

	var users []User
	if err := json.Unmarshal(*friendResp.Data, &users); err != nil {
		return nil, fmt.Errorf("parse friends: %w", err)
	}

	return users, nil
}

// ====================================
// Helpers
// ====================================

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.Session.UserAgent)
	if c.Session.UserAgent == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://chat.zalo.me")
	req.Header.Set("Referer", "https://chat.zalo.me/")

	// Set cookies
	cookieParts := make([]string, 0, len(c.Session.Cookies))
	for k, v := range c.Session.Cookies {
		cookieParts = append(cookieParts, k+"="+v)
	}
	req.Header.Set("Cookie", strings.Join(cookieParts, "; "))
}

func generateClientID() string {
	return fmt.Sprintf("zcloud-%d", time.Now().UnixNano())
}
