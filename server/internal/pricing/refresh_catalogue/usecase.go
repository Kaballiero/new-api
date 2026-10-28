package refresh_catalogue

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

const pricingCatalogWorkers = 5
const pricingCatalogFailureLimit = 20

type Channel interface {
	Value() *domain.BuildChannel
	Metadata(context.Context, string) ([]byte, error)
}
type Source struct {
	Channel     Channel
	Upstream    string
	Endpoint    constant.EndpointType
	Fingerprint string
}
type Store interface {
	Abilities() ([]domain.BuildAbility, error)
	Pricing() []domain.Pricing
	Channel(int) (Channel, error)
}
type Usecase struct {
	deps            domain.Capabilities
	store           Store
	source          func() (domain.CatalogSource, bool, error)
	sync            func(context.Context, domain.CatalogSource, map[string]bool) (domain.CatalogSyncSummary, error)
	publish         func(context.Context, []domain.PricingCatalogEntry) (int, error)
	normalizer      *domain.NativeNormalizer
	batchExpression func(string) (string, bool)
	now             func() time.Time
}

func NewUsecase(deps domain.Capabilities, store Store, source func() (domain.CatalogSource, bool, error), sync func(context.Context, domain.CatalogSource, map[string]bool) (domain.CatalogSyncSummary, error), publish func(context.Context, []domain.PricingCatalogEntry) (int, error), normalizer *domain.NativeNormalizer, batchExpression func(string) (string, bool), now func() time.Time) Usecase {
	return Usecase{deps: deps, store: store, source: source, sync: sync, publish: publish, normalizer: normalizer, batchExpression: batchExpression, now: now}
}
func (u Usecase) DiscoverSources(ctx context.Context) ([]Source, error) {
	abilities, err := u.store.Abilities()
	if err != nil {
		return nil, err
	}
	pricing := make(map[string]domain.Pricing)
	for _, item := range u.store.Pricing() {
		pricing[item.ModelName] = item
	}
	channels := make(map[int]Channel)
	imageModels := make(map[int]map[string]bool)
	var discoveryError error
	seen := make(map[string]bool)
	sources := []Source{}
	for _, ability := range abilities {

		if ability.ChannelType != constant.ChannelTypeOpenRouter {
			continue
		}
		channel, exists := channels[ability.ChannelId]
		if !exists {
			channel, err = u.store.Channel(ability.ChannelId)
			if err != nil {
				return nil, err
			}
			channels[ability.ChannelId] = channel
		}
		if channel.Value().Status != constant.ChannelStatusEnabled {
			continue
		}
		baseURL := channel.Value().GetBaseURL()
		if baseURL == "" {
			baseURL = constant.GetChannelBaseURL(channel.Value().Type)
		}
		if strings.TrimRight(baseURL, "/") != "https://openrouter.ai/api" {
			continue
		}
		if channel.Value().ParamOverride != nil && strings.TrimSpace(*channel.Value().ParamOverride) != "" && strings.TrimSpace(*channel.Value().ParamOverride) != "{}" {
			continue
		}
		upstream, resolved := domain.ResolvePricingUpstreamModel(u.deps, channel.Value(), ability.Model)
		if !resolved || strings.Contains(upstream, ":") && !strings.HasSuffix(upstream, ":batch") && !strings.HasSuffix(upstream, ":free") {
			continue
		}
		if _, loaded := imageModels[channel.Value().Id]; !loaded {
			imageModels[channel.Value().Id] = make(map[string]bool)
			body, fetchErr := channel.Metadata(ctx, "https://openrouter.ai/api/v1/images/models")
			var response struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if fetchErr == nil {
				fetchErr = u.deps.Codec.Unmarshal(body, &response)
			}
			if fetchErr != nil || response.Data == nil {
				discoveryError = errors.Join(discoveryError, errors.New("native image capability discovery failed"))
			} else {
				for _, item := range response.Data {
					imageModels[channel.Value().Id][item.ID] = true
				}
			}
		}
		appendSource := func(endpoint constant.EndpointType) {
			key := fmt.Sprintf("%d:%s:%s", channel.Value().Id, upstream, endpoint)
			if seen[key] {
				return
			}
			seen[key] = true
			sources = append(sources, Source{channel, upstream, endpoint, domain.PricingCatalogSourceFingerprint(u.deps, channel.Value(), upstream, endpoint)})
		}
		for endpoint, billingSources := range pricing[ability.Model].BillingSourcesForGroup(ability.Group) {
			if !slices.Contains(billingSources, domain.BillingSourceOpenRouterActualCost) || endpoint == constant.EndpointTypeOpenAIVideo {
				continue
			}
			if endpoint == domain.PricingEndpointBatch {
				expression, configured := u.batchExpression(ability.Model)
				if configured && expression != "" {
					appendSource(endpoint)
				}
				continue
			}
			if strings.HasSuffix(upstream, ":batch") {
				continue
			}
			if !slices.Contains(pricing[ability.Model].SupportedEndpointTypes, endpoint) {
				continue
			}
			if endpoint == constant.EndpointTypeImageGeneration {
				if imageModels[channel.Value().Id][upstream] {
					appendSource(endpoint)
				}
				continue
			}
			operation, _ := domain.ConsumerOperation(endpoint)
			if operation == "" {
				continue
			}
			appendSource(endpoint)
		}
		if imageModels[channel.Value().Id][upstream] {
			appendSource(constant.EndpointTypeImageGeneration)
		}

	}
	return sources, discoveryError
}
func (u Usecase) Execute(ctx context.Context) (domain.CatalogRefreshSummary, error) {
	source, enabled, err := u.source()
	if err != nil {
		return domain.CatalogRefreshSummary{}, err
	}
	if enabled {
		return u.RefreshUpstream(ctx, source)
	}
	sources, discoveryErr := u.DiscoverSources(ctx)
	summary, refreshErr := u.RefreshSources(ctx, sources, u.FetchSource, func(entries []domain.PricingCatalogEntry) (int, error) {
		return u.publish(ctx, entries)
	})
	if discoveryErr != nil {
		summary.Failed++
	}
	return summary, errors.Join(discoveryErr, refreshErr)
}
func (u Usecase) RefreshSources(ctx context.Context, sources []Source, fetch func(context.Context, Source) (domain.PricingCatalogEntry, error), publish func([]domain.PricingCatalogEntry) (int, error)) (domain.CatalogRefreshSummary, error) {
	summary := domain.CatalogRefreshSummary{Sources: len(sources)}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	jobs := make(chan Source)
	var workers sync.WaitGroup
	var resultLock sync.Mutex
	entries := []domain.PricingCatalogEntry{}
	for range pricingCatalogWorkers {
		workers.Go(func() {
			for source := range jobs {
				failureClass := "metadata_failed"
				entry, err := fetch(ctx, source)

				resultLock.Lock()
				if err != nil || ctx.Err() != nil {
					summary.Failed++
					if ctx.Err() != nil {
						failureClass = "cancelled"
					} else if failureClass == "metadata_failed" {
						if strings.Contains(err.Error(), "override") || strings.Contains(err.Error(), "threshold") {
							failureClass = "pricing_override_invalid"
						}
						if strings.Contains(err.Error(), "identity") || strings.Contains(err.Error(), "endpoint pricing response") {
							failureClass = "metadata_identity_or_schema_invalid"
						}
					}
					channelID := 0
					if source.Channel != nil {
						channelID = source.Channel.Value().Id
					}
					upstream := source.Upstream
					if len(upstream) > 256 {
						upstream = upstream[:256]
					}
					summary.FailedSources = append(summary.FailedSources, domain.CatalogRefreshFailure{ChannelID: channelID, UpstreamModel: upstream, Endpoint: source.Endpoint, Class: failureClass})
				} else {
					entries = append(entries, entry)
				}
				resultLock.Unlock()
			}
		})
	}
enqueue:
	for _, source := range sources {
		select {
		case jobs <- source:
		case <-ctx.Done():
			break enqueue
		}
	}
	close(jobs)
	workers.Wait()
	slices.SortFunc(summary.FailedSources, func(left, right domain.CatalogRefreshFailure) int {
		if left.ChannelID != right.ChannelID {
			return left.ChannelID - right.ChannelID
		}
		if order := strings.Compare(left.UpstreamModel, right.UpstreamModel); order != 0 {
			return order
		}
		return strings.Compare(string(left.Endpoint), string(right.Endpoint))
	})
	if len(summary.FailedSources) > pricingCatalogFailureLimit {
		summary.FailuresTruncated = true
		summary.FailedSources = summary.FailedSources[:pricingCatalogFailureLimit]
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if len(entries) > 0 {
		published, err := publish(entries)
		if err != nil {
			summary.Failed += len(entries)
			for _, entry := range entries {
				if len(summary.FailedSources) < pricingCatalogFailureLimit {
					summary.FailedSources = append(summary.FailedSources, domain.CatalogRefreshFailure{ChannelID: entry.ChannelID, UpstreamModel: entry.UpstreamModel, Endpoint: entry.Endpoint, Class: "publication_failed"})
				}
			}
			return summary, fmt.Errorf("pricing catalog refresh failed for %d of %d sources: %w", summary.Failed, summary.Sources, err)
		}
		summary.Published = published
		summary.Failed += len(entries) - published
	}
	if summary.Failed > 0 {
		return summary, fmt.Errorf("pricing catalog refresh failed for %d of %d sources", summary.Failed, summary.Sources)
	}
	return summary, nil
}
func (u Usecase) FetchSource(ctx context.Context, source Source) (domain.PricingCatalogEntry, error) {
	entry := domain.PricingCatalogEntry{}
	discoveryModel := source.Upstream
	if source.Endpoint == domain.PricingEndpointBatch {
		discoveryModel = strings.TrimSuffix(source.Upstream, ":batch") + ":batch"
	}
	parts := strings.Split(discoveryModel, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return entry, errors.New("invalid native pricing model identity")
	}
	path := "/v1/models/"
	if source.Endpoint == constant.EndpointTypeImageGeneration {
		path = "/v1/images/models/"
	}
	endpointURL := "https://openrouter.ai/api" + path + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/endpoints"
	body, err := source.Channel.Metadata(ctx, endpointURL)
	if err != nil {
		return entry, err
	}
	var offers []domain.CatalogOffer
	if source.Endpoint == constant.EndpointTypeImageGeneration {
		offers, err = u.normalizer.NormalizeImagePricing(source.Upstream, body)
		metadata, decodeErr := u.normalizer.DecodeImageMetadata(body)
		if decodeErr == nil && (len(metadata.Endpoints) == 0 && source.Upstream == "meta/muse-image" || slices.ContainsFunc(metadata.Endpoints, func(endpoint domain.NativeImageEndpoint) bool {
			return endpoint.ProviderTag == "krea" && len(endpoint.Pricing) == 0
		})) {
			endpointURL = "https://openrouter.ai/" + source.Upstream
		}

	} else {
		operation, main := domain.ConsumerOperation(source.Endpoint)
		offers, err = u.normalizer.NormalizeEndpointPricing(discoveryModel, operation, main, body)
		if source.Endpoint == domain.PricingEndpointBatch {
			for index := range offers {
				offers[index].Mode = "batch"
			}
		}
	}
	if err != nil {
		return entry, err
	}
	return domain.PricingCatalogEntry{ChannelID: source.Channel.Value().Id, UpstreamModel: source.Upstream, Endpoint: source.Endpoint, Source: endpointURL, FetchedAt: u.now().UTC().Format(time.RFC3339Nano), Fingerprint: source.Fingerprint, Offers: domain.SelectPricingCatalogOffers(offers)}, nil
}

// refreshUpstreamCatalog uses the existing system-task lifecycle. Selection and
// channel policy remain NewAPI-owned; the upstream supplies only model facts.
func (u Usecase) RefreshUpstream(ctx context.Context, source domain.CatalogSource) (domain.CatalogRefreshSummary, error) {
	summary := domain.CatalogRefreshSummary{}
	abilities, err := u.store.Abilities()
	if err != nil {
		return summary, err
	}
	channels := map[int]Channel{}
	requested := map[string]bool{}
	for _, ability := range abilities {
		if ability.ChannelType != constant.ChannelTypeOpenRouter {
			continue
		}
		channel, exists := channels[ability.ChannelId]
		if !exists {
			channel, err = u.store.Channel(ability.ChannelId)
			if err != nil {
				return summary, err
			}
			channels[ability.ChannelId] = channel
		}
		if channel.Value().Status != constant.ChannelStatusEnabled || !domain.IsOpenRouterCatalogChannel(channel.Value()) {
			continue
		}
		upstream, resolved := domain.ResolvePricingUpstreamModel(u.deps, channel.Value(), ability.Model)
		if !resolved {
			return summary, fmt.Errorf("catalog channel model mapping is unresolved")
		}
		requested[upstream] = true
	}
	summary.Sources = len(requested)
	if len(requested) == 0 {
		return summary, nil
	}
	result, err := u.sync(ctx, source, requested)
	if err != nil {
		return summary, err
	}
	summary.Published = result.ImportedModels
	summary.MissingModels = result.MissingModels
	summary.NewDocuments = result.Publication.NewDocuments
	summary.ReusedDocuments = result.Publication.ReusedDocuments
	summary.Revision = result.Publication.Revision
	return summary, nil
}
