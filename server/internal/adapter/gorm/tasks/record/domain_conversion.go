package record

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func ToDomain(v *Task) *domain.Task {
	if v == nil {
		return nil
	}
	out := &domain.Task{
		ID:          v.ID,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
		TaskID:      v.TaskID,
		Platform:    string(v.Platform),
		UserId:      v.UserId,
		Group:       v.Group,
		ChannelId:   v.ChannelId,
		Quota:       v.Quota,
		Action:      v.Action,
		Status:      domain.TaskStatus(v.Status),
		FailReason:  v.FailReason,
		SubmitTime:  v.SubmitTime,
		StartTime:   v.StartTime,
		FinishTime:  v.FinishTime,
		Progress:    v.Progress,
		Properties:  *ToDomainProperties(&v.Properties),
		Username:    v.Username,
		PrivateData: *ToDomainTaskPrivateData(&v.PrivateData),
		Data:        v.Data,
	}
	return out
}

func FromDomain(v *domain.Task) *Task {
	if v == nil {
		return nil
	}
	out := &Task{
		ID:          v.ID,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
		TaskID:      v.TaskID,
		Platform:    constant.TaskPlatform(v.Platform),
		UserId:      v.UserId,
		Group:       v.Group,
		ChannelId:   v.ChannelId,
		Quota:       v.Quota,
		Action:      v.Action,
		Status:      TaskStatus(v.Status),
		FailReason:  v.FailReason,
		SubmitTime:  v.SubmitTime,
		StartTime:   v.StartTime,
		FinishTime:  v.FinishTime,
		Progress:    v.Progress,
		Properties:  *FromDomainProperties(&v.Properties),
		Username:    v.Username,
		PrivateData: *FromDomainTaskPrivateData(&v.PrivateData),
		Data:        v.Data,
	}
	return out
}

func ToDomainProperties(v *Properties) *domain.Properties {
	if v == nil {
		return nil
	}
	out := &domain.Properties{
		Input:             v.Input,
		UpstreamModelName: v.UpstreamModelName,
		OriginModelName:   v.OriginModelName,
	}
	return out
}

func FromDomainProperties(v *domain.Properties) *Properties {
	if v == nil {
		return nil
	}
	out := &Properties{
		Input:             v.Input,
		UpstreamModelName: v.UpstreamModelName,
		OriginModelName:   v.OriginModelName,
	}
	return out
}

func ToDomainTaskPrivateData(v *TaskPrivateData) *domain.TaskPrivateData {
	if v == nil {
		return nil
	}
	out := &domain.TaskPrivateData{
		Key:                 v.Key,
		UpstreamTaskID:      v.UpstreamTaskID,
		ResultURL:           v.ResultURL,
		Execution:           ToDomainTaskExecutionSnapshot(v.Execution),
		BillingSource:       v.BillingSource,
		SubscriptionId:      v.SubscriptionId,
		TokenId:             v.TokenId,
		NodeName:            v.NodeName,
		BillingContext:      ToDomainTaskBillingContext(v.BillingContext),
		ResponsesBackground: v.ResponsesBackground,
		PluginState:         v.PluginState,
		PollFailures:        v.PollFailures,
		Reconciliation:      ToDomainTaskReconciliation(v.Reconciliation),
		OpenRouterAudio:     ToDomainOpenRouterSyncTask(v.OpenRouterAudio),
		OpenRouterSync:      ToDomainOpenRouterSyncTask(v.OpenRouterSync),
	}
	return out
}

func FromDomainTaskPrivateData(v *domain.TaskPrivateData) *TaskPrivateData {
	if v == nil {
		return nil
	}
	out := &TaskPrivateData{
		Key:                 v.Key,
		UpstreamTaskID:      v.UpstreamTaskID,
		ResultURL:           v.ResultURL,
		Execution:           FromDomainTaskExecutionSnapshot(v.Execution),
		BillingSource:       v.BillingSource,
		SubscriptionId:      v.SubscriptionId,
		TokenId:             v.TokenId,
		NodeName:            v.NodeName,
		BillingContext:      FromDomainTaskBillingContext(v.BillingContext),
		ResponsesBackground: v.ResponsesBackground,
		PluginState:         v.PluginState,
		PollFailures:        v.PollFailures,
		Reconciliation:      FromDomainTaskReconciliation(v.Reconciliation),
		OpenRouterAudio:     FromDomainOpenRouterSyncTask(v.OpenRouterAudio),
		OpenRouterSync:      FromDomainOpenRouterSyncTask(v.OpenRouterSync),
	}
	return out
}

func ToDomainOpenRouterSyncTask(v *OpenRouterSyncTask) *domain.OpenRouterSyncTask {
	if v == nil {
		return nil
	}
	out := &domain.OpenRouterSyncTask{
		CredentialHash:    v.CredentialHash,
		ConfigHash:        v.ConfigHash,
		RoutingHash:       v.RoutingHash,
		CredentialIndex:   v.CredentialIndex,
		IdentityConflict:  v.IdentityConflict,
		UpstreamRequestID: v.UpstreamRequestID,
		DispatchPrepared:  v.DispatchPrepared,
		ProviderTag:       v.ProviderTag,
		ReservationSource: v.ReservationSource,
		ReserveUSD:        v.ReserveUSD,
		PricingUnknown:    v.PricingUnknown,
		Revision:          v.Revision,
		RequestID:         v.RequestID,
		BillingPreference: v.BillingPreference,
		PlannedQuota:      v.PlannedQuota,
		LeaseUntil:        v.LeaseUntil,
		Deadline:          v.Deadline,
		MetadataURL:       v.MetadataURL,
		Headers:           v.Headers,
		Transport:         ToDomainTransport(v.Transport),
		Attempts:          v.Attempts,
		NextPoll:          v.NextPoll,
		LastHTTPStatus:    v.LastHTTPStatus,
		LastError:         v.LastError,
		ResultReady:       v.ResultReady,
		ResultFailed:      v.ResultFailed,
		ResultMimeType:    v.ResultMimeType,
		ResultSize:        v.ResultSize,
		ResultSHA256:      v.ResultSHA256,
	}
	return out
}

func FromDomainOpenRouterSyncTask(v *domain.OpenRouterSyncTask) *OpenRouterSyncTask {
	if v == nil {
		return nil
	}
	out := &OpenRouterSyncTask{
		CredentialHash:    v.CredentialHash,
		ConfigHash:        v.ConfigHash,
		RoutingHash:       v.RoutingHash,
		CredentialIndex:   v.CredentialIndex,
		IdentityConflict:  v.IdentityConflict,
		UpstreamRequestID: v.UpstreamRequestID,
		DispatchPrepared:  v.DispatchPrepared,
		ProviderTag:       v.ProviderTag,
		ReservationSource: v.ReservationSource,
		ReserveUSD:        v.ReserveUSD,
		PricingUnknown:    v.PricingUnknown,
		Revision:          v.Revision,
		RequestID:         v.RequestID,
		BillingPreference: v.BillingPreference,
		PlannedQuota:      v.PlannedQuota,
		LeaseUntil:        v.LeaseUntil,
		Deadline:          v.Deadline,
		MetadataURL:       v.MetadataURL,
		Headers:           v.Headers,
		Transport:         FromDomainTransport(v.Transport),
		Attempts:          v.Attempts,
		NextPoll:          v.NextPoll,
		LastHTTPStatus:    v.LastHTTPStatus,
		LastError:         v.LastError,
		ResultReady:       v.ResultReady,
		ResultFailed:      v.ResultFailed,
		ResultMimeType:    v.ResultMimeType,
		ResultSize:        v.ResultSize,
		ResultSHA256:      v.ResultSHA256,
	}
	return out
}

func ToDomainTaskReconciliation(v *TaskReconciliation) *domain.TaskReconciliation {
	if v == nil {
		return nil
	}
	out := &domain.TaskReconciliation{
		ReservedQuota:         v.ReservedQuota,
		ReservationPending:    v.ReservationPending,
		SubmissionPending:     v.SubmissionPending,
		RefundPending:         v.RefundPending,
		Required:              v.Required,
		SettlementPending:     v.SettlementPending,
		MainApplied:           v.MainApplied,
		TargetQuota:           v.TargetQuota,
		Finalized:             v.Finalized,
		HasUpstreamCost:       v.HasUpstreamCost,
		UpstreamCostConfirmed: v.UpstreamCostConfirmed,
		UpstreamCostUSD:       v.UpstreamCostUSD,
		UpstreamIsBYOK:        v.UpstreamIsBYOK,
		UpstreamCostDetails:   v.UpstreamCostDetails,
		QuotaSaturation:       v.QuotaSaturation,
	}
	return out
}

func FromDomainTaskReconciliation(v *domain.TaskReconciliation) *TaskReconciliation {
	if v == nil {
		return nil
	}
	out := &TaskReconciliation{
		ReservedQuota:         v.ReservedQuota,
		ReservationPending:    v.ReservationPending,
		SubmissionPending:     v.SubmissionPending,
		RefundPending:         v.RefundPending,
		Required:              v.Required,
		SettlementPending:     v.SettlementPending,
		MainApplied:           v.MainApplied,
		TargetQuota:           v.TargetQuota,
		Finalized:             v.Finalized,
		HasUpstreamCost:       v.HasUpstreamCost,
		UpstreamCostConfirmed: v.UpstreamCostConfirmed,
		UpstreamCostUSD:       v.UpstreamCostUSD,
		UpstreamIsBYOK:        v.UpstreamIsBYOK,
		UpstreamCostDetails:   v.UpstreamCostDetails,
		QuotaSaturation:       v.QuotaSaturation,
	}
	return out
}

func ToDomainTaskExecutionSnapshot(v *TaskExecutionSnapshot) *domain.TaskExecutionSnapshot {
	if v == nil {
		return nil
	}
	out := &domain.TaskExecutionSnapshot{
		RequestID:   v.RequestID,
		RequestPath: v.RequestPath,
		TaskPlugin:  ToDomainTaskPluginSnapshot(v.TaskPlugin),
	}
	return out
}

func FromDomainTaskExecutionSnapshot(v *domain.TaskExecutionSnapshot) *TaskExecutionSnapshot {
	if v == nil {
		return nil
	}
	out := &TaskExecutionSnapshot{
		RequestID:   v.RequestID,
		RequestPath: v.RequestPath,
		TaskPlugin:  FromDomainTaskPluginSnapshot(v.TaskPlugin),
	}
	return out
}

func ToDomainTaskPluginSnapshot(v *TaskPluginSnapshot) *domain.TaskPluginSnapshot {
	if v == nil {
		return nil
	}
	out := &domain.TaskPluginSnapshot{
		Key:        v.Key,
		Name:       v.Name,
		Version:    v.Version,
		Author:     ToDomainTaskPluginAuthorSnapshot(v.Author),
		APIVersion: v.APIVersion,
		Generation: v.Generation,
	}
	return out
}

func FromDomainTaskPluginSnapshot(v *domain.TaskPluginSnapshot) *TaskPluginSnapshot {
	if v == nil {
		return nil
	}
	out := &TaskPluginSnapshot{
		Key:        v.Key,
		Name:       v.Name,
		Version:    v.Version,
		Author:     FromDomainTaskPluginAuthorSnapshot(v.Author),
		APIVersion: v.APIVersion,
		Generation: v.Generation,
	}
	return out
}

func ToDomainTaskPluginAuthorSnapshot(v *TaskPluginAuthorSnapshot) *domain.TaskPluginAuthorSnapshot {
	if v == nil {
		return nil
	}
	out := &domain.TaskPluginAuthorSnapshot{
		Name: v.Name,
		URL:  v.URL,
	}
	return out
}

func FromDomainTaskPluginAuthorSnapshot(v *domain.TaskPluginAuthorSnapshot) *TaskPluginAuthorSnapshot {
	if v == nil {
		return nil
	}
	out := &TaskPluginAuthorSnapshot{
		Name: v.Name,
		URL:  v.URL,
	}
	return out
}

func ToDomainTaskBillingContext(v *TaskBillingContext) *domain.TaskBillingContext {
	if v == nil {
		return nil
	}
	out := &domain.TaskBillingContext{
		SettlementMode:       v.SettlementMode,
		OpenRouterCost:       ToDomainBasis(v.OpenRouterCost),
		OpenRouterOrderFacts: v.OpenRouterOrderFacts,
		ModelPrice:           v.ModelPrice,
		GroupRatio:           v.GroupRatio,
		ModelRatio:           v.ModelRatio,
		OtherRatios:          v.OtherRatios,
		OriginModelName:      v.OriginModelName,
		PerCallBilling:       v.PerCallBilling,
		TieredSnapshot:       ToDomainTiered(v.TieredSnapshot),
		BillingFX:            ToDomainTaskBillingFX(v.BillingFX),
		SubmitGroup:          ToDomainTaskBillingSubmitGroup(v.SubmitGroup),
	}
	out.BillingFXEvidence = v.billingFXRaw
	out.SubmitGroupEvidence = v.submitGroupRaw
	out.BillingFXPresent = v.HasBillingFX()
	out.SubmitGroupPresent = v.HasSubmitGroup()
	out.BillingFXComplete = v.HasCompleteBillingFX()
	out.SubmitGroupComplete = v.HasCompleteSubmitGroup()
	return out
}

func FromDomainTaskBillingContext(v *domain.TaskBillingContext) *TaskBillingContext {
	if v == nil {
		return nil
	}
	out := &TaskBillingContext{
		SettlementMode:       v.SettlementMode,
		OpenRouterCost:       FromDomainBasis(v.OpenRouterCost),
		OpenRouterOrderFacts: v.OpenRouterOrderFacts,
		ModelPrice:           v.ModelPrice,
		GroupRatio:           v.GroupRatio,
		ModelRatio:           v.ModelRatio,
		OtherRatios:          v.OtherRatios,
		OriginModelName:      v.OriginModelName,
		PerCallBilling:       v.PerCallBilling,
		TieredSnapshot:       FromDomainTiered(v.TieredSnapshot),
		BillingFX:            FromDomainTaskBillingFX(v.BillingFX),
		SubmitGroup:          FromDomainTaskBillingSubmitGroup(v.SubmitGroup),
	}
	out.billingFXRaw = v.BillingFXEvidence
	out.submitGroupRaw = v.SubmitGroupEvidence
	return out
}

func ToDomainTaskBillingFX(v *TaskBillingFX) *domain.TaskBillingFX {
	if v == nil {
		return nil
	}
	out := &domain.TaskBillingFX{
		SchemaVersion:      v.SchemaVersion,
		Source:             v.Source,
		Rate:               v.Rate,
		PublicationVersion: v.PublicationVersion,
		EffectiveAt:        v.EffectiveAt,
		FetchedAt:          v.FetchedAt,
	}
	return out
}

func FromDomainTaskBillingFX(v *domain.TaskBillingFX) *TaskBillingFX {
	if v == nil {
		return nil
	}
	out := &TaskBillingFX{
		SchemaVersion:      v.SchemaVersion,
		Source:             v.Source,
		Rate:               v.Rate,
		PublicationVersion: v.PublicationVersion,
		EffectiveAt:        v.EffectiveAt,
		FetchedAt:          v.FetchedAt,
	}
	return out
}

func ToDomainTaskBillingSubmitGroup(v *TaskBillingSubmitGroup) *domain.TaskBillingSubmitGroup {
	if v == nil {
		return nil
	}
	out := &domain.TaskBillingSubmitGroup{
		PureRatio:       v.PureRatio,
		HasSpecialRatio: v.HasSpecialRatio,
		SpecialRatio:    v.SpecialRatio,
	}
	return out
}

func FromDomainTaskBillingSubmitGroup(v *domain.TaskBillingSubmitGroup) *TaskBillingSubmitGroup {
	if v == nil {
		return nil
	}
	out := &TaskBillingSubmitGroup{
		PureRatio:       v.PureRatio,
		HasSpecialRatio: v.HasSpecialRatio,
		SpecialRatio:    v.SpecialRatio,
	}
	return out
}

func ToDomainBasis(v *common.OpenRouterCostBasis) *billingdomain.ProviderBasis {
	if v == nil {
		return nil
	}
	out := billingdomain.ProviderBasis(*v)
	return &out
}
func ToDomainTiered(v *billingexpr.BillingSnapshot) *billingdomain.TieredSnapshot {
	if v == nil {
		return nil
	}
	return &billingdomain.TieredSnapshot{
		BillingMode:               v.BillingMode,
		ModelName:                 v.ModelName,
		ExprString:                v.ExprString,
		ExprHash:                  v.ExprHash,
		GroupRatio:                v.GroupRatio,
		EstimatedPromptTokens:     v.EstimatedPromptTokens,
		EstimatedCompletionTokens: v.EstimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: v.EstimatedQuotaBeforeGroup,
		EstimatedQuotaAfterGroup:  v.EstimatedQuotaAfterGroup,
		EstimatedTier:             v.EstimatedTier,
		EstimatedBillingUnit:      string(v.EstimatedBillingUnit),
		EstimatedFixedPrice:       v.EstimatedFixedPrice,
		QuotaPerUnit:              v.QuotaPerUnit,
		ExprVersion:               v.ExprVersion,
		TaskUsageBilling:          v.TaskUsageBilling,
		UsageFacts:                v.UsageFacts,
	}
}
func ToDomainTransport(v dto.ChannelSettings) domain.TransportSettings {
	out := domain.TransportSettings{
		TaskPluginKey:                     v.TaskPluginKey,
		ForceFormat:                       v.ForceFormat,
		ThinkingToContent:                 v.ThinkingToContent,
		Proxy:                             v.Proxy,
		PassThroughBodyEnabled:            v.PassThroughBodyEnabled,
		SystemPrompt:                      v.SystemPrompt,
		SystemPromptOverride:              v.SystemPromptOverride,
		OpenRouterModelReserveUSD:         v.OpenRouterModelReserveUSD,
		OpenRouterSonarResearchReserveUSD: v.OpenRouterSonarResearchReserveUSD,
		HTTPProtocol:                      v.HTTPProtocol,
		HTTP2ConnectionShards:             v.HTTP2ConnectionShards,
	}
	if v.OpenRouterReservationSource != nil {
		out.OpenRouterReservationSource = make(map[string]string, len(v.OpenRouterReservationSource))
		for k, value := range v.OpenRouterReservationSource {
			out.OpenRouterReservationSource[k] = string(value)
		}
	}
	return out
}
func FromDomainBasis(v *billingdomain.ProviderBasis) *common.OpenRouterCostBasis {
	if v == nil {
		return nil
	}
	out := common.OpenRouterCostBasis(*v)
	return &out
}
func FromDomainTiered(v *billingdomain.TieredSnapshot) *billingexpr.BillingSnapshot {
	if v == nil {
		return nil
	}
	return &billingexpr.BillingSnapshot{
		BillingMode:               v.BillingMode,
		ModelName:                 v.ModelName,
		ExprString:                v.ExprString,
		ExprHash:                  v.ExprHash,
		GroupRatio:                v.GroupRatio,
		EstimatedPromptTokens:     v.EstimatedPromptTokens,
		EstimatedCompletionTokens: v.EstimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: v.EstimatedQuotaBeforeGroup,
		EstimatedQuotaAfterGroup:  v.EstimatedQuotaAfterGroup,
		EstimatedTier:             v.EstimatedTier,
		EstimatedBillingUnit:      billingexpr.BillingUnit(v.EstimatedBillingUnit),
		EstimatedFixedPrice:       v.EstimatedFixedPrice,
		QuotaPerUnit:              v.QuotaPerUnit,
		ExprVersion:               v.ExprVersion,
		TaskUsageBilling:          v.TaskUsageBilling,
		UsageFacts:                v.UsageFacts,
	}
}
func FromDomainTransport(v domain.TransportSettings) dto.ChannelSettings {
	out := dto.ChannelSettings{
		TaskPluginKey:                     v.TaskPluginKey,
		ForceFormat:                       v.ForceFormat,
		ThinkingToContent:                 v.ThinkingToContent,
		Proxy:                             v.Proxy,
		PassThroughBodyEnabled:            v.PassThroughBodyEnabled,
		SystemPrompt:                      v.SystemPrompt,
		SystemPromptOverride:              v.SystemPromptOverride,
		OpenRouterModelReserveUSD:         v.OpenRouterModelReserveUSD,
		OpenRouterSonarResearchReserveUSD: v.OpenRouterSonarResearchReserveUSD,
		HTTPProtocol:                      v.HTTPProtocol,
		HTTP2ConnectionShards:             v.HTTP2ConnectionShards,
	}
	if v.OpenRouterReservationSource != nil {
		out.OpenRouterReservationSource = make(map[string]dto.OpenRouterReservationSource, len(v.OpenRouterReservationSource))
		for k, value := range v.OpenRouterReservationSource {
			out.OpenRouterReservationSource[k] = dto.OpenRouterReservationSource(value)
		}
	}
	return out
}

// RestoreDomain retains in-flight references to revision and reconciliation
// state while applying mutations made by a storage capability.
func RestoreDomain(target *domain.Task, source *Task) {
	next := ToDomain(source)
	if old, value := target.PrivateData.Reconciliation, next.PrivateData.Reconciliation; old != nil && value != nil {
		*old = *value
		next.PrivateData.Reconciliation = old
	}
	if old, value := target.PrivateData.OpenRouterSync, next.PrivateData.OpenRouterSync; old != nil && value != nil {
		*old = *value
		next.PrivateData.OpenRouterSync = old
	}
	if old, value := target.PrivateData.OpenRouterAudio, next.PrivateData.OpenRouterAudio; old != nil && value != nil {
		*old = *value
		next.PrivateData.OpenRouterAudio = old
	}
	*target = *next
}

// Snapshot projects the gateway realtime fetch observation through the domain owner.
func (t *Task) Snapshot() domain.TaskSnapshot { return ToDomain(t).Snapshot() }
