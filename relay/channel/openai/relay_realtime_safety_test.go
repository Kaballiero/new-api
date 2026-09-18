package openai

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreConsumeUsageRejectsInvalidUsageWithoutMutatingAggregate(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	for _, invalidFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid_first_%t", invalidFirst), func(t *testing.T) {
			info := &relaycommon.RelayInfo{UsePrice: true}
			valid := &dto.RealtimeUsage{TotalTokens: 100, InputTokens: 100, InputTokenDetails: dto.InputTokenDetails{TextTokens: 100}}
			invalid := &dto.RealtimeUsage{TotalTokens: 100, InputTokens: 100, InputTokenDetails: dto.InputTokenDetails{AudioTokens: -100}}
			total := &dto.RealtimeUsage{}
			if !invalidFirst {
				require.NoError(t, preConsumeUsage(c, info, valid, total))
			}
			before := deepRealtimeSnapshot(t, total)
			require.Error(t, preConsumeUsage(c, info, invalid, total))
			assert.Equal(t, before, total)
			if invalidFirst {
				require.NoError(t, preConsumeUsage(c, info, valid, total))
			}
			assert.Equal(t, valid, total)
		})
	}
}

func deepRealtimeSnapshot(t *testing.T, u *dto.RealtimeUsage) *dto.RealtimeUsage {
	t.Helper()
	encoded, err := common.Marshal(u)
	require.NoError(t, err)
	var snapshot dto.RealtimeUsage
	require.NoError(t, common.Unmarshal(encoded, &snapshot))
	// Sticky billing flags are intentionally not wire fields.
	snapshot.CacheDiscountUnavailable = u.CacheDiscountUnavailable
	snapshot.CacheDiscountUnavailableReason = u.CacheDiscountUnavailableReason
	return &snapshot
}

func TestPreConsumeUsageRejectsOverflowWithoutPartialMutation(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{UsePrice: true}
	total := &dto.RealtimeUsage{TotalTokens: math.MaxInt, InputTokens: math.MaxInt}
	usage := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1}}
	require.Error(t, preConsumeUsage(c, info, usage, total))
	assert.Equal(t, math.MaxInt, total.TotalTokens)
	assert.Equal(t, math.MaxInt, total.InputTokens)
}

func TestPreConsumeUsageKeepsIrrelevantCacheFactsOutOfAggregate(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	for _, expr := range []string{"p*2", "p*2+cr", "p*2+cr+ai", "p*2+cr+img"} {
		for _, noise := range []int{-1, math.MaxInt} {
			t.Run(fmt.Sprintf("%s/%d", expr, noise), func(t *testing.T) {
				info := &relaycommon.RelayInfo{UsePrice: true, TieredBillingSnapshot: &billingexpr.BillingSnapshot{ExprString: expr}}
				vars := billingexpr.UsedVars(expr)
				total := &dto.RealtimeUsage{}
				for range 2 {
					cache := 1
					if !vars["cr"] {
						cache = noise
					}
					if vars["ai"] || vars["img"] {
						cache = 0
					}
					u := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedTokens: cache, CachedTokensDetails: &dto.CachedTokenDetails{TextTokens: &noise, AudioTokens: &noise, ImageTokens: &noise}}}
					require.NoError(t, preConsumeUsage(c, info, u, total))
				}
				assert.Equal(t, 2, total.TotalTokens)
				assert.Equal(t, 2, total.InputTokens)
				assert.Equal(t, 2, total.InputTokenDetails.TextTokens)
				assert.Nil(t, total.InputTokenDetails.CachedTokensDetails)
				assert.False(t, total.CacheDiscountUnavailable)
				assert.Empty(t, total.CacheDiscountUnavailableReason)
				params := service.BuildRealtimeTieredTokenParams(total, vars)
				if vars["cr"] && !vars["ai"] && !vars["img"] {
					assert.Equal(t, float64(2), params.CR)
					assert.Zero(t, params.P)
				} else {
					assert.Equal(t, float64(2), params.P)
				}
			})
		}
	}
}

func TestPreConsumeUsageLateOverflowPreservesDeepPointersAndStickyReason(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	value := func(n int) *int { return &n }
	for _, sticky := range []bool{false, true} {
		t.Run(fmt.Sprintf("sticky_%t", sticky), func(t *testing.T) {
			info := &relaycommon.RelayInfo{UsePrice: true, TieredBillingSnapshot: &billingexpr.BillingSnapshot{ExprString: "p*2+ai+img+cr+cc"}}
			total := &dto.RealtimeUsage{}
			first := &dto.RealtimeUsage{TotalTokens: 6, InputTokens: 6, InputTokenDetails: dto.InputTokenDetails{TextTokens: 2, AudioTokens: 2, ImageTokens: 2, CachedTokens: 3, CachedCreationTokens: math.MaxInt, CachedTokensDetails: &dto.CachedTokenDetails{TextTokens: value(1), AudioTokens: value(1), ImageTokens: value(1)}}}
			require.NoError(t, preConsumeUsage(c, info, first, total))
			// Admission retains only priced overlaps; supply the valid redundant text
			// pointer as well to protect every pointer accepted in aggregate state.
			total.InputTokenDetails.CachedTokensDetails.TextTokens = value(1)
			if sticky {
				poison := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedTokens: 1, CachedTokensDetails: &dto.CachedTokenDetails{AudioTokens: value(1)}}}
				require.NoError(t, preConsumeUsage(c, info, poison, total))
				require.True(t, total.CacheDiscountUnavailable)
				require.Equal(t, "inconsistent_overlap_details", total.CacheDiscountUnavailableReason)
			}
			before := deepRealtimeSnapshot(t, total)
			retainedPointers := *total.InputTokenDetails.CachedTokensDetails
			rejected := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedTokens: 1, CachedCreationTokens: 1, CachedTokensDetails: &dto.CachedTokenDetails{AudioTokens: value(1)}}}
			require.ErrorContains(t, preConsumeUsage(c, info, rejected, total), "aggregate overflow")
			assert.Equal(t, before, total)
			next := &dto.RealtimeUsage{TotalTokens: 6, InputTokens: 6, InputTokenDetails: dto.InputTokenDetails{TextTokens: 2, AudioTokens: 2, ImageTokens: 2, CachedTokens: 3, CachedTokensDetails: &dto.CachedTokenDetails{TextTokens: value(1), AudioTokens: value(1), ImageTokens: value(1)}}}
			require.NoError(t, preConsumeUsage(c, info, next, total))
			assert.Equal(t, 2, *total.InputTokenDetails.CachedTokensDetails.AudioTokens)
			assert.Equal(t, 2, *total.InputTokenDetails.CachedTokensDetails.ImageTokens)
			assert.Equal(t, *before.InputTokenDetails.CachedTokensDetails, retainedPointers)
			assert.NotSame(t, retainedPointers.AudioTokens, total.InputTokenDetails.CachedTokensDetails.AudioTokens)
			assert.NotSame(t, retainedPointers.ImageTokens, total.InputTokenDetails.CachedTokensDetails.ImageTokens)
			assert.Equal(t, before.CacheDiscountUnavailable, total.CacheDiscountUnavailable)
			assert.Equal(t, before.CacheDiscountUnavailableReason, total.CacheDiscountUnavailableReason)
		})
	}
}

func TestPreConsumeUsagePreservesRelevantCacheCreationOverflowAtomicity(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsePrice:              true,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{ExprString: "p*2+cc"},
	}
	total := &dto.RealtimeUsage{}
	first := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{
		TextTokens: 1, CachedCreationTokens: math.MaxInt,
	}}
	require.NoError(t, preConsumeUsage(c, info, first, total))
	second := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{
		TextTokens: 1, CachedCreationTokens: 1,
	}}
	require.Error(t, preConsumeUsage(c, info, second, total))
	assert.Equal(t, 1, total.InputTokens)
	assert.Equal(t, math.MaxInt, total.InputTokenDetails.CachedCreationTokens)
}

func TestPreConsumeUsageAggregatesCacheCreationAcrossPricingMasks(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	for _, expr := range []string{"", "p*2", "p*2+cc"} {
		t.Run(expr, func(t *testing.T) {
			info := &relaycommon.RelayInfo{UsePrice: true}
			if expr != "" {
				info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{ExprString: expr}
			}
			total := &dto.RealtimeUsage{}
			for _, aliases := range [][2]int{{100, 99}, {200, 199}} {
				u := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedCreationTokens: aliases[0], CacheWriteTokens: aliases[1]}}
				require.NoError(t, preConsumeUsage(c, info, u, total))
			}
			assert.Equal(t, 2, total.InputTokens)
			assert.Equal(t, 2, total.TotalTokens)
			assert.Equal(t, 300, total.InputTokenDetails.CachedCreationTokens)
		})
	}
}

func TestPreConsumeUsageNormalizesAndBoundsCacheCreationAcrossPricingMasks(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	for _, expr := range []string{"", "p*2", "p*2+cc"} {
		t.Run(expr, func(t *testing.T) {
			info := &relaycommon.RelayInfo{UsePrice: true}
			if expr != "" {
				info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{ExprString: expr}
			}
			total := &dto.RealtimeUsage{}
			for i, aliases := range [][2]int{{-3, -2}, {-1, 7}} {
				u := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedCreationTokens: aliases[0], CacheWriteTokens: aliases[1]}}
				require.NoError(t, preConsumeUsage(c, info, u, total))
				if i == 0 {
					assert.Zero(t, total.InputTokenDetails.CachedCreationTokens)
				}
			}
			assert.Equal(t, 7, total.InputTokenDetails.CachedCreationTokens)
			for _, firstCreation := range []int{math.MaxInt, math.MaxInt - 1} {
				t.Run(fmt.Sprintf("first_%d", firstCreation), func(t *testing.T) {
					boundary := &dto.RealtimeUsage{}
					one := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedCreationTokens: firstCreation}}
					two := &dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, CachedCreationTokens: 1}}
					require.NoError(t, preConsumeUsage(c, info, one, boundary))
					before := deepRealtimeSnapshot(t, boundary)
					if firstCreation == math.MaxInt {
						require.Error(t, preConsumeUsage(c, info, two, boundary))
						assert.Equal(t, before, boundary)
					} else {
						require.NoError(t, preConsumeUsage(c, info, two, boundary))
						assert.Equal(t, math.MaxInt, boundary.InputTokenDetails.CachedCreationTokens)
						assert.Equal(t, 2, boundary.InputTokens)
						assert.Equal(t, 2, boundary.TotalTokens)
					}
				})
			}
		})
	}
}
