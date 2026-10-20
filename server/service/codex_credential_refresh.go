package service

import (
	"context"

	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/refresh_codex_credential"

	"time"

	"github.com/QuantumNous/new-api/model"
)

type CodexCredentialRefreshOptions struct {
	ResetCaches bool
}

type CodexOAuthKey = channeldomain.CodexOAuthKey

func parseCodexOAuthKey(raw string) (*CodexOAuthKey, error) {
	return channeldomain.ParseCodexOAuthKey(raw)
}

func RefreshCodexChannelCredential(ctx context.Context, channelID int, opts CodexCredentialRefreshOptions) (*CodexOAuthKey, *model.Channel, error) {
	scenario := refresh_codex_credential.NewUsecase(channelstore.NewStore(), func(ctx context.Context, token, proxy string) (*channeldomain.CodexTokenResult, error) {
		result, err := RefreshCodexOAuthTokenWithProxy(ctx, token, proxy)
		if result == nil {
			return nil, err
		}
		return &channeldomain.CodexTokenResult{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: result.ExpiresAt}, err
	}, ExtractCodexAccountIDFromJWT, ExtractEmailFromJWT, time.Now)
	key, channel, err := scenario.Execute(ctx, refresh_codex_credential.Input{ChannelID: channelID, ResetCaches: opts.ResetCaches})
	return key, channelstore.FromDomain(channel), err
}
