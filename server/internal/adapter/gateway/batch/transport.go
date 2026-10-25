package batch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	billing "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/adapter/http/batchaccount"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/batch"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

func doBatchRequest(ctx context.Context, job *domain.Job, request batch.Request, body io.Reader, contentType string) (*http.Response, error) {
	channel, err := model.CacheGetChannel(job.ChannelID)
	if err != nil {
		return nil, err
	}
	hash, err := batchaccount.BatchChannelConfigHash(channel)
	if err != nil {
		return nil, err
	}
	if channel.Type != job.ChannelType || hash != job.ChannelConfigHash {
		return nil, errors.New("batch channel configuration changed; reconciliation required")
	}
	key := channel.Key
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		if job.CredentialIndex < 0 || job.CredentialIndex >= len(keys) {
			return nil, errors.New("batch credential unavailable")
		}
		key = keys[job.CredentialIndex]
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(key))) != job.CredentialHash {
		return nil, errors.New("batch credential changed; reconciliation required")
	}
	settings := channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, err
	}
	copy := *client
	copy.Timeout = 90 * time.Second
	// Never redirect credentials to returned result URLs or another host.
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	base := strings.TrimRight(channel.GetBaseURL(), "/")
	if base == "" {
		base = constant.GetChannelBaseURL(job.ChannelType)
	}
	path := request.Path
	if job.ChannelType == constant.ChannelTypeOpenRouter {
		path = strings.TrimPrefix(path, "/api")
	}
	if body == nil {
		body = bytes.NewReader(request.Body)
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, relaycommon.GetFullRequestURL(base, path, job.ChannelType), body)
	if err != nil {
		return nil, err
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	switch job.ChannelType {
	case constant.ChannelTypeAnthropic:
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		var snapshot billing.BatchBillingSnapshot
		if err := common.UnmarshalJsonStr(job.BillingSnapshot, &snapshot); err != nil {
			return nil, err
		}
		for name, value := range snapshot.Headers {
			req.Header.Set(name, value)
		}
	case constant.ChannelTypeGemini:
		req.Header.Set("x-goog-api-key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if channel.OpenAIOrganization != nil && *channel.OpenAIOrganization != "" {
		req.Header.Set("OpenAI-Organization", *channel.OpenAIOrganization)
	}
	if err := batchaccount.ValidateBatchChannelHeaders(channel); err != nil {
		return nil, err
	}
	for name, value := range channel.GetHeaderOverride() {
		req.Header.Set(name, strings.ReplaceAll(value.(string), "{api_key}", key))
	}
	return copy.Do(req)
}

func uploadOpenAIBatchInput(ctx context.Context, job *domain.Job, submission batch.Submission) (string, error) {
	var buffer bytes.Buffer
	form := multipart.NewWriter(&buffer)
	if err := form.WriteField("purpose", "batch"); err != nil {
		return "", err
	}
	file, err := form.CreateFormFile("file", job.PublicID+".jsonl")
	if err != nil {
		return "", err
	}
	if err := (batch.OpenAI{}).WriteInput(file, submission); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	if buffer.Len() > 32<<20 {
		return "", errors.New("OpenAI batch upload exceeds limit")
	}
	response, err := doBatchRequest(ctx, job, batch.Request{Method: http.MethodPost, Path: "/v1/files"}, &buffer, form.FormDataContentType())
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("batch file upload HTTP %d", response.StatusCode)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := common.DecodeJson(io.LimitReader(response.Body, 1<<20), &uploaded); err != nil {
		return "", err
	}
	if uploaded.ID == "" {
		return "", errors.New("batch upload returned no ID")
	}
	return uploaded.ID, nil
}

var batchFileID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func batchResultsRequest(job *domain.Job, fileID string) (batch.Request, error) {
	switch job.ChannelType {
	case constant.ChannelTypeOpenAI:
		if !batchFileID.MatchString(fileID) {
			return batch.Request{}, errors.New("invalid batch file ID")
		}
		return batch.Request{Method: http.MethodGet, Path: "/v1/files/" + fileID + "/content"}, nil
	case constant.ChannelTypeAnthropic:
		return (batch.Anthropic{}).Results(job.UpstreamID)
	case constant.ChannelTypeGemini:
		id, ok := strings.CutPrefix(fileID, "files/")
		if !ok || !batchFileID.MatchString(id) {
			return batch.Request{}, errors.New("invalid Google output file name")
		}
		return batch.Request{Method: http.MethodGet, Path: "/download/v1beta/files/" + id + ":download?alt=media"}, nil
	default:
		return batch.Request{}, batch.ErrUnsupported
	}
}
