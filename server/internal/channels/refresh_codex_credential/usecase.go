package refresh_codex_credential

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"strings"
	"time"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	Settings(*domain.Channel) dto.ChannelSettings
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
		return nil, nil, fmt.Errorf("channel not found")
	}
	if ch.Type != constant.ChannelTypeCodex {
		return nil, nil, fmt.Errorf("channel type is not Codex")
	}

	oauthKey, err := parseCodexOAuthKey(strings.TrimSpace(ch.Key))
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(oauthKey.RefreshToken) == "" {
		return nil, nil, fmt.Errorf("codex channel: refresh_token is required to refresh credential")
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := usecase.refresh(refreshCtx, oauthKey.RefreshToken, usecase.store.Settings(ch).Proxy)
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

	encoded, err := common.Marshal(oauthKey)
	if err != nil {
		return nil, nil, err
	}

	if err := usecase.store.SetKey(ch.Id, string(encoded)); err != nil {
		return nil, nil, err
	}

	if input.ResetCaches {
		usecase.store.InitCache()
	}

	return oauthKey, ch, nil

}
func parseCodexOAuthKey(raw string) (*domain.CodexOAuthKey, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("codex channel: empty oauth key")
	}
	var key domain.CodexOAuthKey
	if err := common.Unmarshal([]byte(raw), &key); err != nil {
		return nil, errors.New("codex channel: invalid oauth key json")
	}
	return &key, nil
}
