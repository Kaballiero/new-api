package record_failure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/record_failure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type writer struct {
	failures []domain.Failure
	err      error
}

func (w *writer) WriteFailure(_ context.Context, in domain.Failure) error {
	w.failures = append(w.failures, in)
	return w.err
}

type renderer struct{}

func (renderer) RenderNote(n domain.Note) string { return "delivery: " + n.Name }
func TestDeliveryRecordingReadsPolicyAfterConsumeAndPropagatesWriterError(t *testing.T) {
	store := &writer{}
	reads, enabled := 0, false
	usecase := record_failure.NewUsecase(store, renderer{}, func() bool { reads++; return enabled })
	consumed := domain.Consumption{Record: domain.UsageRecord{UserID: 7, Quota: 19}}
	input := record_failure.Input{Step: record_failure.Persist, Failure: domain.Failure{Consumption: &consumed}}
	_, err := usecase.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Zero(t, reads)
	input.Failure.DeliveryReason = "stream_timeout"
	_, err = usecase.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, 1, reads)
	assert.Empty(t, store.failures)
	enabled = true
	_, err = usecase.Execute(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, store.failures, 1)
	assert.Equal(t, consumed.Record, store.failures[0].Record)
	assert.Equal(t, "delivery: stream_timeout", store.failures[0].Content)
	assert.Equal(t, 2, reads)
	store.err = errors.New("writer failed")
	_, err = usecase.Execute(context.Background(), input)
	require.ErrorIs(t, err, store.err)
}
func TestUpstreamAdmissionIsReadBeforeMetadataAndReusedAtWrite(t *testing.T) {
	store := &writer{}
	reads, enabled := 0, false
	usecase := record_failure.NewUsecase(store, renderer{}, func() bool { reads++; return enabled })
	rejected, err := usecase.Execute(context.Background(), record_failure.Input{Step: record_failure.AdmitUpstream})
	require.NoError(t, err)
	assert.False(t, rejected.Allowed())
	assert.Equal(t, 1, reads)
	enabled = true
	admission, err := usecase.Execute(context.Background(), record_failure.Input{Step: record_failure.AdmitUpstream})
	require.NoError(t, err)
	assert.True(t, admission.Allowed())
	assert.Equal(t, 2, reads)
	enabled = false
	input := record_failure.Input{Step: record_failure.Persist, Admission: admission, Failure: domain.Failure{Content: "upstream error"}}
	_, err = usecase.Execute(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, store.failures, 1)
	assert.Equal(t, "upstream error", store.failures[0].Content)
	assert.Equal(t, 2, reads, "persist must not reread the early gate")
	input.Admission = rejected
	_, err = usecase.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Len(t, store.failures, 1)
}

func TestUpstreamZeroAdmissionCannotAuthorizePersistence(t *testing.T) {
	store := &writer{}
	reads := 0
	usecase := record_failure.NewUsecase(store, renderer{}, func() bool { reads++; return true })
	_, err := usecase.Execute(context.Background(), record_failure.Input{
		Step: record_failure.Persist, Failure: domain.Failure{Content: "upstream error"},
	})
	require.NoError(t, err)
	assert.Empty(t, store.failures)
	assert.Zero(t, reads)
}
