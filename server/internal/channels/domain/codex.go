package domain

import (
	"context"
	"errors"
	"strings"
	"time"
)

type CodexOAuthKey struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`

	AccountID   string `json:"account_id,omitempty"`
	LastRefresh string `json:"last_refresh,omitempty"`
	Email       string `json:"email,omitempty"`
	Type        string `json:"type,omitempty"`
	Expired     string `json:"expired,omitempty"`
}

type CodexTokenResult struct {
	AccessToken, RefreshToken string
	ExpiresAt                 time.Time
}

type CodexUsageFetch func(context.Context, string, string, string) (int, []byte, error)

func ParseCodexOAuthKey(raw string, decode func(string) (*CodexOAuthKey, error)) (*CodexOAuthKey, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("codex channel: empty oauth key")
	}
	key, err := decode(raw)
	if err != nil {
		return nil, errors.New("codex channel: invalid oauth key json")
	}
	return key, nil
}
func ShouldRefreshCodexStatus(status int) bool { return status == 1 || status == 3 }
