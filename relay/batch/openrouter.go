package batch

import (
	"io"
	"net/http"
)

type OpenRouter struct{}

func (OpenRouter) Create(submission Submission) (Request, error) {
	items := make([]Item, len(submission.Items))
	for i, item := range submission.Items {
		body, err := requestBody(item, submission.Model, false)
		if err != nil {
			return Request{}, err
		}
		items[i] = Item{CustomID: item.CustomID, Body: body}
	}
	// Field order matters: OpenRouter's streaming parser requires metadata
	// before requests. A struct deliberately preserves that wire order.
	return marshalRequest("/api/v1/batches", struct {
		Endpoint         string `json:"endpoint"`
		Model            string `json:"model"`
		CompletionWindow string `json:"completion_window"`
		Requests         []Item `json:"requests"`
	}{submission.Endpoint, submission.Model, "24h", items})
}

func (OpenRouter) Poll(id string) (Request, error) {
	return resourceRequest(http.MethodGet, "/api/v1/batches/", id, "")
}
func (OpenRouter) Cancel(string) (Request, error) { return Request{}, ErrUnsupported }
func (OpenRouter) Delete(id string) (Request, error) {
	return resourceRequest(http.MethodDelete, "/api/v1/batches/", id, "")
}
func (OpenRouter) ReadStatus(reader io.Reader, emit func(Result) error) (Snapshot, error) {
	return readOpenAIStatus(reader, emit, true)
}
func (OpenRouter) ReadResults(io.Reader, func(Result) error) error { return ErrUnsupported }
