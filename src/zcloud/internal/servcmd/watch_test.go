package servcmd

import (
	"os"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestPickFreePort(t *testing.T) {
	p, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if p < 1024 {
		t.Errorf("port %d không hợp lệ (>= 1024)", p)
	}
	p2, err := pickFreePort()
	if err != nil || p2 < 1024 {
		t.Errorf("pickFreePort 2: port=%d err=%v", p2, err)
	}
}

func TestIsBuildableEvent(t *testing.T) {
	tests := []struct {
		name string
		op   fsnotify.Op
		path string
		want bool
	}{
		{"go write", fsnotify.Write, "main.go", true},
		{"html write", fsnotify.Write, "internal/api/web/chat.html", true},
		{"css write", fsnotify.Write, "style.css", true},
		{"js write", fsnotify.Write, "app.js", true},
		{"sh write", fsnotify.Write, "check.sh", true},
		{"go create", fsnotify.Create, "new.go", true},
		{"md write skip", fsnotify.Write, "README.md", false},
		{"sum write skip", fsnotify.Write, "go.sum", false},
		{"test.go skip", fsnotify.Write, "foo_test.go", false},
		{"db write skip", fsnotify.Write, "store.db", false},
		{"chmod only skip", fsnotify.Chmod, "main.go", false},
		{"rename only skip", fsnotify.Rename, "main.go", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isBuildableEvent(fsnotify.Event{Op: tc.op, Name: tc.path})
			if got != tc.want {
				t.Errorf("isBuildableEvent(%v, %q) = %v, want %v", tc.op, tc.path, got, tc.want)
			}
		})
	}
}

func TestPrefixWriterSplitLines(t *testing.T) {
	// prefixWriter.Split(s) trả về []string đã bỏ dòng rỗng (giống logic trong
	// prefixWriter.Write): split theo \n, bỏ "" ra khỏi kết quả.
	input := "line1\nline2\n\nline3\n"
	parts := strings.Split(input, "\n")
	lines := parts[:0]
	for _, p := range parts {
		if p != "" {
			lines = append(lines, p)
		}
	}
	if len(lines) != 3 || lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("split lines: %v", lines)
	}
}

func TestAtomicRenameBinary(t *testing.T) {
	dir := t.TempDir()
	old := dir + "/zcloudd"
	new := dir + "/zcloudd.new"
	if err := os.WriteFile(old, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(new, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(new, old); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("expected 'new', got %q", string(data))
	}
	if _, err := os.Stat(new); !os.IsNotExist(err) {
		t.Errorf(".new vẫn tồn tại sau rename")
	}
}
