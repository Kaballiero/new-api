package channels

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/advancedcustom"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/samber/lo"
)

type OpenAIModel struct {
	ID         string         `json:"id"`
	Object     string         `json:"object"`
	Created    int64          `json:"created"`
	OwnedBy    string         `json:"owned_by"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Permission []struct {
		ID                 string `json:"id"`
		Object             string `json:"object"`
		Created            int64  `json:"created"`
		AllowCreateEngine  bool   `json:"allow_create_engine"`
		AllowSampling      bool   `json:"allow_sampling"`
		AllowLogprobs      bool   `json:"allow_logprobs"`
		AllowSearchIndices bool   `json:"allow_search_indices"`
		AllowView          bool   `json:"allow_view"`
		AllowFineTuning    bool   `json:"allow_fine_tuning"`
		Organization       string `json:"organization"`
		Group              string `json:"group"`
		IsBlocking         bool   `json:"is_blocking"`
	} `json:"permission"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type OpenAIModelsResponse struct {
	Data    []OpenAIModel `json:"data"`
	Success bool          `json:"success"`
}

func normalizeModelNames(models []string) []string {
	return lo.Uniq(lo.FilterMap(models, func(model string, _ int) (string, bool) {
		trimmed := strings.TrimSpace(model)
		return trimmed, trimmed != ""
	}))
}
func BuildFetchModelsHeaders(channel *model.Channel, key string) (http.Header, error) {
	var headers http.Header
	switch channel.Type {
	case constant.ChannelTypeAnthropic:
		headers = GetClaudeAuthHeader(key)
	default:
		headers = GetAuthHeader(key)
	}

	if err := ApplyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, err
	}
	return headers, nil
}
func ApplyFetchModelsHeaderOverrides(channel *model.Channel, key string, headers http.Header) error {
	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:          key,
			HeadersOverride: channel.GetHeaderOverride(),
		},
	}
	overrides, err := relaychannel.ResolveHeaderOverride(info, nil)
	if err != nil {
		return err
	}
	for name, value := range overrides {
		headers.Set(name, value)
	}

	return nil
}
func SelectChannelUpstreamModelDiscoveryKey(channel *model.Channel) (string, error) {
	if channel == nil {
		return "", fmt.Errorf("channel is nil")
	}
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, nil
	}

	keys := channel.GetKeys()
	if len(keys) == 0 {
		return "", fmt.Errorf("no keys available")
	}
	start := 0
	if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
		start = channel.ChannelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
	}
	for offset := 0; offset < len(keys); offset++ {
		index := (start + offset) % len(keys)
		status := common.ChannelStatusEnabled
		if configuredStatus, ok := channel.ChannelInfo.MultiKeyStatusList[index]; ok {
			status = configuredStatus
		}
		if status == common.ChannelStatusEnabled {
			return keys[index], nil
		}
	}
	return "", fmt.Errorf("no enabled keys")
}
func ParseOpenAIModelIDs(body []byte) ([]string, error) {
	var result struct {
		Data *[]OpenAIModel `json:"data"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid OpenAI Models response: %w", err)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("invalid OpenAI Models response: data is required")
	}
	ids := normalizeModelNames(lo.Map(*result.Data, func(item OpenAIModel, _ int) string {
		return item.ID
	}))
	if len(ids) == 0 {
		return nil, fmt.Errorf("OpenAI Models response contains no valid model IDs")
	}
	return ids, nil
}
func SanitizeFetchModelsError(err error, key string) error {
	if err == nil {
		return nil
	}

	// net/http includes the complete request URL in url.Error. Discovery routes
	// may put the API key in a custom query name or value, so never return that
	// wrapper to an API client.
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}

	message := err.Error()
	key = strings.TrimSpace(key)
	if key != "" {
		message = strings.ReplaceAll(message, key, "[REDACTED]")
		message = strings.ReplaceAll(message, url.QueryEscape(key), "[REDACTED]")
		message = strings.ReplaceAll(message, url.PathEscape(key), "[REDACTED]")
	}
	return errors.New(message)
}
func SanitizeAdvancedCustomRequestError(err error, key string, requestURL string) error {
	err = SanitizeFetchModelsError(err, key)
	if err == nil {
		return nil
	}
	parsedURL, parseErr := url.Parse(requestURL)
	if parseErr != nil {
		return err
	}
	message := err.Error()
	for _, value := range parsedURL.Query() {
		for _, secret := range value {
			if secret == "" {
				continue
			}
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
			message = strings.ReplaceAll(message, url.QueryEscape(secret), "[REDACTED]")
			message = strings.ReplaceAll(message, url.PathEscape(secret), "[REDACTED]")
		}
	}
	if key != "" {
		message = strings.ReplaceAll(message, key, "[REDACTED]")
		message = strings.ReplaceAll(message, url.QueryEscape(key), "[REDACTED]")
		message = strings.ReplaceAll(message, url.PathEscape(key), "[REDACTED]")
	}
	return errors.New(message)
}
func GetFetchModelsResponseBody(method string, requestURL string, channel *model.Channel, headers http.Header) ([]byte, error) {
	request, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
		if strings.EqualFold(name, "Host") {
			request.Host = headers.Get(name)
		}
	}
	client, err := httpclient.NewProxyHttpClient(repository.NewChannelPersistence().GetSetting(channel).Proxy)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}
func FetchChannelUpstreamModelIDs(channel *model.Channel, codexModels func(*model.Channel) ([]string, error)) ([]string, error) {
	if channel.Type == constant.ChannelTypeTaskPlugin {
		plugin, ok := jsplugin.DefaultRegistry.Get(repository.NewChannelPersistence().GetSetting(channel).TaskPluginKey)
		if !ok {
			return nil, fmt.Errorf("task plugin %q is not registered", repository.NewChannelPersistence().GetSetting(channel).TaskPluginKey)
		}
		return normalizeModelNames(plugin.Meta.Models), nil
	}
	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	if channel.Type == constant.ChannelTypeOllama {
		key := strings.TrimSpace(strings.Split(channel.Key, "\n")[0])
		models, err := ollama.FetchOllamaModels(baseURL, key)
		if err != nil {
			return nil, err
		}
		return normalizeModelNames(lo.Map(models, func(item ollama.OllamaModel, _ int) string {
			return item.Name
		})), nil
	}

	if channel.Type == constant.ChannelTypeGemini {
		key, keyErr := SelectChannelUpstreamModelDiscoveryKey(channel)
		if keyErr != nil {
			return nil, &domain.DetailError{Err: domain.ErrChannelKeyUnavailable, Cause: keyErr}
		}
		key = strings.TrimSpace(key)
		models, err := gemini.FetchGeminiModels(baseURL, key, repository.NewChannelPersistence().GetSetting(channel).Proxy)
		if err != nil {
			return nil, err
		}
		return normalizeModelNames(models), nil
	}

	if channel.Type == constant.ChannelTypeAdvancedCustom {
		return FetchAdvancedCustomUpstreamModelIDs(channel, baseURL)
	}

	if channel.Type == constant.ChannelTypeCodex {
		return codexModels(channel)
	}

	var url string
	switch channel.Type {
	case constant.ChannelTypeAli:
		url = fmt.Sprintf("%s/compatible-mode/v1/models", baseURL)
	case constant.ChannelTypeZhipu_v4:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/api/paas/v4/models", baseURL)
		}
	case constant.ChannelTypeVolcEngine:
		// The VolcEngine Ark OpenAI-compatible API is rooted at /api/v3 (chat and
		// embeddings are {base}/api/v3/..., see GetRequestURL in
		// relay/channel/volcengine) and has no /v1/models endpoint; the generic
		// path made model listing always fail with 404.
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/v1/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/api/v3/models", baseURL)
		}
	case constant.ChannelTypeMoonshot:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/v1/models", baseURL)
		}
	default:
		url = fmt.Sprintf("%s/v1/models", baseURL)
	}

	key, keyErr := SelectChannelUpstreamModelDiscoveryKey(channel)
	if keyErr != nil {
		return nil, &domain.DetailError{Err: domain.ErrChannelKeyUnavailable, Cause: keyErr}
	}
	key = strings.TrimSpace(key)

	headers, err := BuildFetchModelsHeaders(channel, key)
	if err != nil {
		return nil, SanitizeFetchModelsError(err, key)
	}

	body, err := GetFetchModelsResponseBody(http.MethodGet, url, channel, headers)
	if err != nil {
		return nil, SanitizeAdvancedCustomRequestError(err, key, url)
	}

	var result OpenAIModelsResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	ids := lo.Map(result.Data, func(item OpenAIModel, _ int) string {
		if channel.Type == constant.ChannelTypeGemini {
			return strings.TrimPrefix(item.ID, "models/")
		}
		return item.ID
	})
	return normalizeModelNames(ids), nil
}
func FetchAdvancedCustomUpstreamModelIDs(channel *model.Channel, baseURL string) ([]string, error) {
	key, keyErr := SelectChannelUpstreamModelDiscoveryKey(channel)
	if keyErr != nil {
		return nil, &domain.DetailError{Err: domain.ErrChannelKeyUnavailable, Cause: keyErr}
	}
	key = strings.TrimSpace(key)

	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAI,
		RelayMode:      relayconstant.RelayModeUnknown,
		RequestURLPath: dto.AdvancedCustomModelListPath,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeAdvancedCustom,
			ChannelBaseUrl:       baseURL,
			ApiKey:               key,
			ChannelOtherSettings: repository.NewChannelPersistence().GetOtherSettings(channel),
		},
	}

	adaptor := &advancedcustom.Adaptor{}
	url, headers, err := adaptor.BuildModelListRequest(info)
	if err != nil {
		return nil, SanitizeFetchModelsError(err, key)
	}
	if err := ApplyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, SanitizeFetchModelsError(err, key)
	}

	body, err := GetFetchModelsResponseBody(http.MethodGet, url, channel, headers)
	if err != nil {
		return nil, SanitizeFetchModelsError(err, key)
	}
	return ParseOpenAIModelIDs(body)
}

type ModelDiscovery struct {
	codexModels func(*model.Channel) ([]string, error)
}

func NewModelDiscovery(codexModels func(*model.Channel) ([]string, error)) ModelDiscovery {
	return ModelDiscovery{codexModels: codexModels}
}
func (discovery ModelDiscovery) Fetch(channel *domain.Channel) ([]string, error) {
	record := repository.FromDomain(channel)
	models, err := FetchChannelUpstreamModelIDs(record, discovery.codexModels)
	*channel = *repository.ToDomain(record)
	return models, err
}

func GetAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	return h
}
func GetClaudeAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("x-api-key", token)
	h.Add("anthropic-version", "2023-06-01")
	return h
}
