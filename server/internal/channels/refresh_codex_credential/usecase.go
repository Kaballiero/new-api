package refresh_codex_credential

import (
	"context"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
	"time"
)

type Store interface {
	domain.CodexKeyCodec
	Get(int, bool) (*domain.Channel, error)
	Proxy(*domain.Channel) string
	SetKey(int, string) error
	InitCache()
}
type Usecase struct {
	store            Store
	refresh          func(context.Context, string, string) (*domain.CodexTokenResult, error)
	accountID, email func(string) (string, bool)
	now              func() time.Time
}

func NewUsecase(store Store, refresh func(context.Context, string, string) (*domain.CodexTokenResult, error), accountID, email func(string) (string, bool), now func() time.Time) Usecase {
	return Usecase{store: store, refresh: refresh, accountID: accountID, email: email, now: now}
}
func (usecase Usecase) Execute(ctx context.Context, input Input) (*domain.CodexOAuthKey, *domain.Channel, error) {
	ch, err := usecase.store.Get(input.ChannelID, true)
	if err != nil {
		return nil, nil, err
	}
	if ch == nil {
		return nil, nil, domain.ErrCodexChannelNotFound
	}
	if ch.Type != constant.ChannelTypeCodex {
		return nil, nil, domain.ErrNotCodexChannel
	}

	oauthKey, err := domain.ParseCodexOAuthKey(strings.TrimSpace(ch.Key), usecase.store.DecodeCodexKey)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(oauthKey.RefreshToken) == "" {
		return nil, nil, domain.ErrCodexRefreshTokenRequired
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := usecase.refresh(refreshCtx, oauthKey.RefreshToken, usecase.store.Proxy(ch))
	if err != nil {
		return nil, nil, err
	}

	oauthKey.AccessToken = res.AccessToken
	oauthKey.RefreshToken = res.RefreshToken
	oauthKey.LastRefresh = usecase.now().Format(time.RFC3339)
	oauthKey.Expired = res.ExpiresAt.Format(time.RFC3339)
	if strings.TrimSpace(oauthKey.Type) == "" {
		oauthKey.Type = "codex"
	}

	if strings.TrimSpace(oauthKey.AccountID) == "" {
		if accountID, ok := usecase.accountID(oauthKey.AccessToken); ok {
			oauthKey.AccountID = accountID
		}
	}
	if strings.TrimSpace(oauthKey.Email) == "" {
		if email, ok := usecase.email(oauthKey.AccessToken); ok {
			oauthKey.Email = email
		}
	}

	encoded, err := usecase.store.EncodeCodexKey(oauthKey)
	if err != nil {
		return nil, nil, err
	}

	if err := usecase.store.SetKey(ch.Id, encoded); err != nil {
		return nil, nil, err
	}

	if input.ResetCaches {
		usecase.store.InitCache()
	}

	return oauthKey, ch, nil

}
