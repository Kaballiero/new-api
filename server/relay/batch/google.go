package batch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Google is the Gemini Developer API. Vertex AI uses a different Batch API
// and is deliberately not selected by this adapter.
type Google struct{}

var googleModel = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func (Google) Create(submission Submission) (Request, error) {
	if submission.Endpoint != "/v1beta/generateContent" {
		return Request{}, errors.New("Google batch supports generateContent")
	}
	model := strings.TrimPrefix(submission.Model, "models/")
	if !googleModel.MatchString(model) {
		return Request{}, errors.New("invalid Google model name")
	}
	type request struct {
		Request  json.RawMessage   `json:"request"`
		Metadata map[string]string `json:"metadata"`
	}
	requests := make([]request, len(submission.Items))
	for i, item := range submission.Items {
		body, err := requestBody(item, submission.Model, false)
		if err != nil {
			return Request{}, err
		}
		var fields map[string]json.RawMessage
		if err := common.Unmarshal(body, &fields); err != nil {
			return Request{}, err
		}
		// The model belongs to the operation URL, not GenerateContentRequest.
		delete(fields, "model")
		body, err = common.Marshal(fields)
		if err != nil {
			return Request{}, err
		}
		requests[i] = request{body, map[string]string{"custom_id": item.CustomID}}
	}
	var body struct {
		Batch struct {
			DisplayName string `json:"displayName"`
			InputConfig struct {
				Requests struct {
					Requests []request `json:"requests"`
				} `json:"requests"`
			} `json:"inputConfig"`
		} `json:"batch"`
	}
	body.Batch.DisplayName = submission.ID
	body.Batch.InputConfig.Requests.Requests = requests
	return marshalRequest("/v1beta/models/"+model+":batchGenerateContent", body)
}

func (Google) Poll(name string) (Request, error) {
	id, err := googleID(name)
	if err != nil {
		return Request{}, err
	}
	return resourceRequest(http.MethodGet, "/v1beta/batches/", id, "")
}
func (Google) Cancel(name string) (Request, error) {
	id, err := googleID(name)
	if err != nil {
		return Request{}, err
	}
	return resourceRequest(http.MethodPost, "/v1beta/batches/", id, ":cancel")
}
func (Google) Delete(name string) (Request, error) {
	id, err := googleID(name)
	if err != nil {
		return Request{}, err
	}
	return resourceRequest(http.MethodDelete, "/v1beta/batches/", id, "")
}

func (Google) ReadStatus(reader io.Reader, emit func(Result) error) (Snapshot, error) {
	stream := newResultStream(reader)
	var snapshot Snapshot
	err := stream.object(func(key string) error {
		switch key {
		case "name":
			return stream.decode(&snapshot.ID)
		case "error":
			return stream.decode(&snapshot.Error)
		case "metadata":
			return stream.object(func(key string) error {
				if key == "state" {
					return stream.decode(&snapshot.NativeState)
				}
				return stream.skip()
			})
		case "response":
			return stream.object(func(key string) error {
				switch key {
				case "responsesFile":
					return stream.decode(&snapshot.OutputFileID)
				case "inlinedResponses":
					return stream.object(func(key string) error {
						if key != "inlinedResponses" {
							return stream.skip()
						}
						return stream.array(func() error {
							var raw json.RawMessage
							if err := stream.decode(&raw); err != nil {
								return err
							}
							result, err := decodeGoogleResult(raw)
							if err != nil {
								return err
							}
							if result.CustomID == "" {
								return errors.New("Google result has no custom_id metadata")
							}
							return emit(result)
						})
					})
				default:
					return stream.skip()
				}
			})
		default:
			return stream.skip()
		}
	})
	if err == nil {
		err = stream.end()
	}
	switch snapshot.NativeState {
	case "BATCH_STATE_PENDING":
		snapshot.State = Validating
	case "BATCH_STATE_RUNNING":
		snapshot.State = Running
	case "BATCH_STATE_SUCCEEDED":
		snapshot.State = Completed
	case "BATCH_STATE_FAILED":
		snapshot.State = Failed
	case "BATCH_STATE_CANCELLED":
		snapshot.State = Cancelled
	case "BATCH_STATE_EXPIRED":
		snapshot.State = Expired
	}
	return requireState(snapshot, err)
}

func (Google) ReadResults(reader io.Reader, emit func(Result) error) error {
	return readJSONLines(reader, decodeGoogleResult, emit)
}

func decodeGoogleResult(data []byte) (Result, error) {
	var native struct {
		Key      string            `json:"key"`
		Metadata map[string]string `json:"metadata"`
		Response json.RawMessage   `json:"response"`
		Error    json.RawMessage   `json:"error"`
	}
	if err := common.Unmarshal(data, &native); err != nil {
		return Result{}, err
	}
	id := native.Key
	if metadataID := native.Metadata["custom_id"]; metadataID != "" {
		if id != "" && id != metadataID {
			return Result{}, errors.New("conflicting Google result IDs")
		}
		id = metadataID
	}
	result := Result{CustomID: id, Error: native.Error}
	if common.GetJsonType(native.Response) == "object" {
		result.StatusCode, result.Body = http.StatusOK, native.Response
	} else if common.GetJsonType(native.Error) != "object" {
		return Result{}, fmt.Errorf("Google batch result %q has neither response nor error", id)
	}
	return result, nil
}
