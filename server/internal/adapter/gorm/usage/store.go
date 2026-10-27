package usage

import (
	"context"
	"errors"
	"fmt"

	keyrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

type Store struct {
	ReadToken                     func(int, int) (*keyrecord.Token, error)
	RankingCatalog                func() ([]pricingdomain.PricingVendor, []pricingdomain.Pricing)
	ConsumeEnabled, ExportEnabled func() bool
	Writer                        Writer
	UserConsumption               func(int, int)
	ChannelConsumption            func(int, int)
}

var errGatewayContext = errors.New("usage recording requires a non-nil gateway Gin context")

func (s Store) AddUserConsumption(id, quota int) {
	s.UserConsumption(id, quota)
}

func (s Store) AddChannelConsumption(id, quota int) {
	s.ChannelConsumption(id, quota)
}

// Missing request observations return a technical error before any side effect.
// Storage never imports Gin or recovers the gateway context.
func (s Store) WriteConsumption(ctx context.Context, in domain.Consumption) error {
	observation, metadata, err := recordingObservation(ctx)
	if err != nil {
		return err
	}
	other := consumptionOther(in, metadata)
	if in.Audio != nil && in.Audio.CacheDiscountUnavailable {
		logger.LogWarn(observation.Context, fmt.Sprintf(
			"realtime cache discount unavailable: scope=session reason=%s", in.Audio.CacheDiscountReason,
		))
	}
	if in.Request.ClampWarning != "" {
		logger.LogWarn(observation.Context, in.Request.ClampWarning)
	}
	id := in.Record
	s.Writer.WriteConsumeLog(observation, id.UserID, RecordConsumeLogParams{
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

func (s Store) WriteFailure(ctx context.Context, in domain.Failure) error {
	observation, metadata, err := recordingObservation(ctx)
	if err != nil {
		return err
	}
	var other *model.LogOther
	if in.Consumption != nil {
		other = consumptionOther(*in.Consumption, metadata)
	} else {
		other = new(model.LogOther)
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
	s.Writer.WriteErrorLog(
		observation, id.UserID, id.ChannelID, id.ModelName, id.TokenName, in.Content,
		id.TokenID, id.UseTimeSeconds, id.IsStream, id.Group, other,
	)
	return nil
}
