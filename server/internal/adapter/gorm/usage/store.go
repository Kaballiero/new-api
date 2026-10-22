package usage

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

type Store struct{ ConsumeEnabled, ExportEnabled func() bool }

var errGatewayContext = errors.New("usage recording requires a non-nil gateway Gin context")

func (Store) AddUserConsumption(id, quota int)    { model.UpdateUserUsedQuotaAndRequestCount(id, quota) }
func (Store) AddChannelConsumption(id, quota int) { model.UpdateChannelUsedQuota(id, quota) }

// A3 compatibility: valid Gin contexts retain their identity, including without Request.
// Other context.Context implementations return a technical error before any side effect.
func (s Store) WriteConsumption(ctx context.Context, in domain.Consumption) error {
	c, metadata, err := gatewayContext(ctx)
	if err != nil {
		return err
	}
	other := consumptionOther(in, metadata)
	if in.Audio != nil && in.Audio.CacheDiscountUnavailable {
		logger.LogWarn(c, fmt.Sprintf(
			"realtime cache discount unavailable: scope=session reason=%s", in.Audio.CacheDiscountReason,
		))
	}
	if in.Request.ClampWarning != "" {
		logger.LogWarn(c, in.Request.ClampWarning)
	}
	id := in.Record
	model.RecordConsumeLogWithSettings(c, id.UserID, model.RecordConsumeLogParams{
		ChannelId:        id.ChannelID,
		PromptTokens:     id.PromptTokens,
		CompletionTokens: id.CompletionTokens,
		ModelName:        id.ModelName,
		TokenName:        id.TokenName,
		Quota:            id.Quota,
		Content:          in.RenderedContent,
		TokenId:          id.TokenID,
		UseTimeSeconds:   id.UseTimeSeconds,
		IsStream:         id.IsStream,
		Group:            id.Group,
		Other:            other,
	}, s.ConsumeEnabled, s.ExportEnabled)
	return nil
}
func (Store) WriteFailure(ctx context.Context, in domain.Failure) error {
	c, metadata, err := gatewayContext(ctx)
	if err != nil {
		return err
	}
	var other *model.LogOther
	if in.Consumption != nil {
		other = consumptionOther(*in.Consumption, metadata)
	} else {
		other = model.NewLogOther()
		if in.Request.URLPresent {
			other.SetPublic("request_path", in.Request.Path)
		}
		other.SetPublic("error_type", in.ErrorType)
		other.SetPublic("error_code", in.ErrorCode)
		other.SetPublic("status_code", in.StatusCode)
		appendRelayAdmin(other, &in.Request, metadata)
		appendPlugin(other, in.Plugin)
	}
	id := in.Record
	model.RecordErrorLog(
		c, id.UserID, id.ChannelID, id.ModelName, id.TokenName, in.Content,
		id.TokenID, id.UseTimeSeconds, id.IsStream, id.Group, other,
	)
	return nil
}
