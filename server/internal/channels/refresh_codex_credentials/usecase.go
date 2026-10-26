package refresh_codex_credentials

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
	"time"
)

type Store interface {
	DecodeCodexKey(string) (*domain.CodexOAuthKey, error)
	RefreshCandidates(int) ([]*domain.Channel, int, error)
}
type Usecase struct {
	store   Store
	now     func() time.Time
	refresh func(context.Context, int) (*domain.CodexOAuthKey, error)
	cache   func()
	debug   func() bool
	log     func(string, string)
}

func NewUsecase(store Store, now func() time.Time, refresh func(context.Context, int) (*domain.CodexOAuthKey, error), cache func(), debug func() bool, log func(string, string)) Usecase {
	return Usecase{store: store, now: now, refresh: refresh, cache: cache, debug: debug, log: log}
}
func (usecase Usecase) Execute() {
	ctx := context.Background()
	now := usecase.now()

	var refreshed int
	var scanned int

	offset := 0
	for {
		channels, next, err := usecase.store.RefreshCandidates(offset)

		if err != nil {
			usecase.log("error", fmt.Sprintf("codex credential auto-refresh: query channels failed: %v", err))
			return
		}
		if len(channels) == 0 {
			break
		}
		offset = next

		for _, ch := range channels {
			if ch == nil {
				continue
			}
			scanned++
			if ch.ChannelInfo.IsMultiKey {
				continue
			}

			rawKey := strings.TrimSpace(ch.Key)
			if rawKey == "" {
				continue
			}

			oauthKey, err := domain.ParseCodexOAuthKey(rawKey, usecase.store.DecodeCodexKey)
			if err != nil {
				continue
			}

			refreshToken := strings.TrimSpace(oauthKey.RefreshToken)
			if refreshToken == "" {
				continue
			}

			expiredAtRaw := strings.TrimSpace(oauthKey.Expired)
			expiredAt, err := time.Parse(time.RFC3339, expiredAtRaw)
			if err == nil && !expiredAt.IsZero() && expiredAt.Sub(now) > 24*time.Hour {
				continue
			}

			refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			newKey, err := usecase.refresh(refreshCtx, ch.Id)
			cancel()
			if err != nil {
				usecase.log("warn", fmt.Sprintf("codex credential auto-refresh: channel_id=%d name=%s refresh failed: %v", ch.Id, ch.Name, err))
				continue
			}

			refreshed++
			usecase.log("info", fmt.Sprintf("codex credential auto-refresh: channel_id=%d name=%s refreshed, expires_at=%s", ch.Id, ch.Name, newKey.Expired))
		}
	}

	if refreshed > 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					usecase.log("warn", fmt.Sprintf("codex credential auto-refresh: InitChannelCache panic: %v", r))
				}
			}()
			usecase.cache()
		}()
	}

	if usecase.debug() {
		usecase.log("debug", fmt.Sprintf("codex credential auto-refresh: scanned=%d refreshed=%d", scanned, refreshed))
	}
}
