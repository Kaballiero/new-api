package batch

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeSubmissionFormats(t *testing.T) {
	submission := Submission{ID: "local-batch", Model: "test-model", Endpoint: "/v1/chat/completions", Items: []Item{{CustomID: "item-1", Body: []byte(`{"temperature":0,"stream":false,"messages":[]}`)}}}
	t.Run("OpenRouter metadata precedes requests", func(t *testing.T) {
		request, err := (OpenRouter{}).Create(submission)
		require.NoError(t, err)
		assert.Equal(t, "/api/v1/batches", request.Path)
		assert.JSONEq(t, `{"endpoint":"/v1/chat/completions","model":"test-model","completion_window":"24h","requests":[{"custom_id":"item-1","body":{"temperature":0,"stream":false,"messages":[]}}]}`, string(request.Body))
		assert.Less(t, bytes.Index(request.Body, []byte(`"model"`)), bytes.Index(request.Body, []byte(`"requests"`)))
	})
	t.Run("OpenAI requires persisted upload", func(t *testing.T) {
		_, err := (OpenAI{}).Create(submission)
		require.Error(t, err)
		var file bytes.Buffer
		require.NoError(t, (OpenAI{}).WriteInput(&file, submission))
		assert.JSONEq(t, `{"custom_id":"item-1","method":"POST","url":"/v1/chat/completions","body":{"model":"test-model","temperature":0,"stream":false,"messages":[]}}`, file.String())
		submission.InputFileID = "file-123"
		request, err := (OpenAI{}).Create(submission)
		require.NoError(t, err)
		assert.JSONEq(t, `{"input_file_id":"file-123","endpoint":"/v1/chat/completions","completion_window":"24h"}`, string(request.Body))
	})
	t.Run("Anthropic native params", func(t *testing.T) {
		submission.Endpoint = "/v1/messages"
		request, err := (Anthropic{}).Create(submission)
		require.NoError(t, err)
		assert.JSONEq(t, `{"requests":[{"custom_id":"item-1","params":{"model":"test-model","temperature":0,"stream":false,"messages":[]}}]}`, string(request.Body))
		submission.Items[0].CustomID = "not valid"
		_, err = (Anthropic{}).Create(submission)
		require.Error(t, err)
	})
	t.Run("Google model URL and correlation metadata", func(t *testing.T) {
		submission.Model, submission.Endpoint = "gemini-2.5-flash", "/v1beta/generateContent"
		submission.Items = []Item{{CustomID: "item-1", Body: []byte(`{"contents":[],"generationConfig":{"temperature":0}}`)}}
		request, err := (Google{}).Create(submission)
		require.NoError(t, err)
		assert.Equal(t, "/v1beta/models/gemini-2.5-flash:batchGenerateContent", request.Path)
		assert.JSONEq(t, `{"batch":{"displayName":"local-batch","inputConfig":{"requests":{"requests":[{"request":{"contents":[],"generationConfig":{"temperature":0}},"metadata":{"custom_id":"item-1"}}]}}}}`, string(request.Body))
	})
}

func TestBatchResultsPreservePartialOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		adapter  Adapter
		status   string
		results  string
		state    State
		unbilled []bool
	}{
		{"OpenAI expired with successful output", OpenAI{}, `{"id":"batch_1","status":"expired","output_file_id":"file-success","error_file_id":"file-errors"}`, `{"custom_id":"good","response":{"status_code":200,"body":{"usage":{"prompt_tokens":5,"completion_tokens":1}}},"error":null}` + "\n" + `{"custom_id":"bad","response":null,"error":{"code":"batch_expired"}}`, Expired, []bool{false, true}},
		{"Anthropic ended with mixed results", Anthropic{}, `{"id":"msgbatch_1","processing_status":"ended","request_counts":{"succeeded":1,"errored":1}}`, `{"custom_id":"good","result":{"type":"succeeded","message":{"usage":{"input_tokens":5,"output_tokens":1}}}}` + "\n" + `{"custom_id":"bad","result":{"type":"errored","error":{"type":"invalid_request_error"}}}`, Completed, []bool{false, true}},
		{"Google output file preserves keys", Google{}, `{"name":"batches/1","metadata":{"state":"BATCH_STATE_SUCCEEDED"},"done":true,"response":{"responsesFile":"files/abc"}}`, `{"key":"good","response":{"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":1}}}` + "\n" + `{"key":"bad","error":{"code":3,"message":"invalid request"}}`, Completed, []bool{false, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := tc.adapter.ReadStatus(strings.NewReader(tc.status), func(Result) error { return errors.New("unexpected inline result") })
			require.NoError(t, err)
			assert.Equal(t, tc.state, snapshot.State)
			var results []Result
			err = tc.adapter.ReadResults(strings.NewReader(tc.results), func(result Result) error { results = append(results, result); return nil })
			require.NoError(t, err)
			require.Len(t, results, 2)
			assert.Equal(t, "good", results[0].CustomID)
			assert.Equal(t, "bad", results[1].CustomID)
			for i := range results {
				assert.Equal(t, tc.unbilled[i], results[i].Unbilled)
			}
			assert.NotEmpty(t, results[0].Body)
			assert.NotEmpty(t, results[1].Error)
		})
	}
}

func TestInlineBatchResultsAndWithheldUsage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter Adapter
		body    string
		state   State
	}{
		{"OpenRouter", OpenRouter{}, `{"id":"batch_1","status":"completed","results":[{"custom_id":"one","response":{"status_code":200,"body":{"usage":{"cost":0.001}}}}]}`, Completed},
		{"Google", Google{}, `{"name":"batches/1","metadata":{"state":"BATCH_STATE_SUCCEEDED"},"response":{"inlinedResponses":{"inlinedResponses":[{"metadata":{"custom_id":"one"},"response":{"usageMetadata":{"promptTokenCount":1}}}]}}}`, Completed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var results []Result
			snapshot, err := tc.adapter.ReadStatus(strings.NewReader(tc.body), func(result Result) error { results = append(results, result); return nil })
			require.NoError(t, err)
			assert.Equal(t, tc.state, snapshot.State)
			require.Len(t, results, 1)
			assert.Equal(t, "one", results[0].CustomID)
			assert.False(t, results[0].Unbilled)
		})
	}
	// A 402 retrieval can contain already-incurred cost with results withheld.
	snapshot, err := (OpenRouter{}).ReadStatus(strings.NewReader(`{"id":"batch_1","status":"completed","usage":{"cost":1.25},"results":null,"error":{"code":402}}`), func(Result) error { return errors.New("unexpected result") })
	require.NoError(t, err)
	assert.JSONEq(t, `{"cost":1.25}`, string(snapshot.Usage))
	assert.JSONEq(t, `{"code":402}`, string(snapshot.Error))
}

func TestBatchProtocolRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	for _, body := range []string{`{"model":"other"}`, `{"stream":true}`, `{"stream":"false"}`, `{"temperature":0,"temperature":1}`, `[]`} {
		_, err := (OpenRouter{}).Create(Submission{Model: "test", Items: []Item{{CustomID: "one", Body: []byte(body)}}})
		require.Error(t, err, body)
	}
	for _, adapter := range []Adapter{OpenRouter{}, OpenAI{}, Anthropic{}, Google{}} {
		_, err := adapter.Poll("https://attacker.example/steal")
		require.Error(t, err)
		_, err = adapter.ReadStatus(strings.NewReader(`{"id":"one","status":"unknown","name":"batches/1","metadata":{"state":"NEW_STATE"},"processing_status":"unknown"}`), func(Result) error { return nil })
		require.Error(t, err)
	}
	_, err := (OpenRouter{}).Cancel("batch_1")
	assert.ErrorIs(t, err, ErrUnsupported)
	_, err = (OpenAI{}).Delete("batch_1")
	assert.ErrorIs(t, err, ErrUnsupported)
	for _, adapter := range []Adapter{OpenAI{}, Anthropic{}, Google{}} {
		err := adapter.ReadResults(strings.NewReader(`{`), func(Result) error { return nil })
		require.Error(t, err)
	}
	err = (OpenAI{}).ReadResults(strings.NewReader(`{"response":{"status_code":200,"body":{}}}`), func(Result) error { return nil })
	require.Error(t, err)
	_, err = (OpenRouter{}).ReadStatus(strings.NewReader(`{"id":"one","status":"completed"} {}`), func(Result) error { return nil })
	require.Error(t, err)
}

func TestBatchResultLimitsAndConsumerFailure(t *testing.T) {
	// The aggregate stream exceeds one item limit, but each item is bounded.
	body, err := common.Marshal(map[string]any{"custom_id": "one", "response": map[string]any{"status_code": 200, "body": map[string]any{"text": strings.Repeat("x", MaxResultBytes/2)}}})
	require.NoError(t, err)
	reader := io.MultiReader(strings.NewReader(`{"id":"batch_1","status":"completed","results":[`), bytes.NewReader(body), strings.NewReader(","), bytes.NewReader(body), strings.NewReader("]}"))
	count := 0
	_, err = (OpenRouter{}).ReadStatus(reader, func(Result) error { count++; return nil })
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	tooLarge := `{"custom_id":"one","response":{"status_code":200,"body":{"text":"` + strings.Repeat("x", MaxResultBytes) + `"}}}`
	err = (OpenAI{}).ReadResults(strings.NewReader(tooLarge), func(Result) error { return nil })
	require.Error(t, err)
	_, err = (OpenRouter{}).ReadStatus(strings.NewReader(`{"id":"batch_1","status":"completed","results":[`+tooLarge+`]}`), func(Result) error { return nil })
	require.Error(t, err)
	want := errors.New("storage unavailable")
	// Decoder read-ahead from the preceding item cannot bypass the limit.
	reader = io.MultiReader(strings.NewReader(`{"id":"batch_1","status":"completed","results":[`), bytes.NewReader(body), strings.NewReader(","+tooLarge+"]}"))
	_, err = (OpenRouter{}).ReadStatus(reader, func(Result) error { return nil })
	require.Error(t, err)
	err = (OpenAI{}).ReadResults(bytes.NewReader(body), func(Result) error { return want })
	assert.ErrorIs(t, err, want)
}
