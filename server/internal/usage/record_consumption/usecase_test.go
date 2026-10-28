package record_consumption_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/record_consumption"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type renderer struct{ notes []domain.Note }

func (r *renderer) RenderNote(n domain.Note) string {
	r.notes = append(r.notes, n)
	switch n.Kind {
	case domain.LiteralContent, domain.OriginalModel:
		return n.Name
	case domain.MissingUpstreamUsage:
		return "missing"
	case domain.ToolCharge:
		return "tool"
	case domain.UnbillableUsage:
		return "unbillable"
	case domain.SettlementRepair:
		return "repair"
	default:
		return "audio"
	}
}
func (r *renderer) RenderContent(notes []domain.Note) string {
	var parts []string
	for _, note := range notes {
		parts = append(parts, r.RenderNote(note))
	}
	return strings.Join(parts, "|")
}

type writer struct {
	consumed domain.Consumption
	writes   int
	err      error
}

func (w *writer) WriteConsumption(_ context.Context, in domain.Consumption) error {
	w.consumed = in
	w.writes++
	return w.err
}
func TestConsumptionDescribesBeforeAndFinalizesAfterSettlement(t *testing.T) {
	store, display := &writer{}, &renderer{}
	usecase := record_consumption.NewUsecase(store, display, record_consumption.AudioPrices{})
	described, err := usecase.Execute(context.Background(), record_consumption.Input{
		Step:        record_consumption.Describe,
		Consumption: domain.Consumption{Kind: domain.TextConsumption, Record: domain.UsageRecord{ModelName: "gpt-4-gizmo-specific"}},
		Extra:       []string{"extra"},
		Text:        domain.TextDescription{OriginMissing: true, Billable: false, Tools: []billingdomain.ToolSurchargeItem{{Name: "search", Count: 2, DisplayQuota: 17}}},
	})
	require.NoError(t, err)
	assert.Zero(t, store.writes)
	require.Len(t, display.notes, 4)
	assert.Equal(t, domain.Note{Kind: domain.ToolCharge, Name: "search", Count: 2, Quota: 17}, display.notes[2])
	described.Text = &domain.TextFacts{Outcome: &billingdomain.Outcome{SettlementStatus: "token_delta_failed"}}
	recorded, err := usecase.Execute(context.Background(), record_consumption.Input{Step: record_consumption.Persist, Consumption: described})
	require.NoError(t, err)
	assert.Equal(t, "gpt-4-gizmo-*", recorded.Record.ModelName)
	assert.Equal(t, "extra|missing|tool|unbillable|repair|gpt-4-gizmo-specific", recorded.RenderedContent)
	assert.Equal(t, recorded, store.consumed)
	described.Text.Outcome.SettlementStatus = "settled"
	recorded, err = usecase.Execute(context.Background(), record_consumption.Input{Step: record_consumption.Persist, Consumption: described})
	require.NoError(t, err)
	assert.Equal(t, "extra|missing|tool|unbillable|gpt-4-gizmo-specific", recorded.RenderedContent)
	store.err = errors.New("unsupported context")
	_, err = usecase.Execute(context.Background(), record_consumption.Input{Step: record_consumption.Persist, Consumption: described})
	require.ErrorIs(t, err, store.err)
}
func TestAudioDisplayReadsAreLateOrderedAndFrozenBeforePersistence(t *testing.T) {
	store, display := &writer{}, &renderer{}
	var reads []string
	ratio := 2.0
	usecase := record_consumption.NewUsecase(store, display, record_consumption.AudioPrices{
		Completion: func(model string) float64 { reads = append(reads, "completion:"+model); return ratio },
		Input:      func(model string) float64 { reads = append(reads, "input:"+model); return ratio },
		Output:     func(model string) float64 { reads = append(reads, "output:"+model); return ratio },
	})
	assert.Empty(t, reads)
	described, err := usecase.Execute(context.Background(), record_consumption.Input{
		Step:        record_consumption.Describe,
		Consumption: domain.Consumption{Kind: domain.AudioConsumption, Record: domain.UsageRecord{ModelName: "mapped"}, Audio: &domain.AudioFacts{}},
		Audio:       record_consumption.AudioDescription{InputModel: "origin", Billable: false},
		Extra:       []string{"extra"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"completion:mapped", "input:origin", "output:mapped"}, reads)
	assert.Equal(t, 2.0, described.Prices.CompletionRatio)
	assert.Equal(t, 2.0, described.Audio.AudioRatio)
	assert.Zero(t, store.writes)
	ratio = 9
	_, err = usecase.Execute(context.Background(), record_consumption.Input{Step: record_consumption.Persist, Consumption: described})
	require.NoError(t, err)
	assert.Len(t, reads, 3)
	assert.Equal(t, 2.0, store.consumed.Audio.AudioCompletionRatio)
}
