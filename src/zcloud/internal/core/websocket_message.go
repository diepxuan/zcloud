// wsMessage parser tách riêng từ websocket.go (H7.4).
//
// Đây là parser cho payload tin nhắn WS cmd 501/521/510/511 — không chứa
// WS protocol (connect, send, read loop). Tách ra để websocket.go tập
// trung vào connection state machine, file này tập trung vào schema
// payload Zalo thay đổi theo từng version.

package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

type wsMessage struct {
	MsgID       string          `json:"msgId"`
	CliMsgID    string          `json:"cliMsgId"`
	Content     json.RawMessage `json:"content"`
	FromUID     string          `json:"uidFrom"`
	DName       string          `json:"dName"`
	ConvID      string          `json:"convId"`
	IDTo        string          `json:"idTo"`
	ToID        string          `json:"toid"`
	Grid        string          `json:"grid"`
	UID         string          `json:"userId"`
	UIN         string          `json:"uin"`
	TS          json.RawMessage `json:"ts"`
	Type        json.RawMessage `json:"msgType"`
	SubType     int             `json:"subType"`
	PropertyExt *struct {
		SubType int    `json:"subType"`
		Ext     string `json:"ext"`
	} `json:"propertyExt"`
	Mentions []wsMention     `json:"mentions"`
	Quote    json.RawMessage `json:"quote"`
}

type wsMention struct {
	UID  string `json:"uid"`
	Pos  int    `json:"pos"`
	Len  int    `json:"len"`
	Name string `json:"name,omitempty"`
}

func (m wsMessage) toMessage(session *Session, tt ThreadType) *Message {
	msgID := m.MsgID
	if msgID == "" {
		msgID = m.CliMsgID
	}
	fromID := m.FromUID
	if fromID == "" {
		fromID = m.UID
	}
	selfUID := ""
	if session != nil {
		selfUID = session.UserID
	}
	// Zalo dùng "0" làm sentinel cho chính account đang đăng nhập:
	// uidFrom="0" là tin mình gửi, idTo="0" là tin gửi đến mình.
	isSelf := fromID == "0" || (selfUID != "" && fromID == selfUID)
	if fromID == "0" && selfUID != "" {
		fromID = selfUID
	}
	// Thread của tin 1-1 luôn là phía đối phương (xem zca-js UserMessage):
	// mình gửi thì thread là người nhận, mình nhận thì thread là người gửi.
	// Với group, zca-js GroupMessage dùng idTo làm threadId; grid là ưu tiên
	// bổ sung khi payload có. Không bao giờ fallback group về uid người gửi.
	convID := skipZero(m.Grid)
	if convID == "" {
		if tt == ThreadGroup {
			convID = firstNonEmptyID(m.IDTo, m.ConvID, m.ToID)
		} else if isSelf {
			convID = firstNonEmptyID(m.IDTo, m.ToID, m.ConvID)
		} else {
			convID = firstNonEmptyID(fromID, m.IDTo, m.ToID, m.ConvID)
		}
	}
	// Chỉ self-thread 1-1 mới được fallback về uid của chính account.
	// Group thiếu conversation phải giữ rỗng để caller skip, tránh ghi bẩn.
	if convID == "" && tt != ThreadGroup {
		convID = selfUID
	}
	msg := &Message{
		ID: msgID, ConvID: convID, FromID: fromID, FromName: m.DName,
		Content:   m.contentText(),
		Timestamp: m.ts(),
		Type:      m.msgType(),
	}
	msg.Attachments = m.attachments()
	for _, mn := range m.Mentions {
		msg.Mentions = append(msg.Mentions, MessageMention{UID: mn.UID, Pos: mn.Pos, Len: mn.Len, Name: mn.Name})
	}
	MarkDeliveryAck(msg)
	return msg
}

func (m wsMessage) attachments() []Attachment {
	var out []Attachment
	obj, ok := m.contentObject()
	if !ok {
		return out
	}

	add := func(id string, url string, name string, fileSize int64, width, height int) {
		if id == "" && url == "" {
			return
		}
		if id == "" {
			id = fmt.Sprintf("%s-%d", m.MsgID, len(out))
		}
		out = append(out, Attachment{
			ID: id, URL: url, FileName: name, FileSize: fileSize, Width: width, Height: height,
		})
	}

	url := firstNonEmpty(obj["href"], obj["url"], obj["fileUrl"], obj["normalUrl"], obj["hdUrl"], obj["oriUrl"], obj["thumbUrl"], obj["thumb"])
	name := firstNonEmpty(obj["fileName"], obj["filename"], obj["name"], obj["title"])
	var size int64
	if f, ok := obj["fileSize"].(float64); ok {
		size = int64(f)
	}
	if s, ok := obj["fileSize"].(string); ok {
		fmt.Sscanf(s, "%d", &size)
	}
	var w, h int
	if f, ok := obj["width"].(float64); ok {
		w = int(f)
	}
	if f, ok := obj["height"].(float64); ok {
		h = int(f)
	}
	id := firstNonEmpty(obj["fileId"], obj["photoId"], obj["fileID"], obj["stickerId"], obj["id"])
	add(id, url, name, size, w, h)

	for _, key := range []string{"normalUrl", "hdUrl", "oriUrl", "thumbUrl", "thumb"} {
		if u, ok := obj[key].(string); ok && u != "" && u != url {
			add(id+"-"+key, u, name, size, w, h)
		}
	}
	return out
}

func (m wsMessage) contentObject() (map[string]any, bool) {
	if len(m.Content) == 0 {
		return nil, false
	}
	if m.Content[0] == '"' {
		var s string
		if json.Unmarshal(m.Content, &s) == nil && len(s) > 0 && s[0] == '{' {
			var obj map[string]any
			if json.Unmarshal([]byte(s), &obj) == nil {
				return obj, true
			}
		}
	}
	var obj map[string]any
	if json.Unmarshal(m.Content, &obj) == nil {
		return obj, true
	}
	return nil, false
}

func (m wsMessage) contentText() string {
	if len(m.Content) == 0 {
		return ""
	}
	if m.Content[0] == '"' {
		var s string
		if json.Unmarshal(m.Content, &s) == nil {
			return s
		}
	}
	var obj map[string]any
	if json.Unmarshal(m.Content, &obj) == nil {
		if s, ok := obj["text"].(string); ok {
			return s
		}
		if s, ok := obj["title"].(string); ok {
			return s
		}
	}
	return strings.TrimSpace(string(m.Content))
}

func (m wsMessage) ts() int64 {
	if len(m.TS) == 0 {
		return 0
	}
	if m.TS[0] == '"' {
		var s string
		if json.Unmarshal(m.TS, &s) == nil {
			var n int64
			fmt.Sscanf(s, "%d", &n)
			return n
		}
	}
	var n int64
	json.Unmarshal(m.TS, &n)
	return n
}

func (m wsMessage) msgType() MsgType {
	if len(m.Type) == 0 {
		return MsgTypeText
	}
	if m.Type[0] == '"' {
		var s string
		if json.Unmarshal(m.Type, &s) == nil {
			switch {
			case strings.Contains(s, "chat.photo"):
				return MsgTypeImage
			case strings.Contains(s, "chat.sticker"):
				return MsgTypeSticker
			case strings.Contains(s, "chat.file"):
				return MsgTypeFile
			case strings.Contains(s, "chat.voice"):
				return MsgTypeVoice
			case strings.Contains(s, "chat.link"):
				return MsgTypeLink
			case strings.Contains(s, "chat.video"):
				return MsgTypeVideo
			case strings.Contains(s, "chat.card"):
				return MsgTypeCard
			case strings.Contains(s, "chat.location"):
				return MsgTypeLocation
			default:
				return MsgTypeText
			}
		}
	}
	var n int
	json.Unmarshal(m.Type, &n)
	return MsgType(n)
}
