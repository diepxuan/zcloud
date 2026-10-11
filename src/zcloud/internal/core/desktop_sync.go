package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Desktop sync endpoints are the PC-side cross-device/backup helpers exposed
// by Zalo PC 26.8.10. They are not wired into the Web login flow by default;
// callers must supply the PC/device values returned by the desktop handshake.

func (c *Client) desktopParams() string {
	u := url.Values{}
	apiVersion := uint(665)
	apiType := uint(30)
	if c.Session != nil {
		if c.Session.APIVersion != 0 {
			apiVersion = c.Session.APIVersion
		}
		if c.Session.APIType != 0 {
			apiType = c.Session.APIType
		}
	}
	u.Set("zpw_ver", fmt.Sprintf("%d", apiVersion))
	u.Set("zpw_type", fmt.Sprintf("%d", apiType))
	return u.Encode()
}

func (c *Client) encodeDesktopPayload(v any) (string, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return "", ErrNotLoggedIn
	}
	rawKey, err := base64.StdEncoding.DecodeString(c.Session.SecretKey)
	if err != nil || len(rawKey) == 0 {
		if err == nil {
			err = fmt.Errorf("empty secret key")
		}
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	enc, err := EncodeAESCBC(rawKey, string(b))
	if err != nil {
		return "", err
	}
	return url.QueryEscape(enc), nil
}

func (c *Client) desktopGet(ctx context.Context, baseURL, path string, params url.Values) ([]byte, error) {
	serviceURL := fmt.Sprintf("%s%s?%s", baseURL, path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("zalo html response")
	}
	return body, nil
}

// GetCrossDB calls /api/message/get_crossdb.
func (c *Client) GetCrossDB(ctx context.Context, pcName, syncSession string) ([]byte, error) {
	enc, err := c.encodeDesktopPayload(map[string]any{"pc_name": pcName, "sync_session": syncSession})
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, vs := range mustParseQuery(c.desktopParams()) {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("params", enc)
	return c.desktopGet(ctx, serviceBaseURL(c.Session, ServiceKeyFile, "https://files-wpa.chat.zalo.me"), "/api/message/get_crossdb", q)
}

// PullMobileMsg calls /api/message/pull_mobile_msg.
func (c *Client) PullMobileMsg(ctx context.Context, pcName, publicKey string, fromSeqID int64, isRetry int, minSeqID int64, tempKey, imei string) ([]byte, error) {
	if pcName == "" {
		pcName = "Web"
	}
	if imei == "" {
		imei = c.Session.IMEI
	}
	enc, err := c.encodeDesktopPayload(map[string]any{
		"pc_name": pcName, "public_key": publicKey,
		"from_seq_id": fromSeqID, "is_retry": isRetry,
		"min_seq_id": minSeqID, "temp_key": tempKey, "imei": imei,
	})
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, vs := range mustParseQuery(c.desktopParams()) {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("params", enc)
	return c.desktopGet(ctx, serviceBaseURL(c.Session, ServiceKeyFile, "https://files-wpa.chat.zalo.me"), "/api/message/pull_mobile_msg", q)
}

// CancelPullMobileMsg calls /api/message/cancel_pull_mobile_msg.
func (c *Client) CancelPullMobileMsg(ctx context.Context, pcName, publicKey string) ([]byte, error) {
	enc, err := c.encodeDesktopPayload(map[string]any{
		"pc_name": pcName, "public_key": publicKey, "imei": c.Session.IMEI,
	})
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, vs := range mustParseQuery(c.desktopParams()) {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("params", enc)
	return c.desktopGet(ctx, serviceBaseURL(c.Session, ServiceKeyFile, "https://files-wpa.chat.zalo.me"), "/api/message/cancel_pull_mobile_msg", q)
}

// RequestTransferSync calls /api/transfer-sync-v2/request-sync.
func (c *Client) RequestTransferSync(ctx context.Context, reqID string, data any) ([]byte, error) {
	enc, err := c.encodeDesktopPayload(map[string]any{"reqId": reqID, "data": json.RawMessage(mustJSON(data))})
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, vs := range mustParseQuery(c.desktopParams()) {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("params", enc)
	return c.desktopGet(ctx, serviceBaseURL(c.Session, ServiceKeyFile, "https://files-wpa.chat.zalo.me"), "/api/transfer-sync-v2/request-sync", q)
}

// GetBackupMsgInfo calls /api/message/get_backupmsginfo.
func (c *Client) GetBackupMsgInfo(ctx context.Context) ([]byte, error) {
	return c.desktopGet(ctx, serviceBaseURL(c.Session, ServiceKeyFile, "https://files-wpa.chat.zalo.me"), "/api/message/get_backupmsginfo", mustParseQuery(c.desktopParams()))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func mustParseQuery(s string) url.Values {
	q, _ := url.ParseQuery(s)
	return q
}


// RequestSyncMessage yêu cầu server bắt đầu cross-device sync (cmd 590).
func (w *WSClient) RequestSyncMessage(ctx context.Context, data any) error {
	return w.SendWSWithID(ctx, 590, 0, map[string]any{"data": data})
}

// AckDeleteSyncSession xác nhận xoá session sync (cmd 591).
func (w *WSClient) AckDeleteSyncSession(ctx context.Context, data any) error {
	return w.SendWSWithID(ctx, 591, 0, map[string]any{"data": data})
}

// RequestMobileWakeUp đánh thức mobile để sync (cmd 592).
func (w *WSClient) RequestMobileWakeUp(ctx context.Context, data any) error {
	var payload map[string]any
	switch v := data.(type) {
	case map[string]any:
		payload = v
	case nil:
		payload = map[string]any{}
	default:
		payload = map[string]any{"data": v}
	}
	return w.SendWSWithID(ctx, 592, 0, payload)
}

// CreateBackupSession mở session backup (cmd 631).
func (w *WSClient) CreateBackupSession(ctx context.Context) error {
	return w.SendWSWithID(ctx, 631, 0, map[string]any{})
}

// InitBackupSession init session backup (cmd 630).
func (w *WSClient) InitBackupSession(ctx context.Context, data any) error {
	return w.SendWSWithID(ctx, 630, 0, map[string]any{"data": data})
}

// GetBackupPcMetadata lấy metadata backup từ PC (cmd 632).
func (w *WSClient) GetBackupPcMetadata(ctx context.Context) error {
	return w.SendWSWithID(ctx, 632, 0, map[string]any{})
}

// SignalRestoreOnMobile báo mobile khôi phục backup (cmd 633).
func (w *WSClient) SignalRestoreOnMobile(ctx context.Context) error {
	return w.SendWSWithID(ctx, 633, 0, map[string]any{})
}

// GetBackupConfigs lấy cấu hình backup (cmd 634).
func (w *WSClient) GetBackupConfigs(ctx context.Context) error {
	return w.SendWSWithID(ctx, 634, 0, map[string]any{})
}

