package relay

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	mapping "github.com/QuantumNous/new-api/gateway/mapping"
	"github.com/QuantumNous/new-api/logger"
	billinggateway "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// SystemOneHelper relays native synchronous decisions. Transport, reservation and
// authoritative cost settlement remain owned by the existing relay mechanisms;
// a Task that took over a deferred cost settles it from generation metadata.
func SystemOneHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	adaptor := GetAdaptor(info.ApiType)
	capability, supported := adaptor.(interface {
		SupportsSystemOne(*relaycommon.RelayInfo) bool
		ReadSystemOneResponse(*gin.Context, *http.Response, *relaycommon.RelayInfo) ([]byte, *types.NewAPIError)
	})
	if !supported || !capability.SupportsSystemOne(info) {
		return types.NewErrorWithStatusCode(errors.New("channel does not support System One"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	original, ok := info.Request.(*dto.SystemOneRequest)
	if !ok {
		return systemOneResponseError("invalid System One request")
	}
	raw, err := common.Marshal(original)
	if err != nil {
		return systemOneResponseError("invalid System One request")
	}
	var request dto.SystemOneRequest
	if common.Unmarshal(raw, &request) != nil {
		return systemOneResponseError("invalid System One request")
	}
	if err = helper.ModelMappedHelper(c, info, &request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	raw, err = common.Marshal(request)
	if err != nil {
		return systemOneResponseError("invalid System One request")
	}
	// Advanced operations may decode/reencode arbitrary objects. Until their
	// order semantics are defined for System One, admit only scalar/routing
	// overrides; untouched state/questions keep their original byte order.
	for key := range info.ParamOverride {
		switch key {
		case "model", "provider", "session_id", "trace", "user":
		default:
			return types.NewErrorWithStatusCode(errors.New("unsupported System One parameter override: "+key), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	if len(info.ParamOverride) > 0 {
		raw, err = relaycommon.ApplyParamOverrideWithRelayInfo(raw, info)
		if err != nil {
			return newAPIErrorFromParamOverride(err)
		}
	}
	if err = common.Unmarshal(raw, &request); err == nil {
		err = request.Validate()
	}
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	info.UpstreamModelName = request.Model
	adaptor.Init(info)
	response, err := adaptor.DoRequest(c, info, bytes.NewReader(raw))
	if err != nil {
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}
	resp, ok := response.(*http.Response)
	if !ok || resp == nil {
		return systemOneResponseError("System One response missing")
	}
	if resp.StatusCode != http.StatusOK {
		return mapping.RelayErrorHandler(c.Request.Context(), resp, false)
	}
	defer func() {
		// Nothing reached the client; the generation lookup charges what
		// OpenRouter billed.
		if newAPIError != nil && relaycommon.HasOpenRouterSyncTask(info) {
			if failErr := taskrecord.FailOpenRouterSyncTask(info, "undelivered_chat_result"); failErr != nil {
				logger.LogError(c, fmt.Sprintf("OpenRouter undelivered decisions task %s requires reconciliation: %v", info.OpenRouterCost.TaskID, failErr))
			}
		}
	}()
	body, responseErr := capability.ReadSystemOneResponse(c, resp, info)
	if responseErr != nil {
		return responseErr
	}
	usage, typedErr := request.ValidateResponse(body)
	if usage == nil {
		usage = &dto.Usage{}
	}
	// A valid authoritative cost must settle even if the typed result is broken.
	// Settle before writing so a disconnected client cannot bypass payment; a
	// Task that owns the hold skips this settlement.
	billinggateway.PostTextConsumeQuota(c, info, usage, nil)
	if typedErr != nil {
		return systemOneResponseError(typedErr.Error())
	}
	if relaycommon.HasOpenRouterSyncTask(info) {
		// The Task retains the hold, so the response is still delivered; an
		// unfinished reservation is parked in manual reconciliation by the poller.
		if completeErr := taskrecord.CompleteOpenRouterSyncTask(c, info, body, "application/json"); completeErr != nil {
			failErr := taskrecord.FailOpenRouterSyncTask(info, "chat_completion_unavailable")
			logger.LogError(c, fmt.Sprintf("OpenRouter decisions task %s requires reconciliation: complete=%v fail=%v", info.OpenRouterCost.TaskID, completeErr, failErr))
		}
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		contentType = "application/json"
	}
	c.Data(http.StatusOK, contentType, body)
	return nil
}

func systemOneResponseError(reason string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}
