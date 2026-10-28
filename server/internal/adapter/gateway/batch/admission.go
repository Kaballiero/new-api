package batch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/adapter/http/batchaccount"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	providerrouting "github.com/QuantumNous/new-api/relay/provider"

	"github.com/gin-gonic/gin"
)

type AdmissionGateway struct{}
type admission struct {
	c         *gin.Context
	channel   *model.Channel
	info      *relaycommon.RelayInfo
	request   domain.CreateRequest
	facts     domain.AdmissionFacts
	estimator *openrouter.RequestEstimator
	fields    map[string]json.RawMessage
}

func (AdmissionGateway) Open(ctx context.Context) (domain.Admission, error) {
	c := ctx.(*gin.Context)
	channel, err := channelstore.CacheGetChannel(common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	if err != nil {
		return nil, err
	}
	return &admission{c: c, channel: channel}, nil
}
func (s *admission) ValidateHeaders() error {
	return batchaccount.ValidateBatchChannelHeaders(s.channel)
}
func (s *admission) Initialize(request domain.CreateRequest) (domain.AdmissionFacts, error) {
	c := s.c
	info := &relaycommon.RelayInfo{OriginModelName: request.Model}
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: request.Model}
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return domain.AdmissionFacts{}, err
	}
	headers := make(map[string]string)
	for _, name := range []string{"anthropic-beta", "anthropic-version"} {
		if value := c.GetHeader(name); value != "" {
			headers[name] = value
		}
	}
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group := c.GetString("auto_group"); group != "" {
		usingGroup = group
	}
	info.UserId = c.GetInt("id")
	info.UserGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	info.TokenGroup = common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	info.UsingGroup = usingGroup
	info.RequestURLPath = request.Endpoint
	s.c, s.info, s.request = c, info, request
	s.facts = domain.AdmissionFacts{UserID: info.UserId, TokenID: common.GetContextKeyInt(c,
		constant.ContextKeyTokenId), ChannelID: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		ChannelType: s.channel.Type, CredentialIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
		UpstreamModel: info.UpstreamModelName, UsingGroup: usingGroup, UserGroup: info.UserGroup,
		TokenGroup: info.TokenGroup, Headers: headers}
	return s.facts, nil
}
func (s *admission) Provider(request domain.CreateRequest) ([]byte, error) {
	s.info.InitChannelMeta(s.c)
	s.info.UpstreamModelName = s.facts.UpstreamModel
	s.info.OriginModelName = request.Model
	envelope := request
	envelope.Model = s.facts.UpstreamModel
	projection, err := common.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	projection, err = relaycommon.ApplyRequestOverrides(s.c, projection, s.info)
	if err != nil {
		return nil, err
	}
	provider, err := providerrouting.Extract(projection)
	if err == nil {
		err = providerrouting.Validate("batch", provider)
	}
	if err != nil {
		return nil, err
	}
	s.estimator = openrouter.NewRequestEstimator()
	return provider, nil
}
func (s *admission) PrepareBody(item domain.RequestItem) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(item.Body, &fields); err != nil {
		return nil, &domain.RuleError{Kind: domain.InvalidPayload, Cause: err}
	}
	if _, exists := fields["model"]; exists {
		fields["model"], _ = common.Marshal(s.facts.UpstreamModel)
	}
	body, err := common.Marshal(fields)
	if err != nil {
		return nil, &domain.RuleError{Kind: domain.InvalidPayload, Cause: err}
	}
	s.fields = fields
	body, err = relaycommon.ApplyParamOverride(body, common.GetContextKeyStringMap(s.c, constant.ContextKeyChannelParamOverride),
		relaycommon.BuildParamOverrideContext(s.info))
	if err != nil {
		return nil, &domain.RuleError{Kind: domain.InvalidChannelParameters, Cause: err}
	}
	return body, nil
}
func (s *admission) Count(model string, body []byte) (int, error) {
	return CountBatchItemTokens(s.c, model, s.request.Endpoint, body)
}
func (s *admission) Estimate(body, provider []byte) (float64, error) {
	promptTokens, err := s.Count(s.request.Model, body)
	if err != nil {
		return 0, err
	}
	fields := s.fields
	s.info.SetEstimatePromptTokens(promptTokens)
	// OpenRouter publishes dedicated :batch endpoint prices. Discover once
	// per possible final model; never infer a universal synchronous discount.
	models, modelErr := openrouter.ModelsFromRequest(body)
	if modelErr != nil {
		return 0, modelErr
	}
	if len(models) == 0 {
		models = []string{s.facts.UpstreamModel}
	}
	adaptor := relay.GetAdaptor(s.info.ApiType)
	if adaptor == nil {
		return 0, &domain.RuleError{Kind: domain.OpenRouterAdaptorUnsupported}
	}
	adaptor.Init(s.info)
	for _, name := range models {
		batchModel := strings.TrimSuffix(name, ":batch") + ":batch"
		if err = s.estimator.Discover(s.c, s.info, adaptor, batchModel); err != nil {
			break
		}
		s.estimator.Models[name] = s.estimator.Models[batchModel]
	}
	if err == nil {
		pricingBody := body
		// Envelope routing applies to every item for admission, but remains
		// separate from each item's native payload on the wire.
		_, itemProvider := fields["provider"]
		if _, present := fields["model"]; !present || len(provider) > 0 || itemProvider {
			var prepared map[string]json.RawMessage
			if err = common.Unmarshal(body, &prepared); err == nil {
				prepared["model"], _ = common.Marshal(s.facts.UpstreamModel)
				if len(provider) > 0 {
					prepared["provider"] = provider
				} else {
					delete(prepared, "provider")
				}
				pricingBody, err = common.Marshal(prepared)
			}
		}
		if err == nil {
			estimate, estimateErr := s.estimator.Estimate(s.info, pricingBody, "batch_"+strings.TrimPrefix(s.request.Endpoint, "/v1/"))
			err = estimateErr
			return estimate, err
		}
	}
	return 0, err

}
func (s *admission) ConfigHash() (string, error) {
	return batchaccount.BatchChannelConfigHash(s.channel)
}
func (s *admission) CredentialHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(common.GetContextKeyString(s.c, constant.ContextKeyChannelKey))))
}
