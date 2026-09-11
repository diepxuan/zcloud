package core

import (
	"fmt"
	"testing"
)

// TestCmdToEventType: ánh WS cmd → EventType cho 8 desktop sync command.
func TestCmdToEventType(t *testing.T) {
	cases := []struct {
		cmd  uint16
		want EventType
	}{
		{590, EventRequestSync},
		{591, EventAckDeleteSession},
		{592, EventMobileWakeUp},
		{630, EventInitBackup},
		{631, EventCreateBackup},
		{632, EventBackupMeta},
		{633, EventRestoreMobile},
		{634, EventBackupConfigs},
		{501, EventError},
		{1, EventError},
		{999, EventError},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("cmd=%d", c.cmd), func(t *testing.T) {
			if got := CmdToEventType(c.cmd, 0); got != c.want {
				t.Errorf("CmdToEventType(%d) = %v, want %v", c.cmd, got, c.want)
			}
		})
	}
}

// TestEventTypeString: 17 EventType đều có tên không rỗng và không trùng.
func TestEventTypeString(t *testing.T) {
	all := []EventType{
		EventNewMessage, EventOldMessages, EventDelivered, EventSeen,
		EventTyping, EventReaction, EventReconnect, EventUploadAttachment,
		EventError, EventRequestSync, EventAckDeleteSession, EventMobileWakeUp,
		EventInitBackup, EventCreateBackup, EventBackupMeta,
		EventRestoreMobile, EventBackupConfigs,
	}
	seen := make(map[string]bool)
	for _, e := range all {
		name := e.String()
		if name == "" || name == "unknown" {
			t.Errorf("EventType(%d).String() = %q (unexpected)", int(e), name)
		}
		if seen[name] {
			t.Errorf("duplicate name %q for EventType %d", name, int(e))
		}
		seen[name] = true
	}
}

// TestMsgTypeIsLink: link type có IsLink() trả true.
func TestMsgTypeIsLink(t *testing.T) {
	if !MsgTypeLink.IsLink() {
		t.Error("MsgTypeLink.IsLink() should be true")
	}
	if MsgTypeImage.IsLink() {
		t.Error("MsgTypeImage.IsLink() should be false")
	}
	if MsgTypeText.IsLink() {
		t.Error("MsgTypeText.IsLink() should be false")
	}
}
