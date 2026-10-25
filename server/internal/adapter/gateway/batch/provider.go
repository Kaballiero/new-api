package batch

import (
	"context"
	"errors"
	"io"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	native "github.com/QuantumNous/new-api/relay/batch"
)

type Gateway struct{}
type provider struct{ native.Adapter }

func unsupported(err error) error {
	if errors.Is(err, native.ErrUnsupported) {
		return domain.ErrUnsupported
	}
	return err
}
func (Gateway) ForChannel(kind int) (domain.Provider, error) {
	p, err := native.ForChannel(kind)
	if err != nil {
		return nil, unsupported(err)
	}
	return provider{p}, nil
}
func (p provider) Create(s domain.Submission) (domain.Request, error) {
	items := make([]native.Item, len(s.Items))
	for i := range s.Items {
		items[i] = native.Item(s.Items[i])
	}
	r, err := p.Adapter.Create(native.Submission{ID: s.ID, Model: s.Model, Endpoint: s.Endpoint,
		Provider: s.Provider, Items: items, InputFileID: s.InputFileID})
	return domain.Request(r), unsupported(err)
}
func (p provider) Poll(id string) (domain.Request, error) {
	r, e := p.Adapter.Poll(id)
	return domain.Request(r), unsupported(e)
}
func (p provider) Cancel(id string) (domain.Request, error) {
	r, e := p.Adapter.Cancel(id)
	return domain.Request(r), unsupported(e)
}
func (p provider) Delete(id string) (domain.Request, error) {
	r, e := p.Adapter.Delete(id)
	return domain.Request(r), unsupported(e)
}
func (p provider) ReadStatus(r io.Reader, emit func(domain.Result) error) (domain.Snapshot, error) {
	s, e := p.Adapter.ReadStatus(r, func(v native.Result) error { return emit(domain.Result(v)) })
	return domain.Snapshot(s), e
}
func (p provider) ReadResults(r io.Reader, emit func(domain.Result) error) error {
	return p.Adapter.ReadResults(r, func(v native.Result) error { return emit(domain.Result(v)) })
}
func (Gateway) Request(ctx context.Context, j *domain.Job, r domain.Request, body io.Reader, contentType string) (*domain.Response, error) {
	res, err := doBatchRequest(ctx, j, native.Request(r), body, contentType)
	if err != nil {
		return nil, err
	}
	return &domain.Response{StatusCode: res.StatusCode, RequestID: res.Header.Get("X-Request-Id"), Body: res.Body}, nil
}
func (Gateway) Upload(ctx context.Context, j *domain.Job, s domain.Submission) (string, error) {
	items := make([]native.Item, len(s.Items))
	for i := range s.Items {
		items[i] = native.Item(s.Items[i])
	}
	return uploadOpenAIBatchInput(ctx, j, native.Submission{ID: s.ID, Model: s.Model, Endpoint: s.Endpoint,
		Provider: s.Provider, Items: items, InputFileID: s.InputFileID})
}
func (Gateway) ResultsRequest(j *domain.Job, id string) (domain.Request, error) {
	r, e := batchResultsRequest(j, id)
	return domain.Request(r), unsupported(e)
}
