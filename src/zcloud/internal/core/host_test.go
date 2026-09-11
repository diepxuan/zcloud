package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestIsHostError: nhận diện DNS/connection errors.
func TestIsHostError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("something else"), false},
		{"no such host", errors.New("dial tcp: lookup xxx.example: no such host"), true},
		{"connection refused", errors.New("dial tcp 127.0.0.1:80: connect: connection refused"), true},
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"i/o timeout", errors.New("read tcp 1.2.3.4:443: i/o timeout"), true},
		{"network unreachable", errors.New("dial: network is unreachable"), true},
		{"tls handshake", errors.New("tls: handshake failure"), true},
		{"certificate", errors.New("x509: certificate signed by unknown authority"), true},
		{"syscall err", &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}, true},
		{"plain string", errors.New("connection refused by upstream proxy"), true}, // chuỗi chứa marker
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isHostError(c.err); got != c.want {
				t.Errorf("isHostError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestFallbackHost: trả host kế tiếp trong ServiceMap list.
func TestFallbackHost(t *testing.T) {
	cases := []struct {
		name    string
		session *Session
		key     string
		idx     int
		want    string
	}{
		{"nil session", nil, "chat", 0, ""},
		{"empty map", &Session{}, "chat", 0, ""},
		{"no key", &Session{ServiceMap: map[string][]string{
			"profile": {"https://p.zalo.me"},
		}}, "chat", 0, ""},
		{"idx 0", &Session{ServiceMap: map[string][]string{
			"chat": {"https://primary.zalo.me", "https://secondary.zalo.me"},
		}}, "chat", 0, "https://primary.zalo.me"},
		{"idx 1", &Session{ServiceMap: map[string][]string{
			"chat": {"https://primary.zalo.me", "https://secondary.zalo.me"},
		}}, "chat", 1, "https://secondary.zalo.me"},
		{"idx out of range", &Session{ServiceMap: map[string][]string{
			"chat": {"https://primary.zalo.me"},
		}}, "chat", 2, ""},
		{"trailing slash", &Session{ServiceMap: map[string][]string{
			"chat": {"https://primary.zalo.me/"},
		}}, "chat", 0, "https://primary.zalo.me"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fallbackHost(c.session, c.key, c.idx); got != c.want {
				t.Errorf("fallbackHost(%q, %d) = %q, want %q", c.key, c.idx, got, c.want)
			}
		})
	}
}

// TestMultiKeyChooser: thử host qua nhiều service keys.
func TestMultiKeyChooser(t *testing.T) {
	mc := &multiKeyChooser{
		session: &Session{ServiceMap: map[string][]string{
			"chat":    {"https://chat-primary.zalo.me", "https://chat-secondary.zalo.me"},
			"profile": {"https://profile.zalo.me"},
		}},
		keys: []string{"chat", "profile"},
	}
	expects := []string{
		"https://chat-primary.zalo.me",
		"https://chat-secondary.zalo.me",
		"https://profile.zalo.me", // hết chat → chuyển profile
	}
	for i, want := range expects {
		got, ok := mc.next()
		if !ok {
			t.Fatalf("step %d: unexpected end", i)
		}
		if got != want {
			t.Errorf("step %d: got %q, want %q", i, got, want)
		}
	}
	// Step 4: hết tất cả
	if _, ok := mc.next(); ok {
		t.Error("expected end of list after exhausting chat + profile")
	}
	if mc.lastErr == nil {
		mc.lastErr = fmt.Errorf("test error") // set manually để check
	}
	_ = mc.lastErr
}

// TestHostRetryChainFallback: HostRetry chain qua nhiều host trong ServiceMap
// khi host đầu fail.
func TestHostRetryChainFallback(t *testing.T) {
	// Mock 2 server: server1 fail (close immediately), server2 OK.
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force close để tạo connection refused.
		hj, _ := w.(http.Hijacker)
		if hj != nil {
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		http.Error(w, "boom", 500)
	}))
	defer server1.Close()

	server2Called := false
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server2Called = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server2.Close()

	sess := &Session{
		ServiceMap: map[string][]string{
			ServiceKeyChat: {server1.URL, server2.URL},
		},
	}
	c := &Client{Session: sess, client: http.DefaultClient}

	u, _ := url.Parse("/api/test?foo=bar")
	resp, err := c.HostRetry(context.Background(), http.MethodGet, u, nil, nil)
	if err != nil {
		t.Fatalf("HostRetry: %v", err)
	}
	defer resp.Body.Close()
	if !server2Called {
		t.Error("server2 (fallback) should be called after server1 fail")
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("body = %s, want contains ok:true", string(body))
	}
}

// TestHostRetryAllHostsFail: cả 2 host fail → trả error.
func TestHostRetryAllHostsFail(t *testing.T) {
	sess := &Session{
		ServiceMap: map[string][]string{
			ServiceKeyChat: {"http://127.0.0.1:1", "http://127.0.0.1:2"}, // port không ai listen
		},
	}
	c := &Client{Session: sess, client: &http.Client{Timeout: 1 * time.Second}}

	u, _ := url.Parse("/api/test")
	_, err := c.HostRetry(context.Background(), http.MethodGet, u, nil, nil)
	if err == nil {
		t.Error("expected error when all hosts fail")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("err = %v, want contains 'exhausted'", err)
	}
}

// TestHostRetryNonHostError: lỗi không phải host (vd 500) → fail ngay.
func TestHostRetryNonHostError(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()

	sess := &Session{
		ServiceMap: map[string][]string{
			ServiceKeyChat: {srv.URL},
		},
	}
	c := &Client{Session: sess, client: http.DefaultClient}

	u, _ := url.Parse("/api/test")
	resp, err := c.HostRetry(context.Background(), http.MethodGet, u, nil, nil)
	if err != nil {
		t.Fatalf("HostRetry: %v", err)
	}
	defer resp.Body.Close()
	if !called {
		t.Error("server should be called")
	}
	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}
