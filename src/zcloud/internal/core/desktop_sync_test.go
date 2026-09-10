package core

import (
	"context"
	"strings"
	"testing"
)

func TestDesktopParamsDefaults(t *testing.T) {
	c := &Client{}
	got := c.desktopParams()
	if !strings.Contains(got, "zpw_ver=665") || !strings.Contains(got, "zpw_type=30") {
		t.Fatalf("desktop params = %q", got)
	}
}

func TestEncodeDesktopPayload(t *testing.T) {
	// 16-byte key is valid AES-128-CBC.
	client := NewClient(&Session{
		SecretKey: "AAAAAAAAAAAAAAAAAAAAAA==",
		IMEI:      "test-imei",
		APIType:   30,
		APIVersion: 688,
	})
	got, err := client.encodeDesktopPayload(map[string]any{"pc_name": "Web", "ok": true})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got == "" || strings.Contains(got, "pc_name") {
		t.Fatalf("expected encrypted query param, got %q", got)
	}
}

func TestDesktopSyncRequestMethodsNoCrashOnNilSession(t *testing.T) {
	c := &Client{}
	_, err := c.GetCrossDB(context.Background(), "pc", "session")
	if err == nil {
		t.Fatal("expected error without session")
	}
}
