package domain

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrObservationLimitExceeded   = errors.New("plugin protocol observation limit exceeded")
	ErrInvalidObservationIdentity = errors.New("invalid plugin protocol observation identity")
)

type ObservationLimits struct {
	Global    int
	PerPlugin int
	PerUser   int
	PerToken  int
}

var DefaultObservationLimits = ObservationLimits{
	Global:    128,
	PerPlugin: 32,
	PerUser:   4,
	PerToken:  2,
}

type ObservationLimitError struct {
	Scope string
	Limit int
}

func (e *ObservationLimitError) Error() string {
	return fmt.Sprintf("%s: %s capacity is %d", ErrObservationLimitExceeded, e.Scope, e.Limit)
}

func (e *ObservationLimitError) Unwrap() error {
	return ErrObservationLimitExceeded
}

type ObservationLimiter struct {
	mu sync.Mutex

	limits ObservationLimits
	global int
	plugin map[string]int
	user   map[int]int
	token  map[int]int
}

func NewObservationLimiter(limits ObservationLimits) *ObservationLimiter {
	return &ObservationLimiter{
		limits: limits,
		plugin: make(map[string]int),
		user:   make(map[int]int),
		token:  make(map[int]int),
	}
}

func (l *ObservationLimiter) Acquire(
	pluginKey string,
	userID int,
	tokenID int,
) (func(), error) {
	pluginKey = strings.TrimSpace(pluginKey)
	switch {
	case pluginKey == "":
		return nil, fmt.Errorf("%w: plugin key is required", ErrInvalidObservationIdentity)
	case userID <= 0:
		return nil, fmt.Errorf("%w: user id must be positive", ErrInvalidObservationIdentity)
	case tokenID <= 0:
		return nil, fmt.Errorf("%w: token id must be positive", ErrInvalidObservationIdentity)
	}

	l.mu.Lock()
	if l.global >= l.limits.Global {
		l.mu.Unlock()
		return nil, &ObservationLimitError{
			Scope: "global",
			Limit: l.limits.Global,
		}
	}
	l.global++

	if l.plugin[pluginKey] >= l.limits.PerPlugin {
		l.global--
		l.mu.Unlock()
		return nil, &ObservationLimitError{
			Scope: "plugin",
			Limit: l.limits.PerPlugin,
		}
	}
	l.plugin[pluginKey]++

	if l.user[userID] >= l.limits.PerUser {
		l.global--
		l.plugin[pluginKey]--
		if l.plugin[pluginKey] == 0 {
			delete(l.plugin, pluginKey)
		}
		l.mu.Unlock()
		return nil, &ObservationLimitError{
			Scope: "user",
			Limit: l.limits.PerUser,
		}
	}
	l.user[userID]++

	if l.token[tokenID] >= l.limits.PerToken {
		l.global--
		l.plugin[pluginKey]--
		if l.plugin[pluginKey] == 0 {
			delete(l.plugin, pluginKey)
		}
		l.user[userID]--
		if l.user[userID] == 0 {
			delete(l.user, userID)
		}
		l.mu.Unlock()
		return nil, &ObservationLimitError{
			Scope: "token",
			Limit: l.limits.PerToken,
		}
	}
	l.token[tokenID]++
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()

			l.global--

			l.plugin[pluginKey]--
			if l.plugin[pluginKey] == 0 {
				delete(l.plugin, pluginKey)
			}

			l.user[userID]--
			if l.user[userID] == 0 {
				delete(l.user, userID)
			}

			l.token[tokenID]--
			if l.token[tokenID] == 0 {
				delete(l.token, tokenID)
			}
		})
	}, nil
}
