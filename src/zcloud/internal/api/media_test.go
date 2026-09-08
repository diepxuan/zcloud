package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
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

func TestExtractAllMediaDedupURL(t *testing.T) {
	atts := []core.Attachment{
		{ID: "a", URL: "https://x/y.jpg", FileName: "y.jpg"},
		{ID: "b", URL: "https://x/y.jpg", FileName: "y.jpg"}, // dup URL
	}
	got := extractAllMedia(atts)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
}

func TestDownloadOneMedia_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
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

	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
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

	dir := t.TempDir()
	st, _ := store.NewSQLite(filepath.Join(dir, "test.db"), filepath.Join(dir, "media"))
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
