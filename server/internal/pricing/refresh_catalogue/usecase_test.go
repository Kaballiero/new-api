package refresh_catalogue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type catalogChannel struct{}

func (catalogChannel) Value() *domain.BuildChannel {
	return &domain.BuildChannel{Id: 1, Type: constant.ChannelTypeOpenRouter, Status: constant.ChannelStatusEnabled}
}
func (catalogChannel) Metadata(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected metadata read")
}

// catalogStore holds one enabled OpenRouter catalog channel with one model.
type catalogStore struct{}

func (catalogStore) Abilities() ([]domain.BuildAbility, error) {
	return []domain.BuildAbility{{Model: "acme/chat", Group: "default", ChannelId: 1, ChannelType: constant.ChannelTypeOpenRouter}}, nil
}
func (catalogStore) BuildChannel(int) (*domain.BuildChannel, error) {
	return catalogChannel{}.Value(), nil
}
func (catalogStore) Pricing() []domain.Pricing    { return nil }
func (catalogStore) Channel(int) (Channel, error) { return catalogChannel{}, nil }

type recordedAlerts struct {
	sent         []string
	err          error
	priceChanges [][]domain.CatalogPriceChange
	priceErr     error
}

func (alerts *recordedAlerts) ModelLoss(lossErr domain.CatalogModelLossError) error {
	alerts.sent = append(alerts.sent, lossErr.RemoteRevision)
	return alerts.err
}
func (alerts *recordedAlerts) PriceChanges(changes []domain.CatalogPriceChange) error {
	alerts.priceChanges = append(alerts.priceChanges, changes)
	return alerts.priceErr
}

type optionRows struct {
	values           map[string]string
	readErr, saveErr error
}

func (rows *optionRows) ReadOption(_ context.Context, key string) (string, error) {
	if rows.readErr != nil {
		return "", rows.readErr
	}
	return rows.values[key], nil
}
func (rows *optionRows) SaveBulk(_ context.Context, keys []string, values map[string]string) error {
	if rows.saveErr != nil {
		return rows.saveErr
	}
	for _, key := range keys {
		rows.values[key] = values[key]
	}
	return nil
}

// A rejected catalog revision fails the task and alerts once per revision; an
// alert that was not delivered is sent by the next run.
func TestRefreshUpstreamAlertsRejectedRevisionOnce(t *testing.T) {
	alerts := &recordedAlerts{}
	rows := &optionRows{values: map[string]string{}}
	var rejected string
	usecase := NewUsecase(pricingadapter.Capabilities(), catalogStore{}, nil, func(_ context.Context, _ domain.CatalogSource, requested map[string]bool) (domain.CatalogSyncSummary, error) {
		if rejected == "" {
			return domain.CatalogSyncSummary{ImportedModels: len(requested)}, nil
		}
		return domain.CatalogSyncSummary{}, domain.CatalogModelLossError{RemoteRevision: rejected, Lost: []string{"acme/chat"}, Published: 1}
	}, nil, nil, nil, time.Now, alerts, rows, func(string) {})

	for _, step := range []struct {
		name        string
		rejected    string
		alertErr    error
		readErr     error
		saveErr     error
		wantAlerts  []string
		wantErr     string
		wantAlerted string
	}{
		{name: "rejected revision is alerted", rejected: "a", wantAlerts: []string{"a"}, wantAlerted: "a"},
		{name: "same rejected revision is not alerted again", rejected: "a", wantAlerted: "a"},
		{name: "another rejected revision is alerted", rejected: "b", wantAlerts: []string{"b"}, wantAlerted: "b"},
		{name: "undelivered alert is not recorded", rejected: "c", alertErr: errors.New("gateway unavailable"), wantAlerts: []string{"c"}, wantErr: "catalog model loss alert failed: gateway unavailable", wantAlerted: "b"},
		{name: "undelivered alert is sent by the next run", rejected: "c", wantAlerts: []string{"c"}, wantAlerted: "c"},
		{name: "unreadable record still alerts", rejected: "c", readErr: errors.New("options unavailable"), wantAlerts: []string{"c"}, wantErr: "options unavailable", wantAlerted: "c"},
		{name: "unrecorded alert is reported", rejected: "d", saveErr: errors.New("options read-only"), wantAlerts: []string{"d"}, wantErr: "catalog model loss alert record failed: options read-only", wantAlerted: "c"},
		{name: "unrecorded alert is sent again", rejected: "d", wantAlerts: []string{"d"}, wantAlerted: "d"},
		{name: "accepted revision is not alerted", wantAlerted: "d"},
	} {
		rejected = step.rejected
		alerts.sent, alerts.err = nil, step.alertErr
		rows.readErr, rows.saveErr = step.readErr, step.saveErr

		summary, err := usecase.RefreshUpstream(context.Background(), domain.CatalogSource{})

		assert.Equal(t, step.wantAlerts, alerts.sent, step.name)
		assert.Equal(t, step.wantAlerted, rows.values[rejectedRevisionKey], step.name)
		if step.rejected == "" {
			require.NoError(t, err, step.name)
			assert.Equal(t, 1, summary.Published, step.name)
			continue
		}
		var lossErr domain.CatalogModelLossError
		require.ErrorAs(t, err, &lossErr, step.name)
		assert.Equal(t, step.rejected, lossErr.RemoteRevision, step.name)
		assert.Equal(t, []string{"acme/chat"}, summary.LostModels, step.name)
		if step.wantErr != "" {
			assert.ErrorContains(t, err, step.wantErr, step.name)
		}
	}
}

// The prices an applied revision changed go to the summary and to the
// administrators; an undelivered report is logged and does not fail the task,
// and a revision without such changes sends nothing.
func TestRefreshUpstreamReportsPriceChanges(t *testing.T) {
	changes := []domain.CatalogPriceChange{{ModelID: "acme/chat", Component: "input", Unit: "million_tokens", OldAmountRub: 112.5, NewAmountRub: 337.5}}
	for _, tc := range []struct {
		name     string
		changes  []domain.CatalogPriceChange
		alertErr error
		wantSent [][]domain.CatalogPriceChange
		wantLogs []string
	}{
		{name: "changes are reported", changes: changes, wantSent: [][]domain.CatalogPriceChange{changes}},
		{name: "undelivered report is logged", changes: changes, alertErr: errors.New("gateway unavailable"), wantSent: [][]domain.CatalogPriceChange{changes},
			wantLogs: []string{"pricing: upstream catalog price change report failed: revision=" + strings.Repeat("e", 64) + " changes=1: gateway unavailable"}},
		{name: "no changes send nothing"},
	} {
		alerts := &recordedAlerts{priceErr: tc.alertErr}
		var logs []string
		usecase := NewUsecase(pricingadapter.Capabilities(), catalogStore{}, nil, func(context.Context, domain.CatalogSource, map[string]bool) (domain.CatalogSyncSummary, error) {
			return domain.CatalogSyncSummary{Publication: domain.CatalogPublication{Revision: strings.Repeat("e", 64), Changed: true}, ImportedModels: 1, PriceChanges: tc.changes}, nil
		}, nil, nil, nil, time.Now, alerts, &optionRows{values: map[string]string{}}, func(line string) { logs = append(logs, line) })

		summary, err := usecase.RefreshUpstream(context.Background(), domain.CatalogSource{})

		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.changes, summary.PriceChanges, tc.name)
		assert.Equal(t, tc.wantSent, alerts.priceChanges, tc.name)
		assert.Equal(t, tc.wantLogs, logs, tc.name)
		assert.Empty(t, alerts.sent, tc.name)
	}
}
