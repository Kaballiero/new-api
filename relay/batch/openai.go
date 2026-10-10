package batch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
)

type OpenAI struct{}

// WriteInput writes the JSONL file uploaded with purpose=batch. Upload and
// creation are separate durable stages in the host, never an implicit retry.
func (OpenAI) WriteInput(writer io.Writer, submission Submission) error {
	for _, item := range submission.Items {
		body, err := requestBody(item, submission.Model, true)
		if err != nil {
			return err
		}
		line, err := common.Marshal(struct {
			CustomID string          `json:"custom_id"`
			Method   string          `json:"method"`
			URL      string          `json:"url"`
			Body     json.RawMessage `json:"body"`
		}{item.CustomID, http.MethodPost, submission.Endpoint, body})
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func (OpenAI) Create(submission Submission) (Request, error) {
	if !nativeID.MatchString(submission.InputFileID) {
		return Request{}, errors.New("persisted OpenAI input file ID is required")
	}
	return marshalRequest("/v1/batches", struct {
		InputFileID      string `json:"input_file_id"`
		Endpoint         string `json:"endpoint"`
		CompletionWindow string `json:"completion_window"`
	}{submission.InputFileID, submission.Endpoint, "24h"})
}

func (OpenAI) Poll(id string) (Request, error) {
	return resourceRequest(http.MethodGet, "/v1/batches/", id, "")
}
func (OpenAI) Cancel(id string) (Request, error) {
	return resourceRequest(http.MethodPost, "/v1/batches/", id, "/cancel")
}
func (OpenAI) Delete(string) (Request, error) { return Request{}, ErrUnsupported }

func (OpenAI) ReadStatus(reader io.Reader, emit func(Result) error) (Snapshot, error) {
	return readOpenAIStatus(reader, emit, false)
}

func (OpenAI) ReadResults(reader io.Reader, emit func(Result) error) error {
	return readJSONLines(reader, decodeOpenAIResult, emit)
}

func readOpenAIStatus(reader io.Reader, emit func(Result) error, openRouter bool) (Snapshot, error) {
	stream := newResultStream(reader)
	var snapshot Snapshot
	err := stream.object(func(key string) error {
		switch key {
		case "id":
			return stream.decode(&snapshot.ID)
		case "status":
			return stream.decode(&snapshot.NativeState)
		case "output_file_id":
			return stream.decode(&snapshot.OutputFileID)
		case "error_file_id":
			return stream.decode(&snapshot.ErrorFileID)
		case "usage":
			return stream.decode(&snapshot.Usage)
		case "error", "errors":
			return stream.decode(&snapshot.Error)
		case "results":
			if !openRouter {
				return stream.skip()
			}
			return stream.array(func() error {
				var raw json.RawMessage
				if err := stream.decode(&raw); err != nil {
					return err
				}
				result, err := decodeOpenAIResult(raw)
				if err != nil {
					return err
				}
				// OpenRouter's charge can include fees independently of the
				// upstream response. Preserve failures for reconciliation.
				result.Unbilled = false
				if result.CustomID == "" {
					return errors.New("OpenRouter result has no custom_id")
				}
				return emit(result)
			})
		default:
			return stream.skip()
		}
	})
	if err == nil {
		err = stream.end()
	}
	switch State(snapshot.NativeState) {
	case Validating, Running, Finalizing, Completed, Failed, Expired, Cancelling, Cancelled:
		snapshot.State = State(snapshot.NativeState)
	}
	return requireState(snapshot, err)
}

func decodeOpenAIResult(data []byte) (Result, error) {
	var native struct {
		CustomID string `json:"custom_id"`
		Response *struct {
			StatusCode int             `json:"status_code"`
			Body       json.RawMessage `json:"body"`
		} `json:"response"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &native); err != nil {
		return Result{}, err
	}
	result := Result{CustomID: native.CustomID, Error: native.Error}
	if native.Response != nil {
		result.StatusCode, result.Body = native.Response.StatusCode, native.Response.Body
		if result.StatusCode < 100 || result.StatusCode > 599 {
			return Result{}, fmt.Errorf("invalid batch result HTTP status %d", result.StatusCode)
		}
	} else if len(native.Error) != 0 && string(native.Error) != "null" {
		var failure struct {
			Code string `json:"code"`
		}
		if err := common.Unmarshal(native.Error, &failure); err != nil {
			return Result{}, err
		}
		// This documented code means the request was not executed before
		// expiration. Unknown failures can still require cost reconciliation.
		result.Unbilled = failure.Code == "batch_expired"
	} else {
		return Result{}, errors.New("batch result has neither response nor error")
	}
	return result, nil
}
