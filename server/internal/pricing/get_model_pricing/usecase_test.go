package get_model_pricing

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReader struct {
	snapshotErr error
}

func (r fakeReader) Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error) {
	return nil, nil, "source", r.snapshotErr
}
func (fakeReader) FX() (domain.BillingFXBasis, error) { return domain.BillingFXBasis{Factor: 1}, nil }
func (fakeReader) AutoGroups(string) []string         { return nil }
func (fakeReader) UsableGroups(string) map[string]string {
	return map[string]string{"default": "default"}
}
func (fakeReader) GroupRatio(string, string) float64 { return 1 }

type fakeUsers struct{ err error }

func (u fakeUsers) UserGroup(int) (string, error) { return "default", u.err }

func TestExecuteClassifiesReadFailures(t *testing.T) {
	projection := domain.NewProjection(domain.ProjectionRuntime{})

	_, err := NewUsecase(fakeReader{}, projection, fakeUsers{err: errors.New("cache miss")}).Execute(Input{UserID: 7, Name: "gpt"})
	require.ErrorIs(t, err, domain.ErrUserGroupUnavailable)
	assert.EqualError(t, err, "failed to resolve user pricing group")

	_, err = NewUsecase(fakeReader{}, projection, fakeUsers{}).Execute(Input{UserID: 7, Name: "gpt", ExpectedRevision: "stale"})
	require.ErrorIs(t, err, domain.ErrRevisionMismatch)
	assert.EqualError(t, err, "Pricing catalog revision changed")

	_, err = NewUsecase(fakeReader{}, projection, fakeUsers{}).Execute(Input{UserID: 7, Name: "gpt"})
	require.ErrorIs(t, err, domain.ErrModelPricingUnavailable)
	assert.EqualError(t, err, "model pricing is unavailable")

	// A broken snapshot keeps its generic text and is answered as unavailable.
	_, err = NewUsecase(fakeReader{snapshotErr: errors.New("db down")}, projection, fakeUsers{}).Execute(Input{UserID: 7, Name: "gpt"})
	require.Error(t, err)
	assert.EqualError(t, err, "failed to resolve pricing catalog revision")
	assert.NotErrorIs(t, err, domain.ErrModelPricingUnavailable)
}
