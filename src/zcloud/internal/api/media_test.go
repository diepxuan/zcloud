//go:build testdb

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/diepxuan/zcloud/internal/core"
)

func TestExtractAllMedia(t *testing.T) {
	atts := []core.Attachment{
		{ID: "a1", URL: "https://x/y.jpg", FileName: "y.jpg"},
		{ID: "a1-hdUrl", URL: "https://x/y-hd.jpg", FileName: "y.jpg"}, // dup id
		{ID: "a2", URL: "https://x/file.pdf", FileName: "doc.pdf"},
		{ID: "a3", URL: "", FileName: "no-url.bin"}, // skip
	}
	got := extractAllMedia(atts)
	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}
	if got[0].FileExt != "jpg" || got[1].FileExt != "jpg" || got[2].FileExt != "pdf" {
		t.Fatalf("ext wrong: %+v", got)
	}
}

// TestExtractAllMediaDedupVariantSameBase: 2 ID cùng base ("a-0", "a-1")
// với URL khác nhau (variant -1 thumb ngắn hơn) → 1 entry, giữ variant URL dài hơn.
func TestExtractAllMediaDedupVariantSameBase(t *testing.T) {
	thumb := "https://photo-stal-12.zdn.vn/t.jpg"
	original := "https://photo-stal-12.zdn.vn/original-hd-2048x1536.jpg"
	atts := []core.Attachment{
		{ID: "a-1", URL: thumb, FileName: "y.jpg"},
		{ID: "a-0", URL: original, FileName: "y.jpg"},
	}
	got := extractAllMedia(atts)
	if len(got) != 1 {
		t.Fatalf("expected 1 (same base ID 'a'), got %d", len(got))
	}
	if got[0].URL != original {
		t.Errorf("expected longest URL, got %q", got[0].URL)
	}
}

// TestExtractAllMediaPreferLongestVariant: cùng base ID với variant -0/-1/-2,
// phải giữ variant URL dài nhất (thường là original HD, không phải thumb).
func TestExtractAllMediaPreferLongestVariant(t *testing.T) {
	// Original HD URL dài nhất, -1/-2 là thumb/preview ngắn hơn.
	thumbURL := "https://photo-stal-12.zdn.vn/t.jpg"
	previewURL := "https://photo-stal-12.zdn.vn/p.jpg"
	originalURL := "https://photo-stal-12.zdn.vn/original-hd-quality-2048x1536.jpg"
	atts := []core.Attachment{
		{ID: "8235996590219-0", URL: originalURL, FileName: "photo.jpg"},
		{ID: "8235996590219-1", URL: thumbURL, FileName: "photo.jpg"},
		{ID: "8235996590219-2", URL: previewURL, FileName: "photo.jpg"},
	}
	got := extractAllMedia(atts)
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped item, got %d: %+v", len(got), got)
	}
	if got[0].URL != originalURL {
		t.Errorf("expected longest URL (original), got %q", got[0].URL)
	}
	if got[0].MsgID != "8235996590219-0" {
		t.Errorf("expected MsgID -0 (original variant), got %q", got[0].MsgID)
	}
}

// TestExtractAllMediaDistinctBases: 2 base ID khác nhau → 2 entries.
// Thứ tự trong slice được giữ nguyên; variant dài hơn ghi đè variant ngắn.
func TestExtractAllMediaDistinctBases(t *testing.T) {
	thumb := "https://photo-stal-12.zdn.vn/t.jpg"
	original := "https://photo-stal-12.zdn.vn/original-hd.jpg"
	atts := []core.Attachment{
		{ID: "msg1-1", URL: thumb, FileName: "a.jpg"},
		{ID: "msg2-0", URL: "https://x/b.jpg", FileName: "b.jpg"},
		{ID: "msg1-0", URL: original, FileName: "a.jpg"},
	}
	got := extractAllMedia(atts)
	if len(got) != 2 {
		t.Fatalf("expected 2 deduped items, got %d", len(got))
	}
	// msg1 phải giữ variant URL dài hơn (original).
	if got[0].URL != original {
		t.Errorf("expected longest msg1 URL, got %q", got[0].URL)
	}
	if got[1].URL != "https://x/b.jpg" {
		t.Errorf("expected b URL, got %q", got[1].URL)
	}
}

// TestBaseAttachmentID: strip variant -N chỉ khi phần sau là số.
func TestBaseAttachmentID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"8235996590219-0", "8235996590219"},
		{"8235996590219-12", "8235996590219"},
		{"abc-def-2", "abc-def"},
		{"raw-id", "raw-id"},          // "id" không phải số
		{"a1-hdUrl", "a1-hdUrl"},     // "hdUrl" không phải số
		{"", ""},
		{"-5", "-5"},                 // prefix "-" nhưng phần trước rỗng → giữ nguyên
	}
	for _, c := range cases {
		if got := baseAttachmentID(c.in); got != c.want {
			t.Errorf("baseAttachmentID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDownloadOneMedia_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer srv.Close()

	st := newTestStore(t)
	defer st.Close()
	if err := st.CreateAccount("acc-1", "Test", 1); err != nil {
		t.Fatalf("account: %v", err)
	}

	msg := &core.Message{
		ID: "m1", ConvID: "c1", FromID: "u1", Type: core.MsgTypeImage,
		Attachments: []core.Attachment{
			{ID: "file-1", URL: srv.URL + "/img.jpg", FileName: "img.jpg"},
		},
	}
	downloadOneMedia(context.Background(), st, "acc-1", msg,
		mediaDownloadInfo{URL: srv.URL + "/img.jpg", FileExt: "jpg", MsgID: "file-1"},
		testLogger())

	path := st.MediaFilePath("acc-1", "c1", "file-1", "jpg")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not saved at %s: %v", path, err)
	}
}

func TestDownloadOneMedia_SkipExisting(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	st := newTestStore(t)
	defer st.Close()
	_ = st.CreateAccount("acc-1", "T", 1)

	// Tạo file sẵn.
	mediaDir := st.MediaDir("acc-1", "c1")
	_ = os.MkdirAll(mediaDir, 0755)
	existing := filepath.Join(mediaDir, "file-1.jpg")
	_ = os.WriteFile(existing, []byte("preexisting"), 0644)

	msg := &core.Message{
		ID: "m1", ConvID: "c1", Type: core.MsgTypeImage,
		Attachments: []core.Attachment{{ID: "file-1", URL: srv.URL + "/x.jpg", FileName: "x.jpg"}},
	}
	downloadOneMedia(context.Background(), st, "acc-1", msg,
		mediaDownloadInfo{URL: srv.URL + "/x.jpg", FileExt: "jpg", MsgID: "file-1"},
		testLogger())
	if hits != 0 {
		t.Fatalf("server should not be hit when file exists, got %d hits", hits)
	}
}

func TestDownloadOneMedia_RetryOnError(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			http.Error(w, "fail", 500)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	st := newTestStore(t)
	defer st.Close()
	_ = st.CreateAccount("acc-1", "T", 1)

	msg := &core.Message{
		ID: "m1", ConvID: "c1", Type: core.MsgTypeImage,
		Attachments: []core.Attachment{{ID: "f", URL: srv.URL, FileName: "f.bin"}},
	}
	downloadOneMedia(context.Background(), st, "acc-1", msg,
		mediaDownloadInfo{URL: srv.URL, FileExt: "bin", MsgID: "f"},
		testLogger())
	if hits < 3 {
		t.Fatalf("expected 3 attempts (2 fail + 1 ok), got %d", hits)
	}
}

func TestMsgTypeIsMedia(t *testing.T) {
	cases := []struct {
		typ  core.MsgType
	want bool
	}{
		{core.MsgTypeText, false},
		{core.MsgTypeImage, true},
		{core.MsgTypeSticker, true},
		{core.MsgTypeFile, true},
		{core.MsgTypeVoice, true},
		{core.MsgTypeVideo, true},
		{core.MsgTypeLink, false},
		{core.MsgTypeCard, false},
		{core.MsgTypeLocation, false},
	}
	for _, c := range cases {
		if got := c.typ.IsMedia(); got != c.want {
			t.Errorf("%d.IsMedia()=%v want %v", c.typ, got, c.want)
		}
	}
}
