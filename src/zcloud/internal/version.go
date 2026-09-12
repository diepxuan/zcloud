package internal

import (
	"os"
	"strings"
)

// Semver là version hiện tại (semantic), đọc từ file VERSION ở project root
// khi build. Có thể override qua ldflags:
//   -ldflags "-X github.com/diepxuan/zcloud/internal.Semver=1.21.0"
// Build script trong servcmd/watch.go đọc file VERSION + inject vào binary.
var Semver string

// ShortCommit là git short hash của binary. Inject qua ldflags (xem watch.go).
var ShortCommit string

// Full trả về version hiện tại dạng "1.21.0 (abc1234)" — dùng cho /api/health.
func Full() string {
	semver := Semver
	if semver == "" {
		semver = readVersionFile()
	}
	if semver == "" {
		semver = "dev"
	}
	commit := ShortCommit
	if commit == "" {
		commit = "?"
	}
	return semver + " (" + commit + ")"
}

// readVersionFile đọc file VERSION ở project root.
func readVersionFile() string {
	// Binary ở /data/zcloud/zcloudd → VERSION ở /data/zcloud/VERSION.
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	for i := 0; i < 4; i++ {
		dir := parentDir(exe, i)
		if dir == "" {
			break
		}
		path := dir + "/VERSION"
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func parentDir(p string, n int) string {
	for i := 0; i < n+1; i++ {
		i := strings.LastIndex(p, "/")
		if i <= 0 {
			return ""
		}
		p = p[:i]
	}
	return p
}
