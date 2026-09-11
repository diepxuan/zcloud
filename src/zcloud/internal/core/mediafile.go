package core

import "bytes"

// MediaContentPrefix sniff 8 byte đầu của media để phân biệt file thật
// (JPEG/PNG/WebP/GIF/MP4/WebM/...) với HTML / text error page mà Zalo server
// trả khi URL die (photo không tồn tại, token hết hạn, …).
//
// Trả về:
//   - mime: chuẩn đoán MIME type (image/jpeg, video/mp4, …). Rỗng nếu không nhận ra.
//   - ext:  extension chuẩn tương ứng (jpg, png, mp4, …). Rỗng nếu không nhận ra.
//
// Lưu ý: chỉ sniff trên 8 byte đầu — đủ cho hầu hết format phổ biến. Đây không
// phải validate chuyên sâu; chỉ cần phân biệt "có vẻ là binary media" với
// "có vẻ là HTML/text".
func MediaContentPrefix(data []byte) (mime, ext string) {
	if len(data) < 4 {
		return "", ""
	}
	// JPEG: FF D8 FF
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return "image/jpeg", "jpg"
	}
	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png", "png"
	}
	// GIF: 47 49 46 38 (37|39)
	if len(data) >= 6 && bytes.HasPrefix(data, []byte{'G', 'I', 'F', '8'}) &&
		(data[4] == '7' || data[4] == '9') && data[5] == 'a' {
		return "image/gif", "gif"
	}
	// WebP: RIFF .... WEBP
	if bytes.HasPrefix(data, []byte{'R', 'I', 'F', 'F'}) &&
		len(data) >= 12 && bytes.Equal(data[8:12], []byte{'W', 'E', 'B', 'P'}) {
		return "image/webp", "webp"
	}
	// MP4/MOV/M4A: .... ftyp (offset 4)
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte{'f', 't', 'y', 'p'}) {
		ftyp := data[8:12]
		// M4A cần check trước vì ftyp=M4A vẫn thuộc MP4 container family.
		if bytes.Equal(ftyp, []byte{'M', '4', 'A', ' '}) {
			return "audio/mp4", "m4a"
		}
		switch {
		case bytes.Equal(ftyp, []byte{'m', 'p', '4', '2'}),
			bytes.Equal(ftyp, []byte{'i', 's', 'o', 'm'}),
			bytes.Equal(ftyp, []byte{'m', 'p', '4', '1'}):
			return "video/mp4", "mp4"
		case bytes.Equal(ftyp, []byte{'q', 't', ' ', ' '}):
			return "video/quicktime", "mov"
		}
		// fallback: vẫn là MP4 container
		return "video/mp4", "mp4"
	}
	// WebM/MKV: 1A 45 DF A3
	if bytes.HasPrefix(data, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		return "video/webm", "webm"
	}
	// OGG: 4F 67 67 53
	if bytes.HasPrefix(data, []byte{'O', 'g', 'g', 'S'}) {
		return "audio/ogg", "ogg"
	}
	// MP3: ID3 hoặc FF FB/FF FA/...
	if bytes.HasPrefix(data, []byte{'I', 'D', '3'}) {
		return "audio/mpeg", "mp3"
	}
	if len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		// FF FB/FF FA (MP3 with sync)
		if data[1] == 0xFB || data[1] == 0xFA || data[1] == 0xF3 || data[1] == 0xF2 {
			return "audio/mpeg", "mp3"
		}
	}
	return "", ""
}

// IsMediaContent trả về true nếu data có vẻ là file media binary
// (không phải HTML/text). Dùng sau khi download để phát hiện URL die trả
// về error page.
func IsMediaContent(data []byte) bool {
	mime, _ := MediaContentPrefix(data)
	return mime != ""
}
