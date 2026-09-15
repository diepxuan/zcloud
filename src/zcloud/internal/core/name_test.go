package core

import (
	"testing"
)

// TestFriendItemNameResolution mô phỏng logic trong GetFriends: chọn
// displayName trước (có dấu, khớp Zalo app), fallback zaloName khi rỗng.
func TestFriendItemNameResolution(t *testing.T) {
	cases := []struct {
		name     string
		item     map[string]any
		want string
	}{
		{
			name: "displayName-co-dau-uu-tien",
			item: map[string]any{
				"userId":      "u1",
				"zaloName":    "Ngoc Duc",
				"displayName": "Ngọc Đức",
				"avatar":      "https://avt/zalo/u1.jpg",
			},
			want: "Ngọc Đức",
		},
		{
			name: "chi-co-zaloName",
			item: map[string]any{
				"userId":   "u2",
				"zaloName": "Ngoc Duc",
				"avatar":   "https://avt/zalo/u2.jpg",
			},
			want: "Ngoc Duc",
		},
		{
			name: "displayName-rong-fallback-zaloName",
			item: map[string]any{
				"userId":      "u3",
				"zaloName":    "Ngoc Duc",
				"displayName": "",
			},
			want: "Ngoc Duc",
		},
		{
			name: "ca-hai-rong",
			item: map[string]any{
				"userId": "u4",
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstNonEmpty(
				toString(tc.item["displayName"]),
				toString(tc.item["zaloName"]),
			)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProfileNameResolution mô phỏng logic trong resolveNames/GetMyProfile.
func TestProfileNameResolution(t *testing.T) {
	type prof struct {
		ZaloName    string `json:"zaloName"`
		DisplayName string `json:"displayName"`
	}
	pick := func(p prof) string {
		n := p.DisplayName
		if n == "" {
			n = p.ZaloName
		}
		return n
	}
	cases := []struct {
		name string
		p    prof
		want string
	}{
		{"displayName-uu-tien", prof{"Ngoc Duc", "Ngọc Đức"}, "Ngọc Đức"},
		{"fallback-displayName-rong", prof{"Ngoc Duc", ""}, "Ngoc Duc"},
		{"fallback-zaloName-rong", prof{"", "Ngọc Đức"}, "Ngọc Đức"},
		{"ca-hai-rong", prof{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pick(tc.p); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
