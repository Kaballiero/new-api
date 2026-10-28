package discover_codex_models

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Configuration interface {
	DecodeCodexKey(string) (*domain.CodexOAuthKey, error)
	Proxy(*domain.Channel) string
}
type Session interface {
	Version(context.Context) (string, error)
	Models(context.Context, string, *domain.CodexOAuthKey, string) (int, []string, error)
}
type Remote interface{ Open(string) (Session, error) }
type Usecase struct {
	configuration Configuration
	remote        Remote
	refresh       func(context.Context, int) (*domain.CodexOAuthKey, error)
}

func NewUsecase(configuration Configuration, remote Remote, refresh func(context.Context, int) (*domain.CodexOAuthKey, error)) Usecase {
	return Usecase{configuration: configuration, remote: remote, refresh: refresh}
}
func (usecase Usecase) Execute(channel *domain.Channel) ([]string, error) {
	if channel == nil || channel.Type != constant.ChannelTypeCodex {
		return nil, domain.ErrNotCodexChannel
	}
	if channel.ChannelInfo.IsMultiKey {
		return nil, fmt.Errorf("codex channel does not support multi-key model discovery")
	}
	session, err := usecase.remote.Open(usecase.configuration.Proxy(channel))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	version, err := session.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Codex client version: %w", err)
	}
	base := channel.BaseURLValue()
	if base == "" {
		base = constant.GetChannelBaseURL(constant.ChannelTypeCodex)
	}
	return usecase.Fetch(ctx, channel, base, session, version)
}
func (usecase Usecase) Fetch(ctx context.Context, channel *domain.Channel, base string, session Session, version string) ([]string, error) {
	key, err := domain.ParseCodexOAuthKey(strings.TrimSpace(channel.Key), usecase.configuration.DecodeCodexKey)
	if err != nil {
		return nil, err
	}
	status, models, err := session.Models(ctx, base, key, version)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		if channel.Id <= 0 {
			return nil, fmt.Errorf("codex channel credential expired; save the channel before retrying model fetch")
		}
		refreshed, err := usecase.refresh(ctx, channel.Id)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh Codex channel credential: %w", err)
		}
		status, models, err = session.Models(ctx, base, &domain.CodexOAuthKey{AccessToken: refreshed.AccessToken, AccountID: refreshed.AccountID}, version)
		if err != nil {
			return nil, err
		}
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream status: %d", status)
	}
	return models, nil
}
