package fetch_upstream_ratios

import (
	"context"

	"fmt"

	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"errors"
)

type Channel struct {
	ID            int
	Name, BaseURL string
}
type Channels interface {
	ByIDs([]int) ([]Channel, error)
}
type Client interface {
	Fetch(context.Context, string, bool, int, string) ([]byte, func(), error)
}
type Clients interface{ NewClient() Client }
type Usecase struct {
	channels Channels
	clients  Clients
	local    func() map[string]any
	logError func(context.Context, string)
	logWarn  func(context.Context, string, ...any)
}

func NewUsecase(channels Channels, clients Clients, local func() map[string]any, logError func(context.Context, string), logWarn func(context.Context, string, ...any)) Usecase {
	return Usecase{channels: channels, clients: clients, local: local, logError: logError, logWarn: logWarn}
}

// Execute fetches the ratios of the requested upstreams and compares them with
// the local pricing. input.ErrorText renders the error of a failed upstream for
// its test result.
func (u Usecase) Execute(ctx context.Context, input Input) (Output, error) {
	req := input.Request
	if req.Timeout <= 0 {
		req.Timeout = 10
	}

	var upstreams []dto.UpstreamDTO

	if len(req.Upstreams) > 0 {
		for _, u := range req.Upstreams {
			if strings.HasPrefix(u.BaseURL, "http") {
				if u.Endpoint == "" {
					u.Endpoint = "/api/pricing"
				}
				u.BaseURL = strings.TrimRight(u.BaseURL, "/")
				upstreams = append(upstreams, u)
			}
		}
	} else if len(req.ChannelIDs) > 0 {
		intIds := make([]int, 0, len(req.ChannelIDs))
		for _, id64 := range req.ChannelIDs {
			intIds = append(intIds, int(id64))
		}
		dbChannels, err := u.channels.ByIDs(intIds)
		if err != nil {
			u.logError(ctx, "failed to query channels: "+err.Error())
			return Output{}, domain.ErrChannelQueryFailed
		}
		for _, ch := range dbChannels {
			if base := ch.BaseURL; strings.HasPrefix(base, "http") {
				upstreams = append(upstreams, dto.UpstreamDTO{
					ID:       ch.ID,
					Name:     ch.Name,
					BaseURL:  strings.TrimRight(base, "/"),
					Endpoint: "",
				})
			}
		}
	}

	if len(upstreams) == 0 {
		return Output{}, domain.ErrNoUpstreams
	}

	var wg sync.WaitGroup
	ch := make(chan domain.UpstreamResult, len(upstreams))

	sem := make(chan struct{}, 8)

	client := u.clients.NewClient()

	for _, chn := range upstreams {
		wg.Add(1)
		go func(chItem dto.UpstreamDTO) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			isOpenRouter := chItem.Endpoint == "openrouter"

			endpoint := chItem.Endpoint
			var fullURL string
			if isOpenRouter {
				fullURL = chItem.BaseURL + "/v1/models"
			} else if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
				fullURL = endpoint
			} else {
				if endpoint == "" {
					endpoint = "/api/pricing"
				} else if !strings.HasPrefix(endpoint, "/") {
					endpoint = "/" + endpoint
				}
				fullURL = chItem.BaseURL + endpoint
			}
			isModelsDev := domain.IsModelsDevAPIEndpoint(fullURL)

			uniqueName := chItem.Name
			if chItem.ID != 0 {
				uniqueName = fmt.Sprintf("%s(%d)", chItem.Name, chItem.ID)
			}

			ctx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
			defer cancel()

			bodyBytes, closeBody, err := client.Fetch(ctx, fullURL, isOpenRouter, chItem.ID, chItem.Name)
			if closeBody != nil {
				defer closeBody()
			}
			if err != nil {
				ch <- domain.UpstreamResult{Name: uniqueName, Err: err}
				return
			}
			converted, err := domain.DecodeRatioData(bodyBytes, isOpenRouter, isModelsDev, chItem.Name)
			if err != nil {
				var decodeErr *domain.RatioDecodeError
				if errors.As(err, &decodeErr) && decodeErr.Warning != "" {
					u.logWarn(ctx, decodeErr.Warning)
				}
				ch <- domain.UpstreamResult{Name: uniqueName, Err: err}
				return
			}
			ch <- domain.UpstreamResult{Name: uniqueName, Data: converted}

		}(chn)
	}

	wg.Wait()
	close(ch)

	localData := domain.EffectivePricingSyncData(u.local())

	var testResults []dto.TestResult
	var successfulChannels []domain.SyncChannel

	for r := range ch {
		// An upstream failure with an empty message (e.g. {"success":false}) has
		// always been reported as a success; keep that contract.
		if r.Err != nil && r.Err.Error() != "" {
			testResults = append(testResults, dto.TestResult{
				Name:   r.Name,
				Status: "error",
				Error:  input.ErrorText(r.Err),
			})
		} else {
			testResults = append(testResults, dto.TestResult{
				Name:   r.Name,
				Status: "success",
			})
			successfulChannels = append(successfulChannels, domain.SyncChannel{Name: r.Name, Data: domain.EffectivePricingSyncData(r.Data)})
		}
	}

	differences := domain.BuildDifferences(localData, successfulChannels)

	prices := make(map[string]ModelSyncPrices, len(differences))
	for name, fields := range differences {
		row := ModelSyncPrices{Current: domain.ModelPricingSyncValues(localData, name), Upstreams: make(map[string]map[string]any)}
		_, expressionPriority := fields["billing_expr"]
		for _, channel := range successfulChannels {
			candidate := domain.ModelPricingSyncValues(channel.Data, name)
			if expressionPriority && candidate["billing_mode"] != domain.BillingModeTieredExpr {
				continue
			}
			_, hasRatio := candidate["model_ratio"]
			_, hasPrice := candidate["model_price"]
			_, hasExpression := candidate["billing_expr"]
			if hasRatio || hasPrice || hasExpression {
				row.Upstreams[channel.Name] = candidate
			}
		}
		prices[name] = row
	}

	return Output{Differences: differences, Prices: prices, TestResults: testResults}, nil
}
