package get_codex_usage

import (
	"context"

	"fmt"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"net/http"
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
type Remote interface {
	DecodeUsage([]byte) (any, error)
	Open(string) (domain.CodexUsageFetch, error)
	Refresh(context.Context, string, string) (*domain.CodexTokenResult, error)
}
type Usecase struct {
	store  Store
	remote Remote
	log    func(string)
	now    func() time.Time
}

func NewUsecase(store Store, remote Remote, log func(string), now func() time.Time) Usecase {
	return Usecase{store: store, remote: remote, log: log, now: now}
}

// Execute returns storage and proxy failures as they are: they are answered
// with their text and HTTP 200.
func (usecase Usecase) Execute(parent context.Context, input Input) (Output, error) {
	ch, err := usecase.store.Get(input.ChannelID, true)
	if err != nil {
		return Output{}, err
	}
	if ch == nil {
		return Output{}, domain.ErrCodexChannelNotFound
	}
	if ch.Type != constant.ChannelTypeCodex {
		return Output{}, domain.ErrNotCodexChannel
	}
	if ch.ChannelInfo.IsMultiKey {
		return Output{}, domain.ErrCodexMultiKey
	}

	oauthKey, err := domain.ParseCodexOAuthKey(strings.TrimSpace(ch.Key), usecase.store.DecodeCodexKey)
	if err != nil {
		usecase.log("failed to parse oauth key: " + err.Error())
		return Output{}, domain.ErrCodexCredentialInvalid
	}
	accessToken := strings.TrimSpace(oauthKey.AccessToken)
	accountID := strings.TrimSpace(oauthKey.AccountID)
	if accessToken == "" {
		return Output{}, domain.ErrCodexAccessTokenRequired
	}
	if accountID == "" {
		return Output{}, domain.ErrCodexAccountIDRequired
	}

	fetch, err := usecase.remote.Open(usecase.store.Proxy(ch))
	if err != nil {
		return Output{}, err
	}

	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()

	statusCode, body, err := fetch(ctx, ch.BaseURLValue(), accessToken, accountID)
	if err != nil {
		usecase.log("failed to fetch codex usage" + ": " + err.Error())
		return Output{}, domain.ErrCodexUsageFailed
	}

	if (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) && strings.TrimSpace(oauthKey.RefreshToken) != "" {
		refreshCtx, refreshCancel := context.WithTimeout(parent, 10*time.Second)
		defer refreshCancel()

		res, refreshErr := usecase.remote.Refresh(refreshCtx, oauthKey.RefreshToken, usecase.store.Proxy(ch))
		if refreshErr == nil {
			oauthKey.AccessToken = res.AccessToken
			oauthKey.RefreshToken = res.RefreshToken
			oauthKey.LastRefresh = usecase.now().Format(time.RFC3339)
			oauthKey.Expired = res.ExpiresAt.Format(time.RFC3339)
			if strings.TrimSpace(oauthKey.Type) == "" {
				oauthKey.Type = "codex"
			}

			encoded, encErr := usecase.store.EncodeCodexKey(oauthKey)
			if encErr == nil {
				_ = usecase.store.SetKey(ch.Id, encoded)
				usecase.store.InitCache()
			}

			ctx2, cancel2 := context.WithTimeout(parent, 15*time.Second)
			defer cancel2()
			statusCode, body, err = fetch(ctx2, ch.BaseURLValue(), oauthKey.AccessToken, accountID)
			if err != nil {
				usecase.log("failed to fetch codex usage" + " after refresh: " + err.Error())
				return Output{}, domain.ErrCodexUsageFailed
			}
		}
	}

	payload, decodeErr := usecase.remote.DecodeUsage(body)
	if decodeErr != nil {
		payload = string(body)
	}

	ok := statusCode >= 200 && statusCode < 300
	message := ""
	if !ok {
		message = fmt.Sprintf("upstream status: %d", statusCode)
	}
	return Output{Success: ok, Message: message, UpstreamStatus: statusCode, Data: payload}, nil

}
