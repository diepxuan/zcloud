package core

import (
	"strings"
	"testing"
)

// TestServiceBaseURL tập trung vào fallback path khi ServiceMap rỗng/thiếu key.
// Các test còn lại cho case có ServiceMap đầy đủ đã được cover bởi integration
// test qua WS (cmd 501/521) khi chạy với Postgres thật.
func TestServiceBaseURL(t *testing.T) {
	cases := []struct {
		name     string
		session  *Session
		key      string
		fallback string
		want     string
	}{
		{
			name:     "ServiceMap nil → fallback",
			session:  &Session{},
			key:      "chat",
			fallback: "https://example.com/",
			want:     "https://example.com",
		},
		{
			name:     "ServiceMap empty → fallback",
			session:  &Session{ServiceMap: map[string][]string{}},
			key:      "chat",
			fallback: "https://example.com/",
			want:     "https://example.com",
		},
		{
			name: "ServiceMap có key khác → fallback",
			session: &Session{ServiceMap: map[string][]string{
				"profile": {"https://profile-wpa.chat.zalo.me"},
			}},
			key:      "chat",
			fallback: "https://tt-convers-wpa.chat.zalo.me/",
			want:     "https://tt-convers-wpa.chat.zalo.me",
		},
		{
			name: "ServiceMap có key nhưng list rỗng → fallback",
			session: &Session{ServiceMap: map[string][]string{
				"chat": {},
			}},
			key:      "chat",
			fallback: "https://tt-convers-wpa.chat.zalo.me/",
			want:     "https://tt-convers-wpa.chat.zalo.me",
		},
		{
			name: "ServiceMap có URL nhưng rỗng string → fallback",
			session: &Session{ServiceMap: map[string][]string{
				"chat": {""},
			}},
			key:      "chat",
			fallback: "https://tt-convers-wpa.chat.zalo.me/",
			want:     "https://tt-convers-wpa.chat.zalo.me",
		},
		{
			name: "ServiceMap có URL đầy đủ",
			session: &Session{ServiceMap: map[string][]string{
				"chat": {"https://chat-server.zalo.me"},
			}},
			key:      "chat",
			fallback: "https://fallback.chat.zalo.me/",
			want:     "https://chat-server.zalo.me",
		},
		{
			name: "ServiceMap trả về URL có trailing slash → trim",
			session: &Session{ServiceMap: map[string][]string{
				"chat": {"https://chat-server.zalo.me/"},
			}},
			key:      "chat",
			fallback: "https://fallback.chat.zalo.me/",
			want:     "https://chat-server.zalo.me",
		},
		{
			name: "ServiceMap có nhiều URL → lấy URL đầu",
			session: &Session{ServiceMap: map[string][]string{
				"chat": {"https://primary.chat.zalo.me", "https://secondary.chat.zalo.me"},
			}},
			key:      "chat",
			fallback: "https://fallback.chat.zalo.me/",
			want:     "https://primary.chat.zalo.me",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := serviceBaseURL(c.session, c.key, c.fallback)
			if got != c.want {
				t.Errorf("serviceBaseURL(%q) = %q, want %q", c.key, got, c.want)
			}
			// Không được có trailing slash.
			if strings.HasSuffix(got, "/") {
				t.Errorf("result should not have trailing slash: %q", got)
			}
		})
	}
}
