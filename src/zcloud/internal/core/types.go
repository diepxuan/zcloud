package core

import (
	"encoding/json"
	"time"
)

// ====================================
// Zalo Data Types
// ====================================

// MsgType represents the type of message
type MsgType int

const (
	MsgTypeText     MsgType = 1
	MsgTypeImage    MsgType = 2
	MsgTypeSticker  MsgType = 3
	MsgTypeFile     MsgType = 4
	MsgTypeVoice    MsgType = 5
	MsgTypeLink     MsgType = 6
	MsgTypeVideo    MsgType = 7
	MsgTypeCard     MsgType = 8
	MsgTypeLocation MsgType = 9
)

// IsMedia trả về true nếu message type có attachment media cần tải về disk.
//
// MsgTypeLink không tự trả true — caller phải check attachment có URL ảnh
// trước (xem HasImageAttachment trong ws.go). Logic tách riêng vì MsgType
// là enum không chứa context attachments.
func (t MsgType) IsMedia() bool {
	switch t {
	case MsgTypeImage, MsgTypeSticker, MsgTypeFile, MsgTypeVoice, MsgTypeVideo:
		return true
	}
	return false
}

// IsLink trả về true nếu message là link. Caller kiểm tra tiếp attachment
// có ảnh preview (OG image) để quyết định có tải về disk hay không.
func (t MsgType) IsLink() bool {
	return t == MsgTypeLink
}
// DesktopSyncEvent chứa payload parsed từ WS cmd 590-592 / 630-634.
// Data là JSON đã giải mã (raw map hoặc struct tuỳ schema Zalo trả về).
// Schema đầy đủ cần reverse thêm trusted-device WASM flow — hiện chỉ lưu
// raw payload để handler downstream xử lý / debug.
type DesktopSyncEvent struct {
	Cmd     uint16 `json:"cmd"`
	SubCmd  uint8  `json:"subCmd"`
	Type    EventType `json:"type"`
	RawData json.RawMessage `json:"rawData,omitempty"`
}

// EventType represents WebSocket event types
type EventType int

const (
	EventNewMessage EventType = iota + 1
	EventOldMessages
	EventDelivered
	EventSeen
	EventTyping
	EventReaction
	EventReconnect
	EventUploadAttachment
	EventError
	// Desktop sync events (cmd 590-592 / 630-634) — xem docs/protocol/pc-desktop.md.
	EventRequestSync      // cmd 590: server yêu cầu client gửi msg cross-device
	EventAckDeleteSession // cmd 591: xác nhận xoá session sync
	EventMobileWakeUp     // cmd 592: đánh thức mobile để sync
	EventInitBackup       // cmd 630: init session backup
	EventCreateBackup     // cmd 631: tạo session backup
	EventBackupMeta       // cmd 632: metadata backup từ PC
	EventRestoreMobile    // cmd 633: báo mobile khôi phục backup
	EventBackupConfigs    // cmd 634: lấy cấu hình backup
)

// CmdToEventType ánh WS cmd/subCmd sang EventType để dispatch đến handler.
// Trả EventError nếu cmd không thuộc nhóm desktop sync.
func CmdToEventType(cmd uint16, subCmd uint8) EventType {
	switch cmd {
	case 590:
		return EventRequestSync
	case 591:
		return EventAckDeleteSession
	case 592:
		return EventMobileWakeUp
	case 630:
		return EventInitBackup
	case 631:
		return EventCreateBackup
	case 632:
		return EventBackupMeta
	case 633:
		return EventRestoreMobile
	case 634:
		return EventBackupConfigs
	}
	return EventError
}

// String trả về tên event để log + broadcast (UI/browser nhận diện).
func (e EventType) String() string {
	switch e {
	case EventNewMessage:
		return "new_message"
	case EventOldMessages:
		return "old_messages"
	case EventDelivered:
		return "delivered"
	case EventSeen:
		return "seen"
	case EventTyping:
		return "typing"
	case EventReaction:
		return "reaction"
	case EventReconnect:
		return "reconnect"
	case EventUploadAttachment:
		return "upload_attachment"
	case EventError:
		return "error"
	case EventRequestSync:
		return "request_sync"
	case EventAckDeleteSession:
		return "ack_delete_session"
	case EventMobileWakeUp:
		return "mobile_wake_up"
	case EventInitBackup:
		return "init_backup"
	case EventCreateBackup:
		return "create_backup"
	case EventBackupMeta:
		return "backup_meta"
	case EventRestoreMobile:
		return "restore_mobile"
	case EventBackupConfigs:
		return "backup_configs"
	}
	return "unknown"
}

// ConvType represents conversation type
type ConvType int

const (
	ConvIndividual ConvType = 0
	ConvGroup      ConvType = 1
)

// ThreadType represents message thread type
type ThreadType int

const (
	ThreadUser  ThreadType = 0
	ThreadGroup ThreadType = 1
)

// Message represents a Zalo message
type Message struct {
	ID          string           `json:"id"`
	ConvID      string           `json:"convId"`
	FromID      string           `json:"fromId"`
	FromName    string           `json:"fromName,omitempty"`
	Content     string           `json:"content"`
	Timestamp   int64            `json:"timestamp"` // Unix ms
	Type        MsgType          `json:"type"`
	Attachments []Attachment     `json:"attachments,omitempty"`
	Mentions    []MessageMention `json:"mentions,omitempty"`
	Quote       *Message         `json:"quote,omitempty"`

	// IsDeliveryAck true nếu message là ack gửi/nhận (Zalo wrap action JSON
	// trong content thay vì text). UI dùng cờ này để render badge
	// "Đã gửi/Đã nhận/Đã xem" thay vì in nguyên JSON.
	IsDeliveryAck bool `json:"isAck,omitempty"`
	// AckStatus là nhãn tiếng Việt đã chuẩn hoá:
	//   "sent"      — Sếp đã gửi đến server (actionType=0)
	//   "delivered" — người nhận đã nhận (delivered event cmd 502)
	//   "seen"      — người nhận đã xem (seen event cmd 502/522)
	//   "unsent"    — người gửi thu hồi (Undo)
	//   "" (rỗng)   — chưa rõ trạng thái, UI mặc định "Đã gửi"
	AckStatus string `json:"ackStatus,omitempty"`
}

// IsDeliveryAck true nếu Message là ack gửi/nhận (Zalo wrap action JSON
// trong trường content thay vì text). UI dùng cờ này để render badge
// "Đã gửi/Đã nhận/Đã xem" thay vì in nguyên JSON.
// AckStatus là nhãn tiếng Việt đã chuẩn hoá cho UI:
//   "sent"      — Sếp đã gửi đến server (actionType=0, default)
//   "delivered" — người nhận đã nhận (delivered event cmd 502)
//   "seen"      — người nhận đã xem (seen event cmd 502/522)
//   "unsent"    — người gửi thu hồi (Undo)
// Khi rỗng → UI render "Đã gửi (chưa rõ trạng thái)".
// Attachment represents a file/image attachment
type Attachment struct {
	ID       string `json:"id"`
	URL      string `json:"url,omitempty"`
	FileName string `json:"fileName,omitempty"`
	FileSize int64  `json:"fileSize,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// MessageMention represents a mention in a message
type MessageMention struct {
	UID  string `json:"uid"`
	Name string `json:"name,omitempty"`
	Pos  int    `json:"pos"`
	Len  int    `json:"len"`
}

// Conversation represents a Zalo conversation
type Conversation struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Avatar    string   `json:"avatar,omitempty"`
	Type      ConvType `json:"type"`
	LastMsg   *Message `json:"lastMsg,omitempty"`
	Unread    int      `json:"unread"`
	UpdatedAt int64    `json:"updatedAt"`
}

// User represents a Zalo user
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar,omitempty"`
	Phone    string `json:"phone,omitempty"`
	IsFriend bool   `json:"isFriend,omitempty"`
}

// Session represents an authenticated Zalo session
type Session struct {
	Cookies    map[string]string   `json:"cookies"`
	SecretKey  string              `json:"secretKey"` // zpw_enk
	IMEI       string              `json:"imei"`
	UserID     string              `json:"userId"`
	UserAgent  string              `json:"userAgent"`
	Language   string              `json:"language,omitempty"`
	ExpiresAt  time.Time           `json:"expiresAt"`
	WSURLs     []string            `json:"wsUrls"`               // zpw_ws
	ServiceMap map[string][]string `json:"serviceMap,omitempty"` // zpw_service_map_v3
	Settings   string              `json:"settings,omitempty"`   // raw settings từ getServerInfo
	ExtraVer   string              `json:"extraVer,omitempty"`
	APIType    uint                `json:"apiType"`
	APIVersion uint                `json:"apiVersion"`
}

// Event represents a WebSocket event from Zalo
type Event struct {
	Type        EventType        `json:"type"`
	Message     *Message         `json:"message,omitempty"`
	FileID      string           `json:"fileId,omitempty"`
	Error       error            `json:"error,omitempty"`
	// DesktopSync chiếm khi Type là 1 trong EventRequestSync ... EventBackupConfigs.
	// Schema đầy đủ của Zalo PC bundle cần reverse thêm WASM flow.
	DesktopSync *DesktopSyncEvent `json:"desktopSync,omitempty"`
}

// OldMessages represents a batch of old messages loaded via WebSocket
type OldMessages struct {
	Messages   []Message
	ThreadType ThreadType
}

// ====================================
// SessionContext — interface cho encrypt.go
// ====================================

// SessionContext cung cấp thông tin session cần cho encryption
type SessionContext interface {
	GetIMEI() string
	GetLanguage() string
	GetAPIType() uint
	GetAPIVersion() uint
}

// Đảm bảo *Session implement SessionContext
func (s *Session) GetIMEI() string { return s.IMEI }
func (s *Session) GetLanguage() string {
	if s.Language == "" {
		return "vi"
	}
	return s.Language
}
func (s *Session) GetAPIType() uint {
	if s.APIType == 0 {
		return 30
	}
	return s.APIType
}
func (s *Session) GetAPIVersion() uint {
	if s.APIVersion == 0 {
		return 665
	}
	return s.APIVersion
}
