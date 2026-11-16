package refresh_catalogue

import (
	"context"
	"errors"
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

// catalogStore holds one enabled OpenRouter catalog channel with one model and
// the model mapping.
type catalogStore struct{ modelMapping string }

func (catalogStore) Abilities() ([]domain.BuildAbility, error) {
	return []domain.BuildAbility{{Model: "acme/chat", Group: "default", ChannelId: 1, ChannelType: constant.ChannelTypeOpenRouter}}, nil
}
func (store catalogStore) BuildChannel(int) (*domain.BuildChannel, error) {
	channel := catalogChannel{}.Value()
	channel.ModelMapping = &store.modelMapping
	return channel, nil
}
func (catalogStore) Pricing() []domain.Pricing    { return nil }
func (catalogStore) Channel(int) (Channel, error) { return catalogChannel{}, nil }

type recordedAlerts struct {
	sent []string
	err  error
}

func (alerts *recordedAlerts) ModelLoss(lossErr domain.CatalogModelLossError) error {
	alerts.sent = append(alerts.sent, lossErr.RemoteRevision)
	return alerts.err
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

// A channel whose model mapping does not resolve leaves the requested models
// unknown: the task fails before the catalog sync, so the head stays.
func TestRefreshUpstreamKeepsHeadWithUnresolvedMapping(t *testing.T) {
	for _, modelMapping := range []string{`{"acme/chat":`, `{"acme/chat":"acme/loop","acme/loop":"acme/chat"}`} {
		usecase := NewUsecase(pricingadapter.Capabilities(), catalogStore{modelMapping: modelMapping}, nil, func(context.Context, domain.CatalogSource, map[string]bool) (domain.CatalogSyncSummary, error) {
			t.Error("the catalog must not sync")
			return domain.CatalogSyncSummary{}, nil
		}, nil, nil, nil, time.Now, &recordedAlerts{}, &optionRows{values: map[string]string{}})

		_, err := usecase.RefreshUpstream(context.Background(), domain.CatalogSource{})

		require.EqualError(t, err, "catalog channel model mapping is unresolved", modelMapping)
	}
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
	}, nil, nil, nil, time.Now, alerts, rows)

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
