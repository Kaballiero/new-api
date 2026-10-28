package count_consumption_test

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/usage/count_consumption"

	"github.com/stretchr/testify/assert"
)

type counter struct {
	calls                                      []string
	userID, channelID, userQuota, channelQuota int
}

func (c *counter) AddUserConsumption(id, quota int) {
	c.calls = append(c.calls, "user")
	c.userID, c.userQuota = id, quota
}
func (c *counter) AddChannelConsumption(id, quota int) {
	c.calls = append(c.calls, "channel")
	c.channelID, c.channelQuota = id, quota
}
func TestCountingSelectsBillingMomentAndAppliedQuota(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		moment                      count_consumption.Moment
		billable, provider, counted bool
		applied, want               int
	}{
		{name: "ordinary before", billable: true, counted: true, want: 19},
		{name: "ordinary unbillable"},
		{name: "ordinary after", moment: count_consumption.AfterSettlement, billable: true},
		{name: "provider before", billable: true, provider: true},
		{name: "provider applied", moment: count_consumption.AfterSettlement, provider: true, counted: true, applied: 7, want: 7},
		{name: "provider applied zero", moment: count_consumption.AfterSettlement, provider: true, counted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &counter{}
			count_consumption.NewUsecase(store).Execute(count_consumption.Input{
				Moment: tc.moment, UserID: 7, ChannelID: 11,
				Billable: tc.billable, ProviderCost: tc.provider, QuotedQuota: 19, AppliedQuota: tc.applied,
			})
			if !tc.counted {
				assert.Empty(t, store.calls)
				return
			}
			assert.Equal(t, []string{"user", "channel"}, store.calls)
			assert.Equal(t, 7, store.userID)
			assert.Equal(t, 11, store.channelID)
			assert.Equal(t, tc.want, store.userQuota)
			assert.Equal(t, tc.want, store.channelQuota)
		})
	}
}
