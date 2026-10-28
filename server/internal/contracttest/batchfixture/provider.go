package batchfixture

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

// Provider serves declared native operations through the real client's RoundTripper seam.
type Provider struct {
	T         *testing.T
	Family    int
	Variant   string
	Transport Transport
	PaidPosts int
	Discovery int
	Polls     int
	Results   int
}

func NewProvider(t *testing.T, family int, variant string) *Provider {
	p := &Provider{T: t, Family: family, Variant: variant}
	p.Transport = Transport{T: t, Calls: []Outgoing{}, Handle: p.handle}
	return p
}

func (p *Provider) handle(request *http.Request, raw []byte) (int, string, error) {
	t := p.T
	switch p.Family {
	case constant.ChannelTypeGemini:
		require.Equal(t, ProviderKey, request.Header.Get("x-goog-api-key"))
	case constant.ChannelTypeAnthropic:
		require.Equal(t, ProviderKey, request.Header.Get("x-api-key"))
		require.NotEmpty(t, request.Header.Get("anthropic-version"))
	default:
		require.Equal(t, "Bearer "+ProviderKey, request.Header.Get("Authorization"))
	}
	path := strings.TrimPrefix(request.URL.Path, "/api")
	if strings.HasSuffix(path, "/endpoints") {
		require.Equal(t, "GET", request.Method)
		p.Discovery++
		if p.Variant == "discovery_denied" {
			return 401, `{}`, nil
		}
		return 200, `{"data":{"id":"test/model:batch","endpoints":[` +
			`{"tag":"cheap","max_completion_tokens":64,"pricing":{"prompt":"0.000001","completion":"0.000002"}},` +
			`{"tag":"expensive","max_completion_tokens":64,"pricing":{"prompt":"0.000002","completion":"0.000004"}}]}}`, nil
	}
	if path == "/v1/files" && request.Method == "POST" {
		if p.Variant == "upload_error" {
			return 500, `{}`, nil
		}
		require.Contains(t, string(raw), `"method":"POST"`)
		require.Contains(t, string(raw), `"model":"test/model"`)
		return 200, `{"id":"file_input"}`, nil
	}
	var job model.BatchJob
	require.NoError(t, model.DB.Order("id desc").First(&job).Error)
	paid := request.Method == "POST" && (strings.HasSuffix(path, "/batches") || strings.HasSuffix(path, ":batchGenerateContent"))
	if paid {
		p.PaidPosts++
		require.Equal(t, "submission_unknown", job.SubmitState, "ambiguity marker must commit before paid POST")
		require.Equal(t, "contract-worker", job.LeaseOwner)
		require.Positive(t, job.LeaseUntil)
		if p.Family == constant.ChannelTypeOpenAI {
			require.Equal(t, "file_input", job.InputFileID, "upload identity must persist before paid creation")
		}
		if strings.HasPrefix(p.Variant, "reject_") {
			var status int
			_, err := fmt.Sscanf(p.Variant, "reject_%d", &status)
			require.NoError(t, err)
			return status, `{"error":{"code":"invalid_request","message":"Bearer test-secret private prompt"}}`, nil
		}
		if p.Variant == "transport_error" {
			return 0, "", errors.New("fixture transport failure")
		}
		if p.Variant == "ambiguous" {
			return 502, `{}`, nil
		}
		if p.Variant == "bad_create_json" {
			return 202, `{`, nil
		}
		switch p.Family {
		case constant.ChannelTypeAnthropic:
			return 200, `{"id":"native_1","processing_status":"in_progress"}`, nil
		case constant.ChannelTypeGemini:
			return 200, `{"name":"batches/native_1","metadata":{"state":"BATCH_STATE_PENDING"}}`, nil
		default:
			if p.Variant == "id_then_bad_json" {
				return 202, `{"id":"native_1","status":"validating","results":[`, nil
			}
			return 202, `{"id":"native_1","status":"validating"}`, nil
		}
	}
	if request.Method == "DELETE" {
		return 204, "", nil
	}
	if strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, ":cancel") {
		if p.Variant == "cancel_error" {
			return 500, `{}`, nil
		}
		return 200, `{}`, nil
	}
	if strings.Contains(path, "/files/") || strings.HasSuffix(path, "/results") {
		p.Results++
		if p.Variant == "download_error" {
			return 503, `{}`, nil
		}
		return 200, p.resultLines(&job), nil
	}
	require.Equal(t, "GET", request.Method)
	require.True(t, strings.HasSuffix(path, "/native_1"), "undeclared resource %s", path)
	p.Polls++
	if p.Variant == "poll_error" {
		return 500, `{}`, nil
	}
	if p.Variant == "poll_bad_json" {
		return 200, `{`, nil
	}
	status := 200
	if p.Variant == "withheld" {
		status = 402
	}
	return status, p.status(&job), nil
}

func (p *Provider) resultLines(job *model.BatchJob) string {
	if p.Variant == "no_ids" {
		return ""
	}
	var items []model.BatchItem
	require.NoError(p.T, model.DB.Where("batch_id = ?", job.ID).Order("ordinal").Find(&items).Error)
	var out strings.Builder
	for i, item := range items {
		if p.Variant == "partial" && i > 0 {
			break
		}
		id := item.ProviderCustomID
		if p.Variant == "foreign_item" {
			id = "foreign_private_id"
		}
		body := `{"usage":{"prompt_tokens":2,"completion_tokens":1,"cost":0.000004}}`
		switch job.Endpoint {
		case "/v1/responses", "/v1/messages":
			body = `{"usage":{"input_tokens":2,"output_tokens":1,"cost":0.000004}}`
		case "/v1/embeddings":
			body = `{"usage":{"prompt_tokens":2,"total_tokens":2,"cost":0.000004}}`
		case "/v1beta/generateContent":
			body = `{"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`
		}
		if p.Variant == "missing_usage" {
			body = `{}`
		}
		var result string
		switch p.Family {
		case constant.ChannelTypeAnthropic:
			result = fmt.Sprintf(`{"custom_id":%q,"result":{"type":"succeeded","message":%s}}`, id, body)
			if p.Variant == "unbilled" {
				result = fmt.Sprintf(`{"custom_id":%q,"result":{"type":"expired"}}`, id)
			}
		case constant.ChannelTypeGemini:
			result = fmt.Sprintf(`{"key":%q,"response":%s}`, id, body)
		default:
			result = fmt.Sprintf(`{"custom_id":%q,"response":{"status_code":200,"body":%s}}`, id, body)
			if p.Variant == "unbilled" {
				result = fmt.Sprintf(`{"custom_id":%q,"error":{"code":"batch_expired"}}`, id)
			}
		}
		out.WriteString(result)
		out.WriteByte('\n')
	}
	if p.Variant == "malformed_suffix" {
		out.WriteString("{\n")
	}
	return out.String()
}

func (p *Provider) status(job *model.BatchJob) string {
	id := "native_1"
	if p.Variant == "foreign_job" {
		id = "foreign_1"
	}
	state := "completed"
	for _, candidate := range []string{"validating", "in_progress", "finalizing", "failed", "expired", "cancelled", "cancelling"} {
		if p.Variant == candidate {
			state = candidate
		}
	}
	results := strings.TrimSuffix(p.resultLines(job), "\n")
	results = strings.ReplaceAll(results, "\n", ",")
	switch p.Family {
	case constant.ChannelTypeAnthropic:
		state = "ended"
		if p.Variant == "in_progress" {
			state = "in_progress"
		}
		return fmt.Sprintf(`{"id":%q,"processing_status":%q}`, id, state)
	case constant.ChannelTypeGemini:
		state = "BATCH_STATE_SUCCEEDED"
		if p.Variant == "in_progress" {
			state = "BATCH_STATE_RUNNING"
		}
		if p.Variant == "file" || p.Variant == "download_error" {
			return fmt.Sprintf(`{"name":"batches/%s","metadata":{"state":%q},"response":{"responsesFile":"files/output"}}`, id, state)
		}
		// Inline Google uses metadata.custom_id, while downloaded JSONL also accepts key.
		return fmt.Sprintf(`{"name":"batches/%s","metadata":{"state":%q},"response":{"inlinedResponses":{"inlinedResponses":[%s]}}}`, id, state, results)
	case constant.ChannelTypeOpenRouter:
		counts := fmt.Sprintf(`{"total":%d,"completed":%d,"failed":0,"pending":0}`, job.RequestCount, job.RequestCount)
		if p.Variant == "bad_counts" {
			counts = `{"total":999,"completed":null}`
		}
		usage := `{"cost":0.000008,"prompt_tokens":4,"completion_tokens":2}`
		if p.Variant == "zero_cost" {
			usage = `{"cost":0}`
		}
		if p.Variant == "missing_cost" {
			usage = `{}`
		}
		if p.Variant == "null_cost" {
			usage = `{"cost":null}`
		}
		if p.Variant == "negative_cost" {
			usage = `{"cost":-1}`
		}
		if p.Variant == "withheld" {
			results = ""
		}
		return fmt.Sprintf(`{"id":%q,"status":%q,"request_counts":%s,"usage":%s,"results":[%s]}`, id, state, counts, usage, results)
	default:
		return fmt.Sprintf(`{"id":%q,"status":%q,"output_file_id":"file_output","error_file_id":null}`, id, state)
	}
}
