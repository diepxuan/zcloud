package core

import "testing"

// TestToMessageThreadID khoá quy ước thread của tin 1-1: thread luôn là phía
// đối phương, không bao giờ là uid của chính account đang đăng nhập.
// Zalo trả "0" như sentinel cho chính mình (uidFrom="0" = tin mình gửi,
// idTo="0" = tin gửi đến mình).
func TestToMessageThreadID(t *testing.T) {
	const me = "559609701372941728"
	const peer = "6036488311923736737"
	session := &Session{UserID: me}

	cases := []struct {
		name       string
		msg        wsMessage
		tt         ThreadType
		wantConv   string
		wantFromID string
	}{
		{
			name:       "tin den, idTo la uid minh",
			msg:        wsMessage{MsgID: "1", FromUID: peer, IDTo: me},
			tt:         ThreadUser,
			wantConv:   peer,
			wantFromID: peer,
		},
		{
			name:       "tin den, idTo sentinel 0",
			msg:        wsMessage{MsgID: "2", FromUID: peer, IDTo: "0"},
			tt:         ThreadUser,
			wantConv:   peer,
			wantFromID: peer,
		},
		{
			name:       "tin minh gui tu thiet bi khac",
			msg:        wsMessage{MsgID: "3", FromUID: me, IDTo: peer},
			tt:         ThreadUser,
			wantConv:   peer,
			wantFromID: me,
		},
		{
			name:       "tin minh gui, uidFrom sentinel 0",
			msg:        wsMessage{MsgID: "4", FromUID: "0", IDTo: peer},
			tt:         ThreadUser,
			wantConv:   peer,
			wantFromID: me,
		},
		{
			name:       "tin nhom uu tien grid",
			msg:        wsMessage{MsgID: "5", FromUID: peer, Grid: "grp-1", IDTo: me},
			tt:         ThreadGroup,
			wantConv:   "grp-1",
			wantFromID: peer,
		},
		{
			name:       "tin nhom thieu grid dung idTo",
			msg:        wsMessage{MsgID: "5b", FromUID: peer, IDTo: "grp-2"},
			tt:         ThreadGroup,
			wantConv:   "grp-2",
			wantFromID: peer,
		},
		{
			name:       "tin nhom khong ro conversation",
			msg:        wsMessage{MsgID: "5c", FromUID: peer},
			tt:         ThreadGroup,
			wantConv:   "",
			wantFromID: peer,
		},
		{
			name:       "tu chat voi chinh minh",
			msg:        wsMessage{MsgID: "6", FromUID: "0", IDTo: "0"},
			tt:         ThreadUser,
			wantConv:   me,
			wantFromID: me,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.msg.toMessage(session, tc.tt)
			if got.ConvID != tc.wantConv {
				t.Errorf("ConvID=%q want %q", got.ConvID, tc.wantConv)
			}
			if got.FromID != tc.wantFromID {
				t.Errorf("FromID=%q want %q", got.FromID, tc.wantFromID)
			}
			if got.ConvID == me && tc.wantConv != me {
				t.Errorf("thread roi vao uid cua chinh minh")
			}
		})
	}
}
