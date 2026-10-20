package validate_models

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/bytedance/gopkg/util/gopool"
	"strings"
	"sync"
	"time"
)

const (
	validateModelsMaxModels       = 1000
	validateModelsConcurrency     = 8
	validateModelsPerModelTimeout = 20 * time.Second
)

type Store interface {
	Cached(int) (*domain.Channel, error)
	Get(int, bool) (*domain.Channel, error)
	IsMissing(error) bool
}
type Usecase struct {
	store    Store
	probe    domain.Probe
	user     func() (int, error)
	classify func(error, *types.NewAPIError) (domain.ModelLiveness, int)
}

func NewUsecase(store Store, probe domain.Probe, user func() (int, error), classify func(error, *types.NewAPIError) (domain.ModelLiveness, int)) Usecase {
	return Usecase{store: store, probe: probe, user: user, classify: classify}
}
func (usecase Usecase) Execute(req ValidateModelsRequest) (ValidateModelsResponse, *domain.Failure) {
	channel, failure := usecase.Resolve(&req)
	if failure != nil {
		return ValidateModelsResponse{}, failure
	}

	models := req.Models
	if len(models) == 0 && req.ChannelId > 0 {
		models = channel.GetModels()
	}
	models = NormalizeModels(models)
	if len(models) == 0 {
		return ValidateModelsResponse{}, &domain.Failure{Kind: "invalid", Message: "no models to validate"}
	}
	if len(models) > validateModelsMaxModels {
		return ValidateModelsResponse{}, &domain.Failure{Kind: "invalid", Message: fmt.Sprintf("too many models: %d (max %d)", len(models), validateModelsMaxModels)}
	}

	testUserID, err := usecase.user()
	if err != nil {
		return ValidateModelsResponse{}, &domain.Failure{Kind: "actor", Cause: err}
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
			results[i] = usecase.ValidateOne(channel, testUserID, m, req.EndpointType)
		})
	}
	wg.Wait()

	resp := ValidateModelsResponse{Results: results}
	for _, r := range results {
		switch r.Status {
		case string(domain.ModelAlive):
			resp.Summary.Alive++
		case string(domain.ModelDead):
			resp.Summary.Dead++
		default:
			resp.Summary.Uncertain++
		}
	}
	return resp, nil
}
func (usecase Usecase) Resolve(req *ValidateModelsRequest) (*domain.Channel, *domain.Failure) {
	if req.ChannelId > 0 {
		channel, err := usecase.store.Cached(req.ChannelId)
		if err == nil {
			return channel, nil
		}
		channel, err = usecase.store.Get(req.ChannelId, true)
		if usecase.store.IsMissing(err) {
			return nil, &domain.Failure{Kind: "missing"}
		}
		if err != nil {
			return nil, &domain.Failure{Kind: "internal", Cause: err}
		}
		return channel, nil
	}

	base := req.BaseURL
	if base == "" {
		base = constant.ChannelBaseURLs[req.Type]
	}
	key := strings.Split(strings.TrimSpace(req.Key), "\n")[0]
	noAutoBan := 0
	return &domain.Channel{
		Type:    req.Type,
		Key:     key,
		BaseURL: &base,
		AutoBan: &noAutoBan,
	}, nil
}
func (usecase Usecase) ValidateOne(channel *domain.Channel, testUserID int, modelName, endpointType string) ModelValidationResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), validateModelsPerModelTimeout)
	defer cancel()
	done := make(chan domain.ProbeOutcome, 1)
	gopool.Go(func() {
		done <- usecase.probe.Test(ctx, channel, testUserID, modelName, endpointType, false)
	})

	out := ModelValidationResult{Model: modelName}
	select {
	case res := <-done:
		out.LatencyMs = time.Since(start).Milliseconds()
		status, code := usecase.classify(res.LocalError(), res.APIError())
		out.Status = string(status)
		out.OK = status == domain.ModelAlive
		out.UpstreamCode = code
		if res.APIError() != nil {
			out.ErrorCode = string(res.APIError().GetErrorCode())
			out.Message = common.MaskSensitiveInfo(res.APIError().Error())
		} else if res.LocalError() != nil {
			out.Message = common.MaskSensitiveInfo(res.LocalError().Error())
		}
	case <-time.After(validateModelsPerModelTimeout):
		out.LatencyMs = time.Since(start).Milliseconds()
		out.Status = string(domain.ModelUncertain)
		out.Message = "validation timed out"
	}
	return out
}
func NormalizeModels(models []string) []string {
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
