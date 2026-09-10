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
		wantConv   string
		wantFromID string
	}{
		{
			name:       "tin den, idTo la uid minh",
			msg:        wsMessage{MsgID: "1", FromUID: peer, IDTo: me},
			wantConv:   peer,
			wantFromID: peer,
		},
		{
			name:       "tin den, idTo sentinel 0",
			msg:        wsMessage{MsgID: "2", FromUID: peer, IDTo: "0"},
			wantConv:   peer,
			wantFromID: peer,
		},
		{
			name:       "tin minh gui tu thiet bi khac",
			msg:        wsMessage{MsgID: "3", FromUID: me, IDTo: peer},
			wantConv:   peer,
			wantFromID: me,
		},
		{
			name:       "tin minh gui, uidFrom sentinel 0",
			msg:        wsMessage{MsgID: "4", FromUID: "0", IDTo: peer},
			wantConv:   peer,
			wantFromID: me,
		},
		{
			name:       "tin nhom uu tien grid",
			msg:        wsMessage{MsgID: "5", FromUID: peer, Grid: "grp-1", IDTo: me},
			wantConv:   "grp-1",
			wantFromID: peer,
		},
		{
			name:       "tu chat voi chinh minh",
			msg:        wsMessage{MsgID: "6", FromUID: "0", IDTo: "0"},
			wantConv:   me,
			wantFromID: me,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.msg.toMessage(session)
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
