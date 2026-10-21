package preview_pricing

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReader struct{ userErr error }

func (fakeReader) GroupRatios() map[string]float64 {
	return map[string]float64{"default": 1, "vip": 0.5, "auto": 1, "": 1}
}
func (r fakeReader) UserGroup(int) (string, error) { return "default", r.userErr }
func (fakeReader) FX() (domain.BillingFXBasis, error) {
	return domain.BillingFXBasis{Factor: 1}, nil
}
func (fakeReader) Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error) {
	return nil, nil, "", nil
}

func TestExecuteValidatesThePreviewGroup(t *testing.T) {
	projection := domain.NewProjection(domain.ProjectionRuntime{
		UsableGroups: func(string) map[string]string { return map[string]string{} },
		GroupRatio:   func(string, string) float64 { return 1 },
		AutoGroups:   func(string) []string { return nil },
		QuotaUnit:    func() float64 { return 500000 },
	})

	for _, groups := range [][]string{{"auto"}, {"missing"}, {"default", "vip"}, {}} {
		_, err := NewUsecase(fakeReader{}, projection).Execute(Input{UserGroups: groups, Supplied: true})
		require.ErrorIs(t, err, domain.ErrInvalidUserGroup, groups)
	}

	_, err := NewUsecase(fakeReader{userErr: errors.New("cache miss")}, projection).Execute(Input{UserID: 3})
	require.ErrorIs(t, err, domain.ErrUserGroupUnavailable)

	output, err := NewUsecase(fakeReader{}, projection).Execute(Input{UserGroups: []string{"vip"}, Supplied: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "vip"}, output.AvailableUserGroups)
}
