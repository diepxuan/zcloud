package core

import "testing"

func TestMediaContentPrefix(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		wantExt string
		wantOK  bool
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "jpg", true},
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, "png", true},
		{"gif87a", []byte{'G', 'I', 'F', '8', '7', 'a'}, "gif", true},
		{"gif89a", []byte{'G', 'I', 'F', '8', '9', 'a'}, "gif", true},
		{"webp", append([]byte("RIFF"), 0, 0, 0, 0, 'W', 'E', 'B', 'P'), "webp", true},
		{"mp4-isom", append([]byte{0, 0, 0, 0}, []byte("ftypisom")...), "mp4", true},
		{"mp4-mp42", append([]byte{0, 0, 0, 0}, []byte("ftypmp42")...), "mp4", true},
		{"mov", append([]byte{0, 0, 0, 0}, []byte("ftypqt  ")...), "mov", true},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0, 0, 0, 0}, "webm", true},
		{"ogg", []byte{'O', 'g', 'g', 'S', 0, 0, 0, 0}, "ogg", true},
		{"mp3-id3", []byte{'I', 'D', '3', 4, 0, 0, 0, 0}, "mp3", true},
		{"mp3-sync", []byte{0xFF, 0xFB, 0x90, 0, 0, 0, 0, 0}, "mp3", true},
		{"m4a", append([]byte{0, 0, 0, 0}, []byte("ftypM4A ")...), "m4a", true},
		{"html", []byte("<!DOCTYPE html><html>"), "", false},
		{"plain", []byte("hello world"), "", false},
		{"empty", []byte{}, "", false},
		{"short", []byte{0xFF, 0xD8}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mime, ext := MediaContentPrefix(c.data)
			if c.wantOK {
				if mime == "" || ext != c.wantExt {
					t.Errorf("got mime=%q ext=%q, want ext=%q", mime, ext, c.wantExt)
				}
				if !IsMediaContent(c.data) {
					t.Errorf("IsMediaContent should be true")
				}
			} else {
				if mime != "" || ext != "" {
					t.Errorf("got mime=%q ext=%q, want both empty", mime, ext)
				}
				if IsMediaContent(c.data) {
					t.Errorf("IsMediaContent should be false")
				}
			}
		})
	}
}
