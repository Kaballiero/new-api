package tasksync

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
)

type Metadata struct{}

func (Metadata) Generation(ctx context.Context, task *domain.Task, endpoint string) (domain.SyncMetadata, int, time.Duration, string) {
	body, status, retry, class := fetchMetadata(ctx, task, endpoint)
	if class != "" {
		return domain.SyncMetadata{}, status, retry, class
	}
	var result struct {
		Data struct {
			ID             string          `json:"id"`
			Model          string          `json:"model"`
			APIType        string          `json:"api_type"`
			Cancelled      *bool           `json:"cancelled"`
			GenerationTime *float64        `json:"generation_time"`
			TotalCost      json.RawMessage `json:"total_cost"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if common.Unmarshal(body, &result) != nil {
		return domain.SyncMetadata{}, status, 0, "identity_or_invalid_cost"
	}
	d := result.Data
	raw := bytes.TrimSpace(d.TotalCost)
	out := domain.SyncMetadata{ID: d.ID, Model: d.Model, APIType: d.APIType, Cancelled: d.Cancelled, GenerationTime: d.GenerationTime,
		ErrorPresent: len(result.Error) > 0 && string(result.Error) != "null", CostPresent: len(raw) > 0 && !bytes.Equal(raw, []byte("null"))}
	if out.CostPresent {
		cost, err := common.ValidateOpenRouterCost(raw)
		out.CostUSD, out.CostValid = cost, err == nil
	}
	return out, status, retry, ""
}
func (Metadata) Model(ctx context.Context, task *domain.Task, endpoint string) (domain.SyncModelMetadata, int, time.Duration, string) {
	body, status, retry, class := fetchMetadata(ctx, task, endpoint)
	if class != "" {
		return domain.SyncModelMetadata{}, status, retry, class
	}
	var result struct {
		Data struct {
			ID            string `json:"id"`
			CanonicalSlug string `json:"canonical_slug"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if common.Unmarshal(body, &result) != nil {
		return domain.SyncModelMetadata{}, status, 0, "identity_or_invalid_cost"
	}
	return domain.SyncModelMetadata{ID: result.Data.ID, CanonicalSlug: result.Data.CanonicalSlug, ErrorPresent: len(result.Error) > 0 && string(result.Error) != "null"}, status, retry, ""
}

func fetchMetadata(ctx context.Context, task *domain.Task, endpoint string) ([]byte, int, time.Duration, string) {
	a := task.PrivateData.OpenRouterSyncState()
	settings, headers := record.FromDomainTransport(a.Transport), http.Header(a.Headers).Clone()
	if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) {
		channel, key, err := chatAccount(task)
		if err != nil {
			return nil, 0, 0, "account_binding_changed"
		}
		settings = channel.GetSetting()
		headers = http.Header{"Authorization": []string{"Bearer " + key}}
		for name, value := range channel.GetHeaderOverride() {
			headers.Set(name, strings.ReplaceAll(value.(string), "{api_key}", key))
		}
	}
	client, err := httpclient.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, 0, 0, "transport_unavailable"
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, "permanent_http_error"
	}
	req.Header = headers
	req.Header.Del("Content-Length")
	req.Header.Del("Content-Type")
	req.Host = req.Header.Get("Host")
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return nil, 0, 0, "transport_error"
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	var retryAfter time.Duration
	if seconds, err := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
		retryAfter = time.Duration(seconds) * time.Second
	} else if until, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		retryAfter = max(time.Until(until), 0)
	}
	if status == 401 || status == 403 {
		return nil, status, 0, "auth"
	}
	if status == 404 || status == 429 || status >= 500 {
		return nil, status, retryAfter, "temporary_http_error"
	}
	if status != 200 {
		return nil, status, 0, "permanent_http_error"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return nil, status, retryAfter, "transport_error"
	}
	if len(body) > 2<<20 {
		return nil, status, 0, "identity_or_invalid_cost"
	}
	return body, status, retryAfter, ""
}
