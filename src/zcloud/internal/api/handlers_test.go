//go:build testdb

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleCookieLogin_ValidateBody kiểm tra các trường hợp validate
// body mà không cần Postgres (cookie fake → fail ở core.CookieLogin,
// nhưng validate body phải chạy trước).
func TestHandleCookieLogin_ValidateBody(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
		wantSub  string // substring của error message
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: 400,
			wantSub:  "thiếu zpsid",
		},
		{
			name:     "missing zpw_sek",
			body:     `{"zpsid":"abc"}`,
			wantCode: 400,
			wantSub:  "thiếu zpsid",
		},
		{
			name:     "empty strings",
			body:     `{"zpsid":"","zpw_sek":""}`,
			wantCode: 400,
			wantSub:  "thiếu zpsid",
		},
		{
			name:     "invalid JSON",
			body:     `not-json`,
			wantCode: 400,
			wantSub:  "invalid body",
		},
		{
			name:     "old {cookie} field no longer supported",
			body:     `{"cookie":"zpsid=fake; zpw_sek=fake"}`,
			wantCode: 400,
			wantSub:  "thiếu zpsid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Logger: newDiscardLogger()}
			req := httptest.NewRequest(http.MethodPost, "/api/login/cookie",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.HandleCookieLogin(rr, req)

			if rr.Code != tc.wantCode {
				t.Fatalf("code: want %d, got %d (body=%s)", tc.wantCode, rr.Code, rr.Body.String())
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("parse response: %v", err)
			}
			if errMsg, ok := resp["error"].(string); !ok || !strings.Contains(errMsg, tc.wantSub) {
				t.Errorf("error: want substring %q, got %q", tc.wantSub, errMsg)
			}
		})
	}
}

// TestHandleAccountSetEnabled kiểm tra validate body mà không cần DB
// (accountId giả → SetAccountEnabled fail ở DB, nhưng validate body chạy trước).
func TestHandleAccountSetEnabled_ValidateBody(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "invalid JSON",
			body:     `not-json`,
			wantCode: 400,
		},
		{
			name:     "missing accountId",
			body:     `{"enabled":true}`,
			wantCode: 400,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Logger: newDiscardLogger()}
			req := httptest.NewRequest(http.MethodPost, "/api/account/enabled",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.HandleAccountSetEnabled(rr, req)
			if rr.Code != tc.wantCode {
				t.Fatalf("code: want %d, got %d (body=%s)", tc.wantCode, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestHandleMessages_NoAccountID kiểm tra validate body khi không có convId.
func TestHandleMessages_NoAccountID(t *testing.T) {
	s := &Server{Logger: newDiscardLogger()}
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rr := httptest.NewRecorder()
	s.HandleMessages(rr, req)
	if rr.Code != 400 {
		t.Fatalf("want 400, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestHandleSendMessage_NoAccountID kiểm tra validate khi thiếu to/content.
func TestHandleSendMessage_NoAccountID(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing to", `{"content":"x"}`, 400},
		{"missing content", `{"to":"c1"}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Logger: newDiscardLogger()}
			req := httptest.NewRequest(http.MethodPost, "/api/messages/send",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			s.HandleSendMessage(rr, req)
			if rr.Code != tc.wantCode {
				t.Fatalf("want %d, got %d (body=%s)", tc.wantCode, rr.Code, rr.Body.String())
			}
		})
	}
}
