package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

type Client struct {
	Session *Session
	WS      *WSClient
	client  *http.Client
}

func NewClient(session *Session) *Client {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 30 * time.Second, Jar: jar}
	if session.Cookies != nil {
		domains := []string{"https://chat.zalo.me/", "https://wpa.chat.zalo.me/", "https://tt-convers-wpa.chat.zalo.me/"}
		for k, v := range session.Cookies {
			ck := &http.Cookie{Name: k, Value: v, Path: "/"}
			for _, d := range domains {
				u, _ := url.Parse(d)
				jar.SetCookies(u, []*http.Cookie{ck})
			}
		}
	}
	return &Client{Session: session, client: client}
}

func (c *Client) ConnectWS(ctx context.Context) error {
	c.WS = NewWSClient(c.Session)
	return c.WS.Connect(ctx)
}

func (c *Client) SendMessage(ctx context.Context, to, content string, msgType MsgType) (*Message, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return nil, ErrNotLoggedIn
	}
	params := map[string]any{
		"message": content, "clientId": generateClientID(), "imei": c.Session.IMEI,
		"msgType": int(msgType), "to": to, "ts": time.Now().UnixMilli(),
	}
	encResult, err := encryptParamsForLogin(c.Session, true, "sendreq")
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	query := url.Values{}
	for k, v := range encResult.Params {
		query.Set(k, fmt.Sprintf("%v", v))
	}
	apiURL := "https://wpa.chat.zalo.me/api/message/sendreq?" + query.Encode()
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	c.setHeaders(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	var sendResp struct {
		ErrorCode int             `json:"error_code"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sendResp); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if sendResp.ErrorCode != 0 {
		return nil, fmt.Errorf("send error %d", sendResp.ErrorCode)
	}
	return &Message{
		ID: params["clientId"].(string), Content: content,
		Type: msgType, Timestamp: time.Now().UnixMilli(),
	}, nil
}

func (c *Client) GetConversations(ctx context.Context) ([]Conversation, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return nil, ErrNotLoggedIn
	}
	rawKey, err := base64.StdEncoding.DecodeString(c.Session.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	paramsEnc, err := EncodeAESCBC(rawKey, `{"threadIdLocalMsgId":"{}","imei":"`+c.Session.IMEI+`"}`)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	query := url.Values{}
	query.Set("zpw_ver", fmt.Sprintf("%d", c.Session.APIVersion))
	query.Set("zpw_type", fmt.Sprintf("%d", c.Session.APIType))
	query.Set("params", paramsEnc)
	apiURL := "https://tt-convers-wpa.chat.zalo.me/api/preloadconvers/get-last-msgs?" + query.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	c.setHeaders(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("convs: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("zalo html: %s", string(body[:min(200, len(body))]))
	}

	var convResp struct {
		ErrorCode int              `json:"error_code"`
		Data      *json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &convResp); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if convResp.ErrorCode != 0 {
		return nil, fmt.Errorf("conv error %d", convResp.ErrorCode)
	}
	if convResp.Data == nil {
		return []Conversation{}, nil
	}

	var dataStr string
	if err := json.Unmarshal(*convResp.Data, &dataStr); err != nil {
		return nil, fmt.Errorf("data str: %w", err)
	}
	decrypted, err := DecodeAESCBC(rawKey, dataStr)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	var rawData map[string]any
	if err := json.Unmarshal(decrypted, &rawData); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	convs := make([]Conversation, 0)
	if dataObj, ok := rawData["data"].(map[string]any); ok {
		if unreads, ok := dataObj["clearUnreads"].([]any); ok {
			for _, item := range unreads {
				if m, ok := item.(map[string]any); ok {
					conv := Conversation{ID: toString(m["idTo"]), Name: toString(m["idTo"])}
					if g, ok := m["isGroup"].(float64); ok && g == 1 {
						conv.Type = ConvGroup
					}
					convs = append(convs, conv)
				}
			}
		}
	}
	return convs, nil
}

func (c *Client) GetFriends(ctx context.Context) ([]User, error) {
	if c.Session == nil {
		return nil, ErrNotLoggedIn
	}
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
		return nil, fmt.Errorf("friends: %w", err)
	}
	defer resp.Body.Close()

	var friendResp struct {
		ErrorCode int              `json:"error_code"`
		Data      *json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&friendResp); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if friendResp.ErrorCode != 0 {
		return nil, fmt.Errorf("friends error %d", friendResp.ErrorCode)
	}
	return []User{}, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.Session.UserAgent)
	if c.Session.UserAgent == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://chat.zalo.me")
	req.Header.Set("Referer", "https://chat.zalo.me/")
}

func generateClientID() string {
	return fmt.Sprintf("zcloud-%d", time.Now().UnixNano())
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
