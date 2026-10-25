package batchruntime

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	math "github.com/QuantumNous/new-api/internal/adapter/billing"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/batch"
	core "github.com/QuantumNous/new-api/internal/app/batch"
	systemcore "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/batch/cancel_batch"
	"github.com/QuantumNous/new-api/internal/batch/confirm_batch_purchase"
	"github.com/QuantumNous/new-api/internal/batch/create_batch"
	"github.com/QuantumNous/new-api/internal/batch/delete_batch"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/fetch_batch_results"
	"github.com/QuantumNous/new-api/internal/batch/get_own_batch"
	"github.com/QuantumNous/new-api/internal/batch/list_own_batches"
	"github.com/QuantumNous/new-api/internal/batch/poll_batches"
	"github.com/QuantumNous/new-api/internal/batch/prepare_batch"
	"github.com/QuantumNous/new-api/internal/batch/recover_batch_identity"
	systemdomain "github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func kinds() domain.ChannelKinds {
	return domain.ChannelKinds{OpenAI: constant.ChannelTypeOpenAI, Anthropic: constant.ChannelTypeAnthropic,
		Google: constant.ChannelTypeGemini, OpenRouter: constant.ChannelTypeOpenRouter}
}

type Runtime struct {
	Poll     poll_batches.Usecase
	Recovery recover_batch_identity.Handler
	Prepare  prepare_batch.Handler
	Create   create_batch.Handler
	Get      get_own_batch.Handler
	List     list_own_batches.Handler
	Results  fetch_batch_results.Handler
	Cancel   cancel_batch.Handler
	Delete   delete_batch.Handler
	Purchase confirm_batch_purchase.Handler
}

var Default = sync.OnceValue(New)

func New() Runtime {
	store := core.NewStore()
	native := gateway.Gateway{}
	recovery := recover_batch_identity.NewUsecase(store, native, kinds(), common.GetRandomString)
	return Runtime{
		Poll: core.NewPolling(native, kinds()), Recovery: recover_batch_identity.NewHandler(recovery),
		Prepare: prepare_batch.NewHandler(prepare_batch.NewUsecase(store, gateway.Validation{},
			math.BatchCodec{}, common.GetRandomString), func(c *gin.Context, endpoint string) {
			service.GetChannelConstraints(c).AddFilter(hostdto.ChannelFilter{Kind: hostdto.FilterBatchEndpoint, BatchEndpoint: endpoint})
		}),
		Create: create_batch.NewHandler(core.NewCreation(gateway.AdmissionGateway{}, gateway.Validation{})),
		Get:    get_own_batch.NewHandler(store), List: list_own_batches.NewHandler(list_own_batches.NewUsecase(store)),
		Results: fetch_batch_results.NewHandler(fetch_batch_results.NewUsecase(store, time.Now)),

		Cancel: cancel_batch.NewHandler(cancel_batch.NewUsecase(store, native)), Delete: delete_batch.NewHandler(delete_batch.NewUsecase(store,
			time.Now)), Purchase: confirm_batch_purchase.NewHandler(confirm_batch_purchase.NewUsecase(store)),
	}
}

// SystemHandler is registered through controller.RegisterScheduledSystemTasks in every scheduler binary.
type SystemHandler struct{ poll poll_batches.Usecase }

func NewSystemHandler(poll poll_batches.Usecase) SystemHandler { return SystemHandler{poll} }
func (SystemHandler) Type() string                             { return "provider_batch_poll" }
func (SystemHandler) Enabled() bool                            { return constant.UpdateTask }
func (SystemHandler) Interval() time.Duration                  { return 15 * time.Second }
func (SystemHandler) NewPayload() any                          { return nil }
func (h SystemHandler) Run(ctx context.Context, task *systemdomain.SystemTask, runnerID string) {
	if err := h.poll.Execute(ctx, runnerID); err != nil {
		systemcore.FailSystemTask(task, runnerID, err)
		return
	}
	if err := systemcore.Default().Store.FinishSystemTask(task.TaskID, runnerID, systemdomain.SystemTaskStatusSucceeded, nil, ""); err != nil {
		common.SysError("failed to finish Batch poll: " + err.Error())
	}
}
