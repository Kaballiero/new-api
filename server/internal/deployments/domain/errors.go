package domain

import (
	"errors"
	"strings"
)

var ErrNotConfigured = errors.New("deployment provider is not configured")
var ErrNameEmpty = errors.New("deployment name is empty")
var ErrNameUnavailable = errors.New("deployment name is unavailable")

func (s Settings) Key() (string, error) {
	if !s.Enabled || strings.TrimSpace(s.APIKey) == "" {
		return "", ErrNotConfigured
	}
	return s.APIKey, nil
}
