# Task 03: Design Core Protocol Layer

## Liên kết
- **Master plan:** [tasks.md](../tasks.md)
- **Task list:** [tasks.md](../tasks.md)
- **Phụ thuộc:** [01-reverse-web-api.md](01-reverse-web-api.md) ✅
- **Kế tiếp:** [04-build-core.md](04-build-core.md)
- **Tham khảo:** zcago `session/context.go`, zca-js `src/models/`

## Mục tiêu
Thiết kế Go interfaces cho core library dựa trên dữ liệu thực tế từ task 01 + source tham khảo.

## Design

### Core interfaces

```go
// ZaloClient — interface chính cho toàn bộ logic Zalo
type ZaloClient interface {
    // Auth
    Login(ctx context.Context) (*Session, error)
    LoginQR(ctx context.Context, qrCallback func(qrData []byte)) (*Session, error)
    Logout(ctx context.Context) error
    
    // Session
    Session() *Session
    LoadSession(path string) error
    SaveSession(path string) error
    
    // Conversations
    GetConversations(ctx context.Context) ([]Conversation, error)
    
    // Messages
    GetMessages(ctx context.Context, convID string, msgType ThreadType, cursor string) ([]Message, error)
    SendMessage(ctx context.Context, to string, msgType ThreadType, content string) (*Message, error)
    RequestOldMessages(ctx context.Context, threadType ThreadType, lastMsgID string) ([]Message, error)
    
    // Social
    GetFriends(ctx context.Context) ([]User, error)
    GetProfile(ctx context.Context) (*User, error)
    
    // Real-time
    Listen(ctx context.Context) (<-chan Event, error)
    Close() error
}
```

### Data types

```go
type Session struct {
    Cookies    map[string]string
    SecretKey  string          // zpw_enk
    IMEI       string
    UserID     string
    UserAgent  string
    ExpiresAt  time.Time
    WSURLs     []string        // zpw_ws
    ServiceMap map[string][]string
}

type Message struct {
    ID        string
    ConvID    string
    FromID    string
    Content   string
    Timestamp int64
    Type      MsgType
    Attachments []Attachment
}

type Conversation struct {
    ID        string
    Name      string
    Avatar    string
    Type      ConvType
    LastMsg   *Message
    Unread    int
    UpdatedAt int64
}

type Event struct {
    Type    EventType
    Message *Message
    Error   error
}

type User struct {
    ID        string
    Name      string
    Avatar    string
    Phone     string
}

type MsgType int    // text=1, image=2, file=3, sticker=4, ...
type EventType int  // new_message, old_messages, delivered, seen, ...
type ConvType int   // individual=0, group=1
type ThreadType int // user=0, group=1
```

### Encryption interface

```go
type Encryptor interface {
    // Zalo params encryption (dùng cho request)
    EncryptParams(ctx Context, params map[string]any, typeStr string) (*EncryptParamResult, error)
    // Response decryption
    DecryptResponse(ctx Context, encrypted string) ([]byte, error)
    // WebSocket event decryption
    DecryptEvent(cipherKey []byte, ct []byte, iv []byte, aad []byte) ([]byte, error)
}
```

### Store interface

```go
type SessionStore interface {
    Save(session *Session) error
    Load() (*Session, error)
    Delete() error
    Exists() bool
}
```

## Output
- `docs/design/interfaces.md` — Go interfaces (file này)
- `docs/design/protocol-layer.md` — architecture design
- `docs/design/encryption.md` — encryption implementation plan

## Verification
- [ ] Interfaces phủ hết API thực tế từ task 01
- [ ] Có đủ method cho login, chat, social, real-time
- [ ] Data types khớp với response thực tế

## Ghi chú
- Task 02 (Android sync) không ảnh hưởng — design đã tính threadType user/group
- Database interface sẽ thêm sau ở task 05
