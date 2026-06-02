package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	validateModelsMaxModels       = 1000
	validateModelsConcurrency     = 8
	validateModelsPerModelTimeout = 20 * time.Second
)

// ValidateModelsRequest is the body of POST /api/channel/validate_models.
// Either ChannelId>0 (validate a saved channel) OR Type/Key/BaseURL (validate a
// not-yet-saved channel, mirroring FetchModels). When Models is empty and a
// ChannelId is given, the channel's full model list is validated.
type ValidateModelsRequest struct {
	ChannelId    int      `json:"channel_id"`
	Type         int      `json:"type"`
	Key          string   `json:"key"`
	BaseURL      string   `json:"base_url"`
	Models       []string `json:"models"`
	EndpointType string   `json:"endpoint_type"`
}

type ModelValidationResult struct {
	Model        string `json:"model"`
	OK           bool   `json:"ok"`
	Status       string `json:"status"` // alive | dead | uncertain
	UpstreamCode int    `json:"upstream_code"`
	ErrorCode    string `json:"error_code"`
	Message      string `json:"message"`
	LatencyMs    int64  `json:"latency_ms"`
}

type validateModelsSummary struct {
	Alive     int `json:"alive"`
	Dead      int `json:"dead"`
	Uncertain int `json:"uncertain"`
}

type ValidateModelsResponse struct {
	Results []ModelValidationResult `json:"results"`
	Summary validateModelsSummary   `json:"summary"`
}

// ValidateModels tests each requested model against the (saved or temporary)
// channel with a real request and classifies the outcome alive/dead/uncertain.
// It is report-only: it never disables a channel or a model.
func ValidateModels(c *gin.Context) {
	var req ValidateModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}

	channel, ok := resolveValidateChannel(c, &req)
	if !ok {
		return
	}

	models := req.Models
	if len(models) == 0 && req.ChannelId > 0 {
		models = channel.GetModels()
	}
	models = dedupeTrimModels(models)
	if len(models) == 0 {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "no models to validate")
		return
	}
	if len(models) > validateModelsMaxModels {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params",
			fmt.Sprintf("too many models: %d (max %d)", len(models), validateModelsMaxModels))
		return
	}

	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusBadRequest, "invalid_params", err)
		return
	}

	results := make([]ModelValidationResult, len(models))
	sem := make(chan struct{}, validateModelsConcurrency)
	var wg sync.WaitGroup
	for i, m := range models {
		wg.Add(1)
		sem <- struct{}{}
		gopool.Go(func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = validateOneModel(channel, testUserID, m, req.EndpointType)
		})
	}
	wg.Wait()

	resp := ValidateModelsResponse{Results: results}
	for _, r := range results {
		switch r.Status {
		case string(service.ModelAlive):
			resp.Summary.Alive++
		case string(service.ModelDead):
			resp.Summary.Dead++
		default:
			resp.Summary.Uncertain++
		}
	}
	common.ApiSuccess(c, resp)
}

// resolveValidateChannel returns the channel to validate against. For a saved
// channel it loads from cache/DB; otherwise it builds a temporary in-memory
// channel from the request (same shape as FetchModels). AutoBan is forced off as
// documentation that validation must never ban a real channel. Returns ok=false
// after writing an error response.
func resolveValidateChannel(c *gin.Context, req *ValidateModelsRequest) (*model.Channel, bool) {
	if req.ChannelId > 0 {
		channel, err := model.CacheGetChannel(req.ChannelId)
		if err == nil {
			return channel, true
		}
		channel, err = model.GetChannelById(req.ChannelId, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
			return nil, false
		}
		if err != nil {
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
			return nil, false
		}
		return channel, true
	}

	base := req.BaseURL
	if base == "" {
		base = constant.ChannelBaseURLs[req.Type]
	}
	key := strings.Split(strings.TrimSpace(req.Key), "\n")[0]
	noAutoBan := 0
	return &model.Channel{
		Type:    req.Type,
		Key:     key,
		BaseURL: &base,
		AutoBan: &noAutoBan,
	}, true
}

// runModelTest runs one testChannel with a hard timeout and maps it to a
// ModelValidationResult. It also returns the raw (unmasked) error message for
// endpoint inference, and whether the call timed out (classified uncertain so a
// slow upstream never marks a real model dead).
func runModelTest(channel *model.Channel, testUserID int, modelName, endpointType string) (ModelValidationResult, string, bool) {
	start := time.Now()
	done := make(chan testResult, 1)
	gopool.Go(func() {
		done <- testChannel(channel, testUserID, modelName, endpointType, false)
	})

	out := ModelValidationResult{Model: modelName}
	select {
	case res := <-done:
		out.LatencyMs = time.Since(start).Milliseconds()
		status, code := service.ClassifyModelValidation(res.localErr, res.newAPIError)
		out.Status = string(status)
		out.OK = status == service.ModelAlive
		out.UpstreamCode = code
		var raw string
		if res.newAPIError != nil {
			raw = res.newAPIError.Error()
			out.ErrorCode = string(res.newAPIError.GetErrorCode())
			out.Message = common.MaskSensitiveInfo(raw)
		} else if res.localErr != nil {
			raw = res.localErr.Error()
			out.Message = common.MaskSensitiveInfo(raw)
		}
		return out, raw, false
	case <-time.After(validateModelsPerModelTimeout):
		out.LatencyMs = time.Since(start).Milliseconds()
		out.Status = string(service.ModelUncertain)
		out.Message = "validation timed out"
		return out, "", true
	}
}

// validateOneModel tests a model and, when the first attempt is inconclusive
// because of a wrong-endpoint error, retries once on the endpoint named in the
// upstream error so a real model is reclassified alive/dead instead of staying
// uncertain. Only retries when the caller did not pin an endpoint.
func validateOneModel(channel *model.Channel, testUserID int, modelName, endpointType string) ModelValidationResult {
	out, raw, timedOut := runModelTest(channel, testUserID, modelName, endpointType)

	if !timedOut && endpointType == "" && out.Status == string(service.ModelUncertain) {
		if ep, ok := service.EndpointFromErrorText(raw); ok && string(ep) != "" {
			retry, _, retryTimedOut := runModelTest(channel, testUserID, modelName, string(ep))
			if !retryTimedOut {
				retry.LatencyMs += out.LatencyMs
				return retry
			}
		}
	}
	return out
}

// dedupeTrimModels trims, drops empties and de-duplicates while preserving order.
func dedupeTrimModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}
