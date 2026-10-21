package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// Tách từ websocket.go (H7.1) — 8 function xử lý event WS Zalo.
// Bao gồm: handleNewMessages, handleOldMessages, parseMessages,
// handleMessageStatus, handleControl, handleActions, handleReactions,
// emitRawEvent. Các function này được gọi từ handleFrame trong websocket.go.

// handleNewMessages xử lý event WS cmd 501/521 (tin nhắn real-time mới).
func (w *WSClient) handleNewMessages(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var rawData struct {
		Msgs      json.RawMessage `json:"msgs"`
		GroupMsgs json.RawMessage `json:"groupMsgs"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &rawData); err != nil {
		slog.Error("ws_new_message_parse_failed", "error", err)
		return
	}

	msgsRaw := rawData.Msgs
	// Zalo cmd 501/521 wrap payload trong {error_code, data: {msgs|groupMsgs}}
	// (tham chiếu zca-js src/apis/listen.ts:259) — unwrap layer 2 khi cần.
	if len(msgsRaw) == 0 && len(rawData.Data) > 0 {
		var inner struct {
			Msgs      json.RawMessage `json:"msgs"`
			GroupMsgs json.RawMessage `json:"groupMsgs"`
		}
		if json.Unmarshal(rawData.Data, &inner) == nil {
			msgsRaw = inner.Msgs
			if tt == ThreadGroup && len(inner.GroupMsgs) > 0 {
				msgsRaw = inner.GroupMsgs
			}
		}
	}
	if tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	w.parseMessages(msgsRaw, EventNewMessage, tt)
}

// handleOldMessages xử lý cmd 510/511 (old messages response)
func (w *WSClient) handleOldMessages(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var rawData struct {
		Msgs      json.RawMessage `json:"msgs"`
		GroupMsgs json.RawMessage `json:"groupMsgs"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &rawData); err != nil {
		slog.Error("ws_old_message_parse_failed", "error", err)
		return
	}
	msgsRaw := rawData.Msgs
	if len(msgsRaw) == 0 && len(rawData.Data) > 0 {
		// Thử parse từ wrapper {data: {msgs: ...}}
		var inner struct {
			Msgs json.RawMessage `json:"msgs"`
		}
		if json.Unmarshal(rawData.Data, &inner) == nil {
			msgsRaw = inner.Msgs
		}
	}
	if len(msgsRaw) == 0 && tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	if tt == ThreadGroup && len(rawData.GroupMsgs) > 0 {
		msgsRaw = rawData.GroupMsgs
	}
	if len(msgsRaw) == 0 {
		return
	}
	w.parseMessages(msgsRaw, EventOldMessages, tt)
}

func (w *WSClient) parseMessages(msgsRaw json.RawMessage, evtType EventType, tt ThreadType) {
	if len(msgsRaw) == 0 {
		return
	}
	var msgs []wsMessage
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil || len(msgs) == 0 {
		return
	}
	for _, m := range msgs {
		if m.MsgID == "" && m.CliMsgID == "" {
			continue
		}
		msg := m.toMessage(w.session, tt)
		if msg.ConvID == "" {
			id := m.MsgID
			if id == "" {
				id = m.CliMsgID
			}
			slog.Warn("ws_message_missing_conversation",
				"message_id", id, "sender_id", m.FromUID, "thread_type", tt)
			continue
		}
		evt := Event{Type: evtType, Message: msg}
		select {
		case w.msgChan <- evt:
		default:
		}
	}
}

func (w *WSClient) handleMessageStatus(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var raw struct {
		Delivereds json.RawMessage `json:"delivereds"`
		Seens      json.RawMessage `json:"seens"`
		GroupSeens json.RawMessage `json:"groupSeens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	w.emitRawEvent(EventDelivered, raw.Delivereds)
	if tt == ThreadGroup && len(raw.GroupSeens) > 0 {
		w.emitRawEvent(EventSeen, raw.GroupSeens)
	} else {
		w.emitRawEvent(EventSeen, raw.Seens)
	}
}

func (w *WSClient) handleControl(payload []byte) {
	data := w.decryptPayload(payload)
	var raw struct {
		Controls []struct {
			Content struct {
				ActionType string          `json:"act_type"`
				Action     string          `json:"act"`
				Data       json.RawMessage `json:"data"`
				FileID     *int64          `json:"fileId,omitempty"`
			} `json:"content"`
		} `json:"controls"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for _, c := range raw.Controls {
		if c.Content.ActionType != "file_done" {
			continue
		}
		payloadData := c.Content.Data
		if len(payloadData) > 0 && payloadData[0] == '"' {
			var s string
			if json.Unmarshal(payloadData, &s) == nil {
				payloadData = []byte(s)
			}
		}
		var up struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(payloadData, &up) == nil && up.URL != "" {
			evt := Event{Type: EventUploadAttachment, Message: &Message{Content: up.URL}}
			if c.Content.FileID != nil {
				evt.FileID = fmt.Sprintf("%d", *c.Content.FileID)
			}
			select {
			case w.msgChan <- evt:
			default:
			}
		}
	}
}

func (w *WSClient) handleActions(payload []byte) {
	data := w.decryptPayload(payload)
	var raw struct {
		Actions []struct {
			ActionType string          `json:"act_type"`
			Action     string          `json:"act"`
			Data       json.RawMessage `json:"data"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for _, a := range raw.Actions {
		if a.ActionType != "typing" {
			continue
		}
		d := a.Data
		if len(d) > 0 && d[0] == '"' {
			var s string
			if json.Unmarshal(d, &s) == nil {
				d = []byte(s)
			}
		}
		evt := Event{Type: EventTyping}
		if a.Action == "gtyping" {
			var g struct {
				GID string `json:"gid"`
				UID string `json:"uid"`
			}
			json.Unmarshal(d, &g)
			evt.Message = &Message{ConvID: g.GID, FromID: g.UID}
		} else {
			var u struct {
				UID  string `json:"uid"`
				ToID string `json:"toid"`
			}
			json.Unmarshal(d, &u)
			evt.Message = &Message{ConvID: u.ToID, FromID: u.UID}
		}
		select {
		case w.msgChan <- evt:
		default:
		}
	}
}

func (w *WSClient) handleReactions(payload []byte, tt ThreadType) {
	data := w.decryptPayload(payload)
	var raw struct {
		Reacts      json.RawMessage `json:"reacts"`
		ReactGroups json.RawMessage `json:"reactGroups"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	if tt == ThreadGroup {
		w.emitRawEvent(EventReaction, raw.ReactGroups)
	} else {
		w.emitRawEvent(EventReaction, raw.Reacts)
	}
}

func (w *WSClient) emitRawEvent(typ EventType, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return
	}
	for range arr {
		select {
		case w.msgChan <- Event{Type: typ}:
		default:
		}
	}
}
