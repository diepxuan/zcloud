package store

import (
	"database/sql"
	"time"
)

// ====================================
// Domain types — dùng chung giữa core, api, store
// ====================================

type Account struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	UserID      string    `json:"userId"`
	Avatar      string    `json:"avatar"`
	AccountType int       `json:"accountType"` // 1: Zalo User, 2: Zalo OA
	Status      int       `json:"status"`
	Note        string    `json:"note"`
	Enabled     bool      `json:"enabled"` // false = ẩn khỏi UI nhưng vẫn listen WS
	Transport   string    `json:"transport,omitempty"` // ""=web | "pc"
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Session struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"accountId"`
	UserID     string    `json:"userId"`
	Cookies    string    `json:"cookies"`
	SecretKey  string    `json:"secretKey"`
	IMEI       string    `json:"imei"`
	UserAgent  string    `json:"userAgent"`
	Language   string    `json:"language"`
	WSURLs     string    `json:"wsUrls"`
	ServiceMap string    `json:"serviceMap"`
	APIType    uint      `json:"apiType"`
	APIVersion uint      `json:"apiVersion"`
	IsActive   int       `json:"isActive"`
	Transport  string    `json:"transport,omitempty"` // ""=web | "pc" = Zalo PC
	CipherKey  string    `json:"cipherKey,omitempty"` // base64 AES-GCM (PC only)
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type Conversation struct {
	ID        string       `json:"id"`
	AccountID string       `json:"accountId"`
	Name      string       `json:"name"`
	Avatar    string       `json:"avatar"`
	ConvType  int          `json:"convType"` // 0: cá nhân, 1: nhóm, 2: OA
	LastMsgID string       `json:"lastMsgId"`
	LastMsgAt sql.NullTime `json:"lastMsgAt"`
	// LastMsgContent preview text của tin cuối (vd "Khi mô có gối cao su…" hoặc
	// "📷 Ảnh" nếu là image). Lấy qua LEFT JOIN messages trong GetConversations;
	// rỗng nếu messages bị xoá hoặc content > MaxLastMsgPreview.
	LastMsgContent string    `json:"lastMsgContent,omitempty"`
	Unread         int       `json:"unread"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Message struct {
	ID          string `json:"id"`
	AccountID   string `json:"accountId"`
	ConvID      string `json:"convId"`
	FromID      string `json:"fromId"`
	FromName    string `json:"fromName"`
	Content     string `json:"content"`
	MsgType     int    `json:"msgType"`
	Timestamp   int64  `json:"timestamp"`
	Attachments string `json:"attachments"`
}

type MediaFile struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"accountId"`
	ConvID       string    `json:"convId"`
	MsgID        string    `json:"msgId"`
	FileName     string    `json:"fileName"`
	FilePath     string    `json:"filePath"`
	FileExt      string    `json:"fileExt"`
	MimeType     string    `json:"mimeType"`
	FileSize     int64     `json:"fileSize"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	ThumbPath    string    `json:"thumbPath"`
	WidthThumb   int       `json:"widthThumb"`
	HeightThumb  int       `json:"heightThumb"`
	OCRText      string    `json:"ocrText"`
	AITags       string    `json:"aiTags"`
	AIProcessed  int       `json:"aiProcessed"`
	AIConfidence float64   `json:"aiConfidence"`
	IsDownloaded int       `json:"isDownloaded"`
	SourceURL    string    `json:"sourceUrl"`
	CreatedAt    time.Time `json:"createdAt"`
}

type OAConfig struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"accountId"`
	OAID         string    `json:"oaId"`
	OAName       string    `json:"oaName"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	SecretKey    string    `json:"secretKey"`
	WebhookURL   string    `json:"webhookUrl"`
	IsVerified   int       `json:"isVerified"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type OAWebhookLog struct {
	ID        int       `json:"id"`
	OAID      string    `json:"oaId"`
	EventID   string    `json:"eventId"`
	EventType string    `json:"eventType"`
	SenderID  string    `json:"senderId"`
	RawData   string    `json:"rawData"`
	Processed int       `json:"processed"`
	ErrorMsg  string    `json:"errorMsg"`
	CreatedAt time.Time `json:"createdAt"`
}

type MediaJob struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"accountId"`
	ConvID      string    `json:"convId"`
	MsgID       string    `json:"msgId"`
	FileName    string    `json:"fileName"`
	FileExt     string    `json:"fileExt"`
	SourceURL   string    `json:"sourceUrl"`
	LocalPath   string    `json:"localPath"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	MaxAttempts int       `json:"maxAttempts"`
	LastError   string    `json:"lastError"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
