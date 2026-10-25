package usage

import (
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	batch "github.com/QuantumNous/new-api/internal/batch/domain"
	billing "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type BatchLogs struct{}

func (BatchLogs) Item(job batch.Job, account batch.LogAccount, customID string, quota int, details string) (string, error) {
	var facts struct {
		Usage *dto.Usage     `json:"usage"`
		Other map[string]any `json:"other"`
	}
	if err := common.UnmarshalJsonStr(details, &facts); err != nil {
		return "", err
	}
	if facts.Other == nil {
		facts.Other = make(map[string]any)
	}
	facts.Other["batch_id"] = job.PublicID
	facts.Other["batch_custom_id"] = customID
	otherJSON, err := common.Marshal(facts.Other)
	if err != nil {
		return "", err
	}
	requestID := fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":"+customID)))
	log := model.Log{UserId: account.UserID, Username: account.Username, TokenName: account.TokenName,
		TokenId: account.TokenID, ChannelId: job.ChannelID, ModelName: job.Model, Group: job.UsingGroup,
		Quota: quota, Type: model.LogTypeConsume, CreatedAt: common.GetTimestamp(), RequestId: requestID,
		Other: string(otherJSON), Content: "Batch request"}
	if facts.Usage != nil {
		log.PromptTokens = facts.Usage.PromptTokens
		log.CompletionTokens = facts.Usage.CompletionTokens
	}
	payload, err := common.Marshal(log)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
func (BatchLogs) Aggregate(job batch.Job, account batch.LogAccount, quota int, cost float64, basis billing.ProviderBasis) (string, error) {
	other := new(model.LogOther)
	other.SetPublic("batch_id", job.PublicID)
	other.SetPublic("settlement_scope", "job")
	other.SetPublic("settlement_mode", batch.BatchSettlementOpenRouterJobCostV2)
	other.SetPublic("actual_quota", quota)
	other.SetAdmin("upstream_cost_usd", cost)
	other.SetAdmin("openrouter_cost_basis", storedCostBasis(basis))
	otherJSON, err := common.Marshal(other)
	if err != nil {
		return "", err
	}
	requestID := fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":aggregate:v2")))
	log := model.Log{UserId: account.UserID, Username: account.Username, TokenName: account.TokenName,
		TokenId: account.TokenID, ChannelId: job.ChannelID, ModelName: job.Model, Group: job.UsingGroup,
		Quota: quota, Type: model.LogTypeConsume, CreatedAt: common.GetTimestamp(), RequestId: requestID,
		Other: string(otherJSON), Content: "Batch aggregate"}
	var tokens struct {
		Prompt     *int `json:"prompt_tokens"`
		Completion *int `json:"completion_tokens"`
	}
	if common.UnmarshalJsonStr(job.UpstreamUsage, &tokens) == nil {
		if tokens.Prompt != nil && *tokens.Prompt >= 0 && *tokens.Prompt <= math.MaxInt32 {
			log.PromptTokens = *tokens.Prompt
		}
		if tokens.Completion != nil && *tokens.Completion >= 0 && *tokens.Completion <= math.MaxInt32 {
			log.CompletionTokens = *tokens.Completion
		}
	}
	payload, err := common.Marshal(log)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
