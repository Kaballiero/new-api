package get_group_pricing

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReader struct{}

func (fakeReader) Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error) {
	return nil, nil, "source", nil
}
func (fakeReader) FX() (domain.BillingFXBasis, error) { return domain.BillingFXBasis{Factor: 1}, nil }
func (fakeReader) AutoGroups(string) []string         { return nil }
func (fakeReader) UsableGroups(string) map[string]string {
	return map[string]string{"default": "default"}
}
func (fakeReader) GroupRatio(string, string) float64 { return 1 }

type fakeGroups map[string]bool

func (g fakeGroups) ContainsGroup(group string) bool { return g[group] }

type fakeCache struct{ puts int }

func (*fakeCache) Get(string) (domain.ConsumerPricingResponse, bool) {
	return domain.ConsumerPricingResponse{}, false
}
func (c *fakeCache) Put(string, domain.ConsumerPricingResponse) { c.puts++ }

func TestExecuteRejectsGroupsAndStaleRevisions(t *testing.T) {
	cache := &fakeCache{}
	usecase := NewUsecase(domain.Capabilities{Codec: domain.JSONCodec{Marshal: common.Marshal}}, fakeReader{}, domain.NewProjection(domain.ProjectionRuntime{Capabilities: domain.Capabilities{Codec: domain.JSONCodec{Marshal: common.Marshal}}}), cache, fakeGroups{"default": true, "auto": true})

	for _, group := range []string{"auto", "vip"} {
		_, err := usecase.Execute(Input{Group: group})
		require.ErrorIs(t, err, domain.ErrInvalidUserGroup, group)
		assert.EqualError(t, err, "invalid user pricing group")
	}

	_, err := usecase.Execute(Input{Page: true, UsingGroup: "vip"})
	require.ErrorIs(t, err, domain.ErrInvalidQuery)
	assert.ErrorIs(t, err, domain.ErrPricingUsingGroup)
	assert.EqualError(t, err, "invalid using_group")

	_, err = usecase.Execute(Input{Page: true, Query: domain.EffectivePricingPageQuery{ExpectedRevision: "stale", Limit: 20}})
	require.ErrorIs(t, err, domain.ErrRevisionMismatch)

	// The full catalogue of an anonymous group read is cached; using_group
	// is ignored outside page mode.
	output, err := usecase.Execute(Input{UsingGroup: "vip"})
	require.NoError(t, err)
	require.NotNil(t, output.Full)
	assert.Equal(t, "default", output.Full.UserGroup)
	assert.Equal(t, 1, cache.puts)
}

func (fakeReader) Billable([]domain.Pricing, string, string) map[string]bool { return nil }
