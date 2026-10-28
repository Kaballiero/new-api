package taskrecord

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/internal/tasks/hold_sync_task"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/tasks/begin_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/commit_sync_reservation"
	"github.com/QuantumNous/new-api/internal/tasks/complete_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/configure_sync_submission"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/fail_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/record_sync_generation"
	"github.com/QuantumNous/new-api/internal/tasks/record_sync_receipt"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

var syncTasks = sync.OnceValue(tasksapp.NewReconciler)

func openRouterSyncIdentity(info *relaycommon.RelayInfo) (constant.TaskPlatform, string, error) {
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		if info.NativeImage != nil {
			return constant.TaskPlatformOpenRouterImage, "image", nil
		}
	case relayconstant.RelayModeRerank:
		return constant.TaskPlatformOpenRouterRerank, "rerank", nil
	case relayconstant.RelayModeAudioSpeech, relayconstant.RelayModeAudioTranscription:
		return constant.TaskPlatformOpenRouterAudio, "audio", nil
	case relayconstant.RelayModeChatCompletions:
		if info.OpenRouterCost != nil && info.OpenRouterCost.ChatImage && !info.OpenRouterCost.BufferedAudio {
			return constant.TaskPlatformOpenRouterChatImage, "chat-image", nil
		}
		if info.OpenRouterCost != nil && info.OpenRouterCost.BufferedAudio {
			return constant.TaskPlatformOpenRouterAudio, "audio", nil
		}
		if info.OpenRouterCost != nil && info.OpenRouterCost.ChatText {
			return constant.TaskPlatformOpenRouterChatImage, "chat", nil
		}
	}
	return "", "", errors.New("unsupported OpenRouter synchronous operation")
}

// BeginOpenRouterSyncTask maps the provider operation into the durable task owner.
func BeginOpenRouterSyncTask(c *gin.Context, info *relaycommon.RelayInfo, quota int) error {
	if err := domain.ValidateNewSyncReservation(quota, info.NativeImage != nil || info.OpenRouterCost != nil && (info.OpenRouterCost.ChatImage || info.OpenRouterCost.ChatText), relaycommon.HasOpenRouterSyncTask(info)); err != nil {
		return err
	}
	platform, action, err := openRouterSyncIdentity(info)
	if err != nil {
		return err
	}
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{Action: action}
	input := begin_sync_task.Input{Task: record.ToDomain(InitTaskSubmission(c, platform, info)), Quota: quota,
		RequestID: info.RequestId, Preference: common.NormalizeBillingPreference(info.UserSetting.BillingPreference),
	}
	if platform == constant.TaskPlatformOpenRouterChatImage {
		input.Account = domain.SyncSubmissionAccount{ChannelType: info.ChannelType, Transport: record.ToDomainTransport(info.ChannelSetting), Headers: info.HeadersOverride,
			APIKey: info.ApiKey, CredentialIndex: info.ChannelMultiKeyIndex, RoutingHash: info.OpenRouterCost.RoutingHash}
	}
	if native := info.NativeImage; native != nil {
		input.Native = &domain.SyncNativeImage{ProviderTag: native.ProviderTag, ReserveUSD: native.ReserveUSD, PricingUnknown: native.PricingUnknown}
	}
	task, err := syncTasks().Begin(context.Background(), input)
	if err != nil {
		return err
	}
	info.OpenRouterCost.TaskID = task.TaskID
	c.Header("X-Task-Id", task.TaskID)
	return nil
}

func openRouterSyncTask(info *relaycommon.RelayInfo) (*domain.Task, error) {
	if !relaycommon.HasOpenRouterSyncTask(info) {
		return nil, errors.New("missing durable OpenRouter synchronous task")
	}
	input := domain.SyncIdentity{TaskID: info.OpenRouterCost.TaskID, Operation: func() (string, string, error) {
		platform, action, err := openRouterSyncIdentity(info)
		return string(platform), action, err
	}, UserID: info.UserId, ChannelID: info.ChannelId,
		TokenID: info.TokenId, UpstreamModel: info.UpstreamModelName, RequestID: info.RequestId, NativeImage: info.NativeImage != nil}
	if info.NativeImage != nil {
		input.ProviderTag = info.NativeImage.ProviderTag
	}
	return syncTasks().Load(context.Background(), input)
}

type syncReservationHold struct {
	context *gin.Context
	info    *relaycommon.RelayInfo
}

func (h syncReservationHold) Quota() int {
	quota := h.info.Billing.GetPreConsumedQuota()
	h.info.PriceData.Quota = quota
	return quota
}
func (h syncReservationHold) Transfer(quota int) error { return h.info.Billing.Settle(quota) }
func (h syncReservationHold) Record(task *domain.Task) error {
	return LogTaskConsumption(h.context, h.info, record.FromDomain(task))
}

func CommitOpenRouterSyncReservation(c *gin.Context, info *relaycommon.RelayInfo) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	return syncTasks().Commit(context.Background(), commit_sync_reservation.Input{Task: task, Source: info.BillingSource, SubscriptionID: info.SubscriptionId, Hold: syncReservationHold{context: c, info: info}})
}
func ConfigureOpenRouterSyncSubmission(info *relaycommon.RelayInfo, metadataURL string, headers http.Header) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	return syncTasks().Configure(context.Background(), configure_sync_submission.Input{Task: task, MetadataURL: metadataURL, Headers: headers.Clone(), Transport: record.ToDomainTransport(info.ChannelSetting)})
}
func RecordOpenRouterSyncGeneration(info *relaycommon.RelayInfo, id string) error {
	identity := &relaycommon.OpenRouterCostState{}
	identity.ObserveGenerationID(id)
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	return syncTasks().Generation(context.Background(), record_sync_generation.Input{Task: task, GenerationID: id, APIKey: info.ApiKey, IdentityConflict: identity.IdentityConflict})
}
func RecordOpenRouterSyncSubmission(info *relaycommon.RelayInfo, resp *http.Response, requestErr error) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	input := record_sync_receipt.Input{Task: task, ResponsePresent: resp != nil, RequestError: requestErr != nil, APIKey: info.ApiKey,
		Observe: func(id string, conflict bool) {
			if conflict {
				info.OpenRouterCost.IdentityConflict = true
			}
			if id != "" {
				info.OpenRouterCost.GenerationID = id
			}
		},
	}
	if resp != nil {
		input.Status = resp.StatusCode
		input.RequestIDs = []string{resp.Header.Get("X-Request-Id"), resp.Header.Get("Request-Id"), resp.Header.Get(common.RequestIdKey)}
		input.GenerationID = resp.Header.Get("X-Generation-Id")
		if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) && input.GenerationID != "" {
			state := &relaycommon.OpenRouterCostState{}
			state.ObserveGenerationID(input.GenerationID)
			input.GenerationConflict = state.IdentityConflict
		}
	}
	return syncTasks().Receipt(context.Background(), input)
}
func syncCost(info *relaycommon.RelayInfo) domain.SyncCost {
	state := info.OpenRouterCost
	return domain.SyncCost{GenerationID: state.GenerationID, Final: state.Final, Valid: state.Valid, IdentityConflict: state.IdentityConflict, CostUSD: state.CostUSD, IsBYOK: state.IsBYOK, CostDetails: state.CostDetails}
}
func CompleteOpenRouterSyncTask(c *gin.Context, info *relaycommon.RelayInfo, body []byte, mimeType string) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	var ctx context.Context = context.Background()
	if c != nil {
		ctx = c
	}
	return syncTasks().Complete(ctx, complete_sync_task.Input{Task: task, Cost: syncCost(info), BodySize: int64(len(body)), MimeType: mimeType})
}
func FailOpenRouterSyncTask(info *relaycommon.RelayInfo, reason string) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	return syncTasks().Fail(context.Background(), fail_sync_task.Input{Task: task, Cost: syncCost(info), Reason: reason})
}

func HoldOpenRouterSyncTaskForReconciliation(info *relaycommon.RelayInfo, reason string) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	return syncTasks().Hold(context.Background(), hold_sync_task.Input{Task: task, IdentityConflict: info.OpenRouterCost.IdentityConflict, Reason: reason})
}
