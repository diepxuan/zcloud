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
	"strings"
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
		domains := []string{"https://chat.zalo.me/", "https://wpa.chat.zalo.me/", "https://tt-convers-wpa.chat.zalo.me/", "https://profile-wpa.chat.zalo.me/", "https://group-wpa.chat.zalo.me/", "https://tt-profile-wpa.chat.zalo.me/"}
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

	// Build name map từ msgs
	names := make(map[string]string)
	if dataObj, ok := rawData["data"].(map[string]any); ok {
		for _, key := range []string{"msgs", "groupMsgs", "pageMsgs"} {
			if msgs, ok := dataObj[key].([]any); ok {
				for _, msg := range msgs {
					if m, ok := msg.(map[string]any); ok {
						uid := toString(m["uidFrom"])
						dName := toString(m["dName"])
						if uid != "" && dName != "" {
							if _, exists := names[uid]; !exists {
								names[uid] = dName
							}
						}
						// Group name from group msgs
						gid := toString(m["grid"])
						gName := toString(m["dName"])
						if gid != "" && gName != "" {
							if _, exists := names[gid]; !exists {
								names[gid] = gName
							}
						}
					}
				}
			}
		}
	}

	convs := make([]Conversation, 0)
	if dataObj, ok := rawData["data"].(map[string]any); ok {
		if unreads, ok := dataObj["clearUnreads"].([]any); ok {
			for _, item := range unreads {
				if m, ok := item.(map[string]any); ok {
					idTo := toString(m["idTo"])
					name := toString(m["userName"])
					if name == "" { name = idTo }
					if n, ok := names[idTo]; ok { name = n }
					conv := Conversation{ID: idTo, Name: name}
					if g, ok := m["isGroup"].(float64); ok && g == 1 {
						conv.Type = ConvGroup
					}
					convs = append(convs, conv)
				}
			}
		}
	}
	// Resolve names từ API
	resolveNames(c, convs)

	return convs, nil
}

func resolveNames(c *Client, convs []Conversation) {
	ids := make([]string, 0, len(convs))
	for _, cc := range convs {
		if cc.Type == ConvIndividual { ids = append(ids, cc.ID) }
	}
	if len(ids) == 0 { return }

	fpm := make([]string, len(ids))
	for i, uid := range ids {
		if !strings.Contains(uid, "_") { fpm[i] = uid + "_0" } else { fpm[i] = uid }
	}

	if c.Session.SecretKey == "" { return }
	rawKey, err := base64.StdEncoding.DecodeString(c.Session.SecretKey)
	if err != nil || len(rawKey) == 0 {
		fmt.Printf("[zcloud] resolveNames: bad key\n"); return
	}

	payload := map[string]any{
		"friend_pversion_map": fpm, "avatar_size": 120,
		"language": "vi", "show_online_status": 1, "imei": c.Session.IMEI,
	}
	jsonP, _ := json.Marshal(payload)
	enc, err := EncodeAESCBC(rawKey, string(jsonP))
	if err != nil { fmt.Printf("[zcloud] resolveNames: encrypt err=%v\n", err); return }

	baseURL := "https://profile-wpa.chat.zalo.me"
	if c.Session.ServiceMap != nil {
		if p, ok := c.Session.ServiceMap["profile"]; ok && len(p) > 0 { baseURL = p[0] }
	}
	serviceURL := fmt.Sprintf("%s/api/social/friend/getprofiles/v2?zpw_ver=%d&zpw_type=%d",
		baseURL, c.Session.APIVersion, c.Session.APIType)
	bodyStr := "params=" + url.QueryEscape(enc)
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "POST", serviceURL, strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil { fmt.Printf("[zcloud] resolveNames: req err=%v\n", err); return }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[zcloud] resolveNames: raw=%s\n", string(body)[:min(300, len(body))])

	var result struct {
		ErrorCode int              `json:"error_code"`
		Data      *json.RawMessage `json:"data"`
	}
	json.Unmarshal(body, &result)
	if result.ErrorCode != 0 {
		fmt.Printf("[zcloud] resolveNames: api error=%d\n", result.ErrorCode)
		return
	}
	if result.Data == nil { return }

	var dataStr string
	if err := json.Unmarshal(*result.Data, &dataStr); err != nil { return }

	decrypted, err := DecodeAESCBC(rawKey, dataStr)
	if err != nil { fmt.Printf("[zcloud] resolveNames: decrypt err=%v len=%d\n", err, len(dataStr)); return }

	var profilesWrap struct {
		Data *json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &profilesWrap); err != nil { fmt.Printf("[zcloud] resolveNames: wrap parse err=%v\n", err); return }
	if profilesWrap.Data == nil { fmt.Printf("[zcloud] resolveNames: no data in wrapper\n"); return }

	var profiles struct {
		ChangedProfiles map[string]struct {
			ZaloName    string `json:"zaloName"`
			DisplayName string `json:"displayName"`
			Avatar      string `json:"avatar"`
		} `json:"changed_profiles"`
	}
	if err := json.Unmarshal(*profilesWrap.Data, &profiles); err != nil { fmt.Printf("[zcloud] resolveNames: parse err=%v\n", err); return }

	for i := range convs {
		if p, ok := profiles.ChangedProfiles[convs[i].ID]; ok {
			n := p.ZaloName; if n == "" { n = p.DisplayName }
			if n != "" { convs[i].Name = n }
			if p.Avatar != "" { convs[i].Avatar = p.Avatar }
		}
	}
}

// GetMyProfile lấy tên + avatar của chính user đang login
func (c *Client) GetMyProfile(ctx context.Context) (string, string, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return "", "", ErrNotLoggedIn
	}
	ids := []string{c.Session.UserID + "_0"}
	rawKey, err := base64.StdEncoding.DecodeString(c.Session.SecretKey)
	if err != nil || len(rawKey) == 0 { return "", "", err }

	payload := map[string]any{
		"friend_pversion_map": ids, "avatar_size": 120,
		"language": "vi", "show_online_status": 1, "imei": c.Session.IMEI,
	}
	jsonP, _ := json.Marshal(payload)
	enc, err := EncodeAESCBC(rawKey, string(jsonP))
	if err != nil { return "", "", err }

	baseURL := "https://profile-wpa.chat.zalo.me"
	if c.Session.ServiceMap != nil {
		if p, ok := c.Session.ServiceMap["profile"]; ok && len(p) > 0 { baseURL = p[0] }
	}
	serviceURL := fmt.Sprintf("%s/api/social/friend/getprofiles/v2?zpw_ver=%d&zpw_type=%d",
		baseURL, c.Session.APIVersion, c.Session.APIType)
	bodyStr := "params=" + url.QueryEscape(enc)
	req, _ := http.NewRequestWithContext(ctx, "POST", serviceURL, strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil { return "", "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[zcloud] GetMyProfile: raw=%s\n", string(body[:min(300, len(body))]))

	var result struct {
		ErrorCode int              `json:"error_code"`
		Data      *json.RawMessage `json:"data"`
	}
	json.Unmarshal(body, &result)
	if result.ErrorCode != 0 {
		fmt.Printf("[zcloud] GetMyProfile: api error=%d\n", result.ErrorCode)
		return "", "", nil
	}
	if result.Data == nil { return "", "", nil }

	var dataStr string
	json.Unmarshal(*result.Data, &dataStr)

	decrypted, err := DecodeAESCBC(rawKey, dataStr)
	if err != nil { return "", "", err }

	var wrap struct {
		Data *json.RawMessage `json:"data"`
	}
	json.Unmarshal(decrypted, &wrap)
	if wrap.Data == nil { return "", "", nil }

	var profiles struct {
		ChangedProfiles map[string]struct {
			ZaloName    string `json:"zaloName"`
			DisplayName string `json:"displayName"`
			Avatar      string `json:"avatar"`
		} `json:"changed_profiles"`
	}
	json.Unmarshal(*wrap.Data, &profiles)

	if p, ok := profiles.ChangedProfiles[c.Session.UserID+"_0"]; ok {
		n := p.ZaloName; if n == "" { n = p.DisplayName }
		return n, p.Avatar, nil
	}
	return "", "", nil
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

// GetGroupInfo lấy thông tin nhóm (tên, avatar)
func (c *Client) GetGroupInfo(ctx context.Context, groupIDs []string) (map[string]struct{ Name, Avatar string }, error) {
	if c.Session == nil || c.Session.SecretKey == "" {
		return nil, ErrNotLoggedIn
	}
	rawKey, err := base64.StdEncoding.DecodeString(c.Session.SecretKey)
	if err != nil || len(rawKey) == 0 { return nil, err }

	gridVerMap := make(map[string]int)
	for _, gid := range groupIDs {
		gridVerMap[gid] = 0
	}
	gvmJSON, _ := json.Marshal(gridVerMap)

	payload := map[string]any{
		"gridVerMap": string(gvmJSON),
	}
	jsonP, _ := json.Marshal(payload)
	enc, err := EncodeAESCBC(rawKey, string(jsonP))
	if err != nil { return nil, err }

	baseURL := "https://group-wpa.chat.zalo.me"
	if c.Session.ServiceMap != nil {
		if p, ok := c.Session.ServiceMap["group"]; ok && len(p) > 0 { baseURL = p[0] }
	}
	serviceURL := fmt.Sprintf("%s/api/group/getmg-v2?zpw_ver=%d&zpw_type=%d",
		baseURL, c.Session.APIVersion, c.Session.APIType)
	bodyStr := "params=" + url.QueryEscape(enc)
	req, _ := http.NewRequestWithContext(ctx, "POST", serviceURL, strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[zcloud] GetGroupInfo: raw=%s\n", string(body)[:min(300, len(body))])

	type resultType struct {
		Data *json.RawMessage `json:"data"`
	}
	var result resultType
	json.Unmarshal(body, &result)
	if result.Data == nil { return nil, nil }

	var dataStr string
	json.Unmarshal(*result.Data, &dataStr)

	decrypted, err := DecodeAESCBC(rawKey, dataStr)
	if err != nil { return nil, err }
	fmt.Printf("[zcloud] GetGroupInfo: decrypted=%s\n", string(decrypted)[:min(500, len(decrypted))])

	var wrap struct {
		Data *json.RawMessage `json:"data"`
	}
	json.Unmarshal(decrypted, &wrap)
	if wrap.Data == nil { return nil, nil }

	var groups struct {
		GridInfoMap map[string]struct {
			Name string `json:"name"`
			Avt  string `json:"avt"`
		} `json:"gridInfoMap"`
	}
	json.Unmarshal(*wrap.Data, &groups)

	resultMap := make(map[string]struct{ Name, Avatar string })
	for gid, info := range groups.GridInfoMap {
		if info.Name != "" {
			resultMap[gid] = struct{ Name, Avatar string }{Name: info.Name, Avatar: info.Avt}
		}
	}
	return resultMap, nil
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
