package get_codex_usage

import (
	"context"

	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"net/http"
	"strings"
	"time"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	Settings(*domain.Channel) dto.ChannelSettings
	SetKey(int, string) error
	InitCache()
}
type Remote interface {
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
func (usecase Usecase) Execute(parent context.Context, input Input) (Output, *domain.Failure) {
	ch, err := usecase.store.Get(input.ChannelID, true)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "internal", Cause: err}
	}
	if ch == nil {
		return Output{}, &domain.Failure{Kind: "message", Message: "channel not found"}
	}
	if ch.Type != constant.ChannelTypeCodex {
		return Output{}, &domain.Failure{Kind: "message", Message: "channel type is not Codex"}
	}
	if ch.ChannelInfo.IsMultiKey {
		return Output{}, &domain.Failure{Kind: "message", Message: "multi-key channel is not supported"}
	}

	oauthKey, err := domain.ParseCodexOAuthKey(strings.TrimSpace(ch.Key))
	if err != nil {
		usecase.log("failed to parse oauth key: " + err.Error())
		return Output{}, &domain.Failure{Kind: "message", Message: "解析凭证失败，请检查渠道配置"}
	}
	accessToken := strings.TrimSpace(oauthKey.AccessToken)
	accountID := strings.TrimSpace(oauthKey.AccountID)
	if accessToken == "" {
		return Output{}, &domain.Failure{Kind: "message", Message: "codex channel: access_token is required"}
	}
	if accountID == "" {
		return Output{}, &domain.Failure{Kind: "message", Message: "codex channel: account_id is required"}
	}

	fetch, err := usecase.remote.Open(usecase.store.Settings(ch).Proxy)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "internal", Cause: err}
	}

	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()

	statusCode, body, err := fetch(ctx, ch.BaseURLValue(), accessToken, accountID)
	if err != nil {
		usecase.log("failed to fetch codex usage" + ": " + err.Error())
		return Output{}, &domain.Failure{Kind: "message", Message: "获取用量信息失败，请稍后重试"}
	}

	if (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) && strings.TrimSpace(oauthKey.RefreshToken) != "" {
		refreshCtx, refreshCancel := context.WithTimeout(parent, 10*time.Second)
		defer refreshCancel()

		res, refreshErr := usecase.remote.Refresh(refreshCtx, oauthKey.RefreshToken, usecase.store.Settings(ch).Proxy)
		if refreshErr == nil {
			oauthKey.AccessToken = res.AccessToken
			oauthKey.RefreshToken = res.RefreshToken
			oauthKey.LastRefresh = usecase.now().Format(time.RFC3339)
			oauthKey.Expired = res.ExpiresAt.Format(time.RFC3339)
			if strings.TrimSpace(oauthKey.Type) == "" {
				oauthKey.Type = "codex"
			}

			encoded, encErr := common.Marshal(oauthKey)
			if encErr == nil {
				_ = usecase.store.SetKey(ch.Id, string(encoded))
				usecase.store.InitCache()
			}

			ctx2, cancel2 := context.WithTimeout(parent, 15*time.Second)
			defer cancel2()
			statusCode, body, err = fetch(ctx2, ch.BaseURLValue(), oauthKey.AccessToken, accountID)
			if err != nil {
				usecase.log("failed to fetch codex usage" + " after refresh: " + err.Error())
				return Output{}, &domain.Failure{Kind: "message", Message: "获取用量信息失败，请稍后重试"}
			}
		}
	}

	var payload any
	if common.Unmarshal(body, &payload) != nil {
		payload = string(body)
	}

	ok := statusCode >= 200 && statusCode < 300
	message := ""
	if !ok {
		message = fmt.Sprintf("upstream status: %d", statusCode)
	}
	return Output{Success: ok, Message: message, UpstreamStatus: statusCode, Data: payload}, nil

}
