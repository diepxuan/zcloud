package core

import "time"

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

// EventType represents WebSocket event types
type EventType int

const (
	EventNewMessage    EventType = iota + 1
	EventOldMessages
	EventDelivered
	EventSeen
	EventTyping
	EventReaction
	EventReconnect
	EventError
)

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
}

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
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Avatar    string     `json:"avatar,omitempty"`
	Type      ConvType   `json:"type"`
	LastMsg   *Message   `json:"lastMsg,omitempty"`
	Unread    int        `json:"unread"`
	UpdatedAt int64      `json:"updatedAt"`
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
	Cookies     map[string]string `json:"cookies"`
	SecretKey   string            `json:"secretKey"`   // zpw_enk
	IMEI        string            `json:"imei"`
	UserID      string            `json:"userId"`
	UserAgent   string            `json:"userAgent"`
	Language    string            `json:"language,omitempty"`
	ExpiresAt   time.Time         `json:"expiresAt"`
	WSURLs      []string          `json:"wsUrls"`      // zpw_ws
	APIType     uint              `json:"apiType"`
	APIVersion  uint              `json:"apiVersion"`
}

// Event represents a WebSocket event from Zalo
type Event struct {
	Type    EventType `json:"type"`
	Message *Message  `json:"message,omitempty"`
	Error   error     `json:"error,omitempty"`
}

// OldMessages represents a batch of old messages loaded via WebSocket
type OldMessages struct {
	Messages    []Message
	ThreadType  ThreadType
}

// LoginResult kết quả đăng nhập
type LoginResult struct {
	Session *Session
	Cookies map[string]string
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
func (s *Session) GetIMEI() string       { return s.IMEI }
func (s *Session) GetLanguage() string   { if s.Language == "" { return "vi" }; return s.Language }
func (s *Session) GetAPIType() uint      { if s.APIType == 0 { return 30 }; return s.APIType }
func (s *Session) GetAPIVersion() uint   { if s.APIVersion == 0 { return 665 }; return s.APIVersion }
