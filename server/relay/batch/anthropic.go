package batch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
)

type Anthropic struct{}

func (Anthropic) Create(submission Submission) (Request, error) {
	if submission.Endpoint != "/v1/messages" {
		return Request{}, errors.New("Anthropic batch supports /v1/messages")
	}
	type request struct {
		CustomID string          `json:"custom_id"`
		Params   json.RawMessage `json:"params"`
	}
	requests := make([]request, len(submission.Items))
	for i, item := range submission.Items {
		if !anthropicCustomID.MatchString(item.CustomID) {
			return Request{}, fmt.Errorf("invalid Anthropic custom_id %q", item.CustomID)
		}
		body, err := requestBody(item, submission.Model, true)
		if err != nil {
			return Request{}, err
		}
		requests[i] = request{item.CustomID, body}
	}
	return marshalRequest("/v1/messages/batches", struct {
		Requests []request `json:"requests"`
	}{requests})
}

func (Anthropic) Poll(id string) (Request, error) {
	return resourceRequest(http.MethodGet, "/v1/messages/batches/", id, "")
}
func (Anthropic) Cancel(id string) (Request, error) {
	return resourceRequest(http.MethodPost, "/v1/messages/batches/", id, "/cancel")
}
func (Anthropic) Delete(id string) (Request, error) {
	return resourceRequest(http.MethodDelete, "/v1/messages/batches/", id, "")
}
func (Anthropic) Results(id string) (Request, error) {
	return resourceRequest(http.MethodGet, "/v1/messages/batches/", id, "/results")
}

func (Anthropic) ReadStatus(reader io.Reader, _ func(Result) error) (Snapshot, error) {
	stream := newResultStream(reader)
	var snapshot Snapshot
	err := stream.object(func(key string) error {
		switch key {
		case "id":
			return stream.decode(&snapshot.ID)
		case "processing_status":
			return stream.decode(&snapshot.NativeState)
		default:
			return stream.skip()
		}
	})
	if err == nil {
		err = stream.end()
	}
	switch snapshot.NativeState {
	case "in_progress":
		snapshot.State = Running
	case "canceling":
		snapshot.State = Cancelling
	case "ended":
		snapshot.State = Completed // Individual items may still fail/cancel/expire.
	}
	return requireState(snapshot, err)
}

func (Anthropic) ReadResults(reader io.Reader, emit func(Result) error) error {
	return readJSONLines(reader, decodeAnthropicResult, emit)
}

func decodeAnthropicResult(data []byte) (Result, error) {
	var native struct {
		CustomID string `json:"custom_id"`
		Result   struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
			Error   json.RawMessage `json:"error"`
		} `json:"result"`
	}
	if err := common.Unmarshal(data, &native); err != nil {
		return Result{}, err
	}
	result := Result{CustomID: native.CustomID}
	switch native.Result.Type {
	case "succeeded":
		if common.GetJsonType(native.Result.Message) != "object" {
			return Result{}, errors.New("successful Anthropic result has no message")
		}
		result.StatusCode, result.Body = http.StatusOK, native.Result.Message
	case "errored":
		result.Error, result.Unbilled = native.Result.Error, true
	case "canceled", "expired":
		result.Error, _ = common.Marshal(map[string]string{"type": native.Result.Type})
		result.Unbilled = true
	default:
		return Result{}, fmt.Errorf("unknown Anthropic result type %q", native.Result.Type)
	}
	return result, nil
}
