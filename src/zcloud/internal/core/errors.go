package core

import "fmt"

// ====================================
// Custom errors
// ====================================

var (
	ErrNotLoggedIn    = fmt.Errorf("zcloud: not logged in")
	ErrSessionExpired = fmt.Errorf("zcloud: session expired")
	ErrNetwork        = fmt.Errorf("zcloud: network error")
)

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("zcloud: api error %d: %s", e.Code, e.Message)
}

func WrapAPIError(code int, msg string) error {
	return &APIError{Code: code, Message: msg}
}
