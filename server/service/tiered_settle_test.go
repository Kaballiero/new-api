package service

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Claude Sonnet-style tiered expression: standard vs long-context
const sonnetTieredExpr = `p <= 200000 ? tier("standard", p * 1.5 + c * 7.5) : tier("long_context", p * 3 + c * 11.25)`

// Simple flat expression
const flatExpr = `tier("default", p * 2 + c * 10)`

// Expression with cache tokens
const cacheExpr = `tier("default", p * 2 + c * 10 + cr * 0.2 + cc * 2.5 + cc1h * 4)`

// Expression with request probes
const probeExpr = `param("service_tier") == "fast" ? tier("fast", p * 4 + c * 20) : tier("normal", p * 2 + c * 10)`

const testQuotaPerUnit = 500_000.0

func TestBuildRealtimeTieredTokenParams(t *testing.T) {
	value := func(n int) *int { return &n }
	usage := &dto.RealtimeUsage{
		InputTokens:  1200,
		OutputTokens: 350,
		InputTokenDetails: dto.InputTokenDetails{
			CachedTokens: 600,
			TextTokens:   200,
			AudioTokens:  800,
			ImageTokens:  200,
			CachedTokensDetails: &dto.CachedTokenDetails{
				TextTokens:  value(100),
				AudioTokens: value(400),
				ImageTokens: value(100),
			},
		},
		OutputTokenDetails: dto.OutputTokenDetails{AudioTokens: 300, ImageTokens: 50},
	}

	cases := []struct {
		name string
		vars map[string]bool
		p    float64
		ai   float64
		img  float64
		cr   float64
	}{
		{name: "base", vars: map[string]bool{}, p: 1200, ai: 800, img: 200, cr: 600},
		{name: "audio", vars: map[string]bool{"ai": true}, p: 400, ai: 800, img: 200, cr: 600},
		{name: "image", vars: map[string]bool{"img": true}, p: 1000, ai: 800, img: 200, cr: 600},
		{name: "audio_image", vars: map[string]bool{"ai": true, "img": true}, p: 200, ai: 800, img: 200, cr: 600},
		{name: "cache", vars: map[string]bool{"cr": true}, p: 600, ai: 800, img: 200, cr: 600},
		{name: "cache_audio", vars: map[string]bool{"cr": true, "ai": true}, p: 200, ai: 400, img: 200, cr: 600},
		{name: "cache_image", vars: map[string]bool{"cr": true, "img": true}, p: 500, ai: 800, img: 100, cr: 600},
		{name: "cache_audio_image", vars: map[string]bool{"cr": true, "ai": true, "img": true}, p: 100, ai: 400, img: 100, cr: 600},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := BuildRealtimeTieredTokenParams(usage, tc.vars)
			assert.Equal(t, tc.p, params.P)
			assert.Equal(t, tc.ai, params.AI)
			assert.Equal(t, tc.img, params.Img)
			assert.Equal(t, tc.cr, params.CR)
			assert.Equal(t, float64(1200), params.Len)
			assert.Empty(t, RealtimeCacheDiscountIssue(usage, tc.vars))
		})
	}

	for _, tc := range []struct {
		vars map[string]bool
		c    float64
	}{{map[string]bool{}, 350}, {map[string]bool{"ao": true}, 50}, {map[string]bool{"img_o": true}, 300}, {map[string]bool{"ao": true, "img_o": true}, 0}} {
		output := BuildRealtimeTieredTokenParams(usage, tc.vars)
		assert.Equal(t, tc.c, output.C)
		assert.Equal(t, float64(300), output.AO)
		assert.Equal(t, float64(50), output.ImgO)
	}

	usage.CacheDiscountUnavailable = true
	params := BuildRealtimeTieredTokenParams(usage, map[string]bool{"cr": true, "ai": true, "img": true})
	assert.Equal(t, float64(200), params.P)
	assert.Equal(t, float64(800), params.AI)
	assert.Equal(t, float64(200), params.Img)
	assert.Equal(t, float64(0), params.CR)
}

func TestRealtimeCacheDiscountIssue(t *testing.T) {
	value := func(n int) *int { return &n }
	valid := func() *dto.RealtimeUsage {
		return &dto.RealtimeUsage{
			InputTokens: 1000,
			InputTokenDetails: dto.InputTokenDetails{
				CachedTokens:        500,
				AudioTokens:         800,
				CachedTokensDetails: &dto.CachedTokenDetails{AudioTokens: value(400)},
			},
		}
	}

	assert.Empty(t, RealtimeCacheDiscountIssue(valid(), map[string]bool{"cr": true, "ai": true}))
	missing := valid()
	missing.InputTokenDetails.CachedTokensDetails = nil
	assert.Equal(t, "missing_overlap_details", RealtimeCacheDiscountIssue(missing, map[string]bool{"cr": true, "ai": true}))
	inconsistent := valid()
	inconsistent.InputTokenDetails.CachedTokensDetails.AudioTokens = value(801)
	assert.Equal(t, "inconsistent_overlap_details", RealtimeCacheDiscountIssue(inconsistent, map[string]bool{"cr": true, "ai": true}))
	partial := valid()
	partial.InputTokenDetails.CachedTokensDetails.TextTokens = value(200)
	assert.Equal(t, "inconsistent_overlap_details", RealtimeCacheDiscountIssue(partial, map[string]bool{"cr": true, "ai": true}))
	zero := valid()
	zero.InputTokenDetails.CachedTokens = 0
	assert.Equal(t, "inconsistent_overlap_details", RealtimeCacheDiscountIssue(zero, map[string]bool{"cr": true, "ai": true}))
	negative := valid()
	negative.InputTokenDetails.CachedTokens = -1
	assert.Equal(t, "inconsistent_overlap_details", RealtimeCacheDiscountIssue(negative, map[string]bool{"cr": true, "ai": true}))
	assert.Empty(t, RealtimeCacheDiscountIssue(missing, map[string]bool{"ai": true}))
	positivePoison := &dto.RealtimeUsage{
		InputTokens: 200,
		InputTokenDetails: dto.InputTokenDetails{
			CachedTokens: 100,
			TextTokens:   200,
			CachedTokensDetails: &dto.CachedTokenDetails{
				TextTokens:  value(100),
				AudioTokens: value(100),
			},
		},
	}
	assert.Equal(t, "inconsistent_overlap_details", RealtimeCacheDiscountIssue(positivePoison, map[string]bool{"cr": true, "ai": true}))
	tooLarge := valid()
	tooLarge.InputTokenDetails.CachedTokens = 1001
	assert.Equal(t, "inconsistent_overlap_details", RealtimeCacheDiscountIssue(tooLarge, map[string]bool{"cr": true, "ai": true}))
}

func TestValidateRealtimeUsage(t *testing.T) {
	for _, field := range []string{"text", "ai", "img", "text_o", "ao", "img_o"} {
		for _, n := range []int{-1, -100, 101, 200, 100, 70} {
			t.Run(fmt.Sprintf("%s/%d", field, n), func(t *testing.T) {
				u := &dto.RealtimeUsage{TotalTokens: 200, InputTokens: 100, OutputTokens: 100}
				switch field {
				case "text":
					u.InputTokenDetails.TextTokens = n
				case "ai":
					u.InputTokenDetails.AudioTokens = n
				case "img":
					u.InputTokenDetails.ImageTokens = n
				case "text_o":
					u.OutputTokenDetails.TextTokens = n
				case "ao":
					u.OutputTokenDetails.AudioTokens = n
				case "img_o":
					u.OutputTokenDetails.ImageTokens = n
				}
				if n < 0 || n > 100 {
					assertInvalidRealtimeEntryPoints(t, u)
				} else {
					require.NoError(t, ValidateRealtimeUsage(u))
					require.NoError(t, PreWssConsumeQuota(nil, &relaycommon.RelayInfo{UsePrice: true}, u))
				}
			})
		}
	}
	for _, tc := range []struct {
		name  string
		usage *dto.RealtimeUsage
	}{
		{"negative_input", &dto.RealtimeUsage{TotalTokens: 1, InputTokens: -1}},
		{"negative_output", &dto.RealtimeUsage{TotalTokens: 1, OutputTokens: -1}},
		{"negative_total", &dto.RealtimeUsage{TotalTokens: -1}},
		{"zero_total_input", &dto.RealtimeUsage{InputTokens: 1}},
		{"zero_total_output", &dto.RealtimeUsage{OutputTokens: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertInvalidRealtimeEntryPoints(t, tc.usage) })
	}
	require.NoError(t, ValidateRealtimeUsage(&dto.RealtimeUsage{}))
}

func assertInvalidRealtimeEntryPoints(t *testing.T, u *dto.RealtimeUsage) {
	t.Helper()
	require.Error(t, ValidateRealtimeUsage(u))
	for _, usePrice := range []bool{false, true} {
		for _, expr := range []string{"p*2", "p*2+cr", "p*2+cr+ai", "p*2+cr+img"} {
			info := &relaycommon.RelayInfo{UsePrice: usePrice, TieredBillingSnapshot: &billingexpr.BillingSnapshot{ExprString: expr}}
			require.Error(t, PreWssConsumeQuota(nil, info, u), "UsePrice=%t expr=%s", usePrice, expr)
			require.Error(t, PostWssConsumeQuota(nil, info, "", u, ""), "UsePrice=%t expr=%s", usePrice, expr)
		}
	}
}

func TestValidateRealtimeUsageCombinedSubsets(t *testing.T) {
	for _, output := range []bool{false, true} {
		for _, tc := range []struct {
			name                 string
			total, first, second int
			valid                bool
		}{
			{"over_capacity", 100, 60, 41, false},
			{"integer_overflow", math.MaxInt, math.MaxInt, 1, false},
			{"exact_capacity", 100, 60, 40, true},
			{"integer_boundary", math.MaxInt, math.MaxInt - 1, 1, true},
		} {
			t.Run(fmt.Sprintf("output_%t/%s", output, tc.name), func(t *testing.T) {
				u := &dto.RealtimeUsage{TotalTokens: tc.total}
				if output {
					u.OutputTokens = tc.total
					u.OutputTokenDetails = dto.OutputTokenDetails{AudioTokens: tc.first, ImageTokens: tc.second}
				} else {
					u.InputTokens = tc.total
					u.InputTokenDetails = dto.InputTokenDetails{TextTokens: tc.first, AudioTokens: tc.second}
				}
				if tc.valid {
					require.NoError(t, ValidateRealtimeUsage(u))
				} else {
					assertInvalidRealtimeEntryPoints(t, u)
				}
			})
		}
	}
}

func TestRealtimeCacheDiscountFeasibilityMatrix(t *testing.T) {
	value := func(n int) *int { return &n }
	for _, modality := range []string{"ai", "img"} {
		for _, tc := range []struct {
			name                   string
			input, capacity, cache int
			text, modal, other     *int
			reason                 string
		}{
			{"valid_partial", 100, 60, 30, nil, value(20), nil, ""},
			{"valid_complete", 100, 60, 30, value(10), value(20), value(0), ""},
			{"explicit_zero", 100, 60, 30, nil, value(0), nil, ""},
			{"absent_required", 100, 60, 30, nil, nil, nil, "missing_overlap_details"},
			{"negative_cache", 100, 60, -1, nil, value(0), nil, "inconsistent_overlap_details"},
			{"cache_over_input", 100, 60, 101, nil, value(0), nil, "inconsistent_overlap_details"},
			{"negative_modal", 100, 60, 30, nil, value(-1), nil, "inconsistent_overlap_details"},
			{"max_modal", 100, 60, 30, nil, value(math.MaxInt), nil, "inconsistent_overlap_details"},
			{"max_text", 100, 60, 30, value(math.MaxInt), value(20), nil, "inconsistent_overlap_details"},
			{"max_other", 100, 60, 30, nil, value(20), value(math.MaxInt), "inconsistent_overlap_details"},
			{"negative_text", 100, 60, 30, value(-1), value(20), nil, "inconsistent_overlap_details"},
			{"negative_other", 100, 60, 30, nil, value(20), value(-1), "inconsistent_overlap_details"},
			{"modal_over_capacity", 100, 60, 80, nil, value(61), nil, "inconsistent_overlap_details"},
			{"modal_over_cache", 100, 60, 30, nil, value(31), nil, "inconsistent_overlap_details"},
			{"text_over_capacity", 100, 60, 80, value(41), value(30), nil, "inconsistent_overlap_details"},
			{"other_over_capacity", 100, 60, 30, nil, value(20), value(1), "inconsistent_overlap_details"},
			{"known_sum_over_cache", 100, 60, 30, value(20), value(20), nil, "inconsistent_overlap_details"},
			{"impossible_remaining", 100, 60, 70, nil, value(20), nil, "inconsistent_overlap_details"},
			{"complete_mismatch", 100, 60, 30, value(5), value(20), value(0), "inconsistent_overlap_details"},
			{"positive_cache_zero_capacity", 100, 0, 30, value(30), value(1), nil, "inconsistent_overlap_details"},
			{"positive_cache_zero_corrected", 100, 0, 30, value(30), value(0), nil, ""},
			{"zero_cache_irrelevant_noise", 100, 0, 0, value(math.MaxInt), value(-1), value(math.MaxInt), ""},
			{"zero_cache_required_noise", 100, 60, 0, nil, value(1), nil, "inconsistent_overlap_details"},
			{"max_valid", math.MaxInt, 2, math.MaxInt, value(math.MaxInt - 2), value(2), value(0), ""},
			{"max_impossible_remaining", math.MaxInt, 2, math.MaxInt, nil, value(1), nil, "inconsistent_overlap_details"},
			{"max_known_sum_over_cache", math.MaxInt, 2, math.MaxInt - 1, value(math.MaxInt - 2), value(2), nil, "inconsistent_overlap_details"},
		} {
			t.Run(modality+"/"+tc.name, func(t *testing.T) {
				u := &dto.RealtimeUsage{TotalTokens: tc.input, InputTokens: tc.input, InputTokenDetails: dto.InputTokenDetails{CachedTokens: tc.cache, CachedTokensDetails: &dto.CachedTokenDetails{TextTokens: tc.text}}}
				details := u.InputTokenDetails.CachedTokensDetails
				if modality == "ai" {
					u.InputTokenDetails.AudioTokens = tc.capacity
					details.AudioTokens = tc.modal
					details.ImageTokens = tc.other
				} else {
					u.InputTokenDetails.ImageTokens = tc.capacity
					details.ImageTokens = tc.modal
					details.AudioTokens = tc.other
				}
				require.NoError(t, ValidateRealtimeUsage(u))
				vars := map[string]bool{"cr": true, modality: true}
				reason := RealtimeCacheDiscountIssue(u, vars)
				assert.Equal(t, tc.reason, reason)
				u.CacheDiscountUnavailable = reason != ""
				params := BuildRealtimeTieredTokenParams(u, vars)
				if tc.reason == "" {
					assert.Equal(t, float64(tc.cache), params.CR)
				} else {
					assert.Zero(t, params.CR)
				}
			})
		}
	}
}

func TestRealtimeCacheDiscountIgnoresUnusedFacts(t *testing.T) {
	for _, expr := range []string{"p*2", "p*2+cr"} {
		for _, noise := range []int{-1, math.MaxInt} {
			t.Run(fmt.Sprintf("%s/%d", expr, noise), func(t *testing.T) {
				u := &dto.RealtimeUsage{TotalTokens: 100, InputTokens: 100, InputTokenDetails: dto.InputTokenDetails{TextTokens: 100, CachedTokens: 50, CachedTokensDetails: &dto.CachedTokenDetails{TextTokens: &noise, AudioTokens: &noise, ImageTokens: &noise}}}
				vars := billingexpr.UsedVars(expr)
				assert.Empty(t, RealtimeCacheDiscountIssue(u, vars))
				params := BuildRealtimeTieredTokenParams(u, vars)
				if vars["cr"] {
					assert.Equal(t, float64(50), params.P)
					assert.Equal(t, float64(50), params.CR)
				} else {
					assert.Equal(t, float64(100), params.P)
				}
				if !vars["cr"] {
					u.InputTokenDetails.CachedTokens = noise
					assert.Empty(t, RealtimeCacheDiscountIssue(u, vars))
					assert.Equal(t, float64(100), BuildRealtimeTieredTokenParams(u, vars).P)
				}
			})
		}
	}
}

func TestTryTieredWssSettleAcceptsZero(t *testing.T) {
	for _, tc := range []struct {
		name, expr string
		group      float64
	}{
		{"zero", "0", 1}, {"free_branch", `len>0 ? tier("free",0) : tier("paid",p*2)`, 1}, {"zero_group", "p*2", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := makeRelayInfo(tc.expr, tc.group, 1, 0)
			ok, quota, result, err := TryTieredWssSettle(info, billingexpr.TokenParams{P: 1, Len: 1})
			require.True(t, ok)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Zero(t, quota)
		})
	}
}

func TestTryTieredWssSettleRejectsNegativeFinalQuota(t *testing.T) {
	info := makeRelayInfo(`tier("negative", p * -1)`, 1, 1, 0)
	ok, _, result, err := TryTieredWssSettle(info, billingexpr.TokenParams{P: 1})
	require.True(t, ok)
	require.Error(t, err)
	assert.Nil(t, result)
	info = makeRelayInfo(`tier("negative-fraction", p * -0.1)`, 1, 1, 0)
	_, _, _, err = TryTieredWssSettle(info, billingexpr.TokenParams{P: 1})
	require.Error(t, err)
}

func TestPostAudioConsumeQuotaUsesEffectiveBillingGroupRatio(t *testing.T) {
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() { common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch })

	require.NoError(t, model.LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM tokens").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM users").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 901, Username: "audio-effective", Password: "placeholder", Quota: 100, Group: "default"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 901, UserId: 901, Key: "audio-effective-token", Name: "audio-effective-token", RemainQuota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 901, Name: "audio-effective", Type: 1, Key: "test"}).Error)

	c, _ := gin.CreateTestContext(nil)
	c.Set("token_name", "audio-effective-token")
	info := &relaycommon.RelayInfo{
		UserId:          901,
		TokenId:         901,
		TokenKey:        "audio-effective-token",
		OriginModelName: "audio-effective-model",
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 901},
		PriceData: types.PriceData{
			ModelRatio:        1,
			BillingGroupRatio: 2,
			GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1, PromptTokensDetails: dto.InputTokenDetails{TextTokens: 1}}
	PostAudioConsumeQuota(c, info, usage, "")

	var user model.User
	var token model.Token
	var channel model.Channel
	require.NoError(t, model.DB.First(&user, 901).Error)
	require.NoError(t, model.DB.First(&token, 901).Error)
	require.NoError(t, model.DB.First(&channel, 901).Error)
	assert.Equal(t, 98, user.Quota)
	assert.Equal(t, 2, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, 98, token.RemainQuota)
	assert.Equal(t, 2, token.UsedQuota)
	assert.EqualValues(t, 2, channel.UsedQuota)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 2, logs[0].Quota)
	assert.Contains(t, logs[0].Content, "分组倍率 1.00")
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, float64(1), other["group_ratio"])
}

func makeSnapshot(expr string, groupRatio float64, estPrompt, estCompletion int) *billingexpr.BillingSnapshot {
	return &billingexpr.BillingSnapshot{
		BillingMode:               "tiered_expr",
		ExprString:                expr,
		ExprHash:                  billingexpr.ExprHashString(expr),
		GroupRatio:                groupRatio,
		EstimatedPromptTokens:     estPrompt,
		EstimatedCompletionTokens: estCompletion,
		QuotaPerUnit:              testQuotaPerUnit,
	}
}

func makeRelayInfo(expr string, groupRatio float64, estPrompt, estCompletion int) *relaycommon.RelayInfo {
	snap := makeSnapshot(expr, groupRatio, estPrompt, estCompletion)
	cost, trace, _ := billingexpr.RunExpr(expr, billingexpr.TokenParams{P: float64(estPrompt), C: float64(estCompletion)})
	quotaBeforeGroup := cost / 1_000_000 * testQuotaPerUnit
	snap.EstimatedQuotaBeforeGroup = quotaBeforeGroup
	snap.EstimatedQuotaAfterGroup = billingexpr.QuotaRound(quotaBeforeGroup * groupRatio)
	snap.EstimatedTier = trace.MatchedTier
	return &relaycommon.RelayInfo{
		TieredBillingSnapshot: snap,
		FinalPreConsumedQuota: snap.EstimatedQuotaAfterGroup,
	}
}

// ---------------------------------------------------------------------------
// Existing tests (preserved)
// ---------------------------------------------------------------------------

func TestTryTieredSettleUsesFrozenRequestInput(t *testing.T) {
	exprStr := `param("service_tier") == "fast" ? tier("fast", p * 2) : tier("normal", p)`
	relayInfo := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                exprStr,
			ExprHash:                  billingexpr.ExprHashString(exprStr),
			GroupRatio:                1.0,
			EstimatedPromptTokens:     100,
			EstimatedCompletionTokens: 0,
			EstimatedQuotaAfterGroup:  50,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"service_tier":"fast"}`),
		},
	}

	ok, quota, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 100})
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	// fast: p*2 = 200; quota = 200 / 1M * 500K = 100
	if quota != 100 {
		t.Fatalf("quota = %d, want 100", quota)
	}
	if result == nil || result.MatchedTier != "fast" {
		t.Fatalf("matched tier = %v, want fast", result)
	}
}

func TestTryTieredWssSettleRejectsFinalClamp(t *testing.T) {
	info := makeRelayInfo(`p > 10000 ? 1e20 : 0`, 1, 0, 0)
	ok, _, _, err := TryTieredWssSettle(info, billingexpr.TokenParams{P: 10001})
	require.True(t, ok)
	require.Error(t, err)
	assert.NotNil(t, info.QuotaClamp)
}

func TestRefreshTieredBillingGroupRecomposesSelectedPureGroup(t *testing.T) {
	info := &relaycommon.RelayInfo{
		BillingFXRate:   90,
		BillingFXFactor: 0.9,
		PriceData: types.PriceData{
			GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 3},
			BillingGroupRatio: 1.8,
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.8,
			EstimatedQuotaBeforeGroup: 100,
			EstimatedQuotaAfterGroup:  180,
		},
	}

	snapshot, err := refreshTieredBillingGroup(info)
	require.NoError(t, err)
	require.Equal(t, 2.7, snapshot.GroupRatio)
	require.Equal(t, 270, snapshot.EstimatedQuotaAfterGroup)
}

func TestTryTieredSettleFallsBackToFrozenPreConsumeOnExprError(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		FinalPreConsumedQuota: 321,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:              "tiered_expr",
			ExprString:               `invalid +-+ expr`,
			ExprHash:                 billingexpr.ExprHashString(`invalid +-+ expr`),
			GroupRatio:               1.0,
			EstimatedQuotaAfterGroup: 123,
		},
	}

	ok, quota, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 100})
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	if quota != 321 {
		t.Fatalf("quota = %d, want 321", quota)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

// ---------------------------------------------------------------------------
// Pre-consume vs Post-consume consistency
// ---------------------------------------------------------------------------

func TestTryTieredSettle_PreConsumeMatchesPostConsume(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 1000, 500)
	params := billingexpr.TokenParams{P: 1000, C: 500}

	ok, quota, _ := TryTieredSettle(info, params)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500
	if quota != 3500 {
		t.Fatalf("quota = %d, want 3500", quota)
	}
	if quota != info.FinalPreConsumedQuota {
		t.Fatalf("pre-consume %d != post-consume %d", info.FinalPreConsumedQuota, quota)
	}
}

func TestTryTieredSettle_PostConsumeOverPreConsume(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 1000, 500)
	preConsumed := info.FinalPreConsumedQuota // 3500

	// Actual usage is higher than estimated
	params := billingexpr.TokenParams{P: 2000, C: 1000}
	ok, quota, _ := TryTieredSettle(info, params)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 14000; quota = 14000 / 1M * 500K = 7000
	if quota != 7000 {
		t.Fatalf("quota = %d, want 7000", quota)
	}
	if quota <= preConsumed {
		t.Fatalf("expected supplement: actual %d should > pre-consumed %d", quota, preConsumed)
	}
}

func TestTryTieredSettle_PostConsumeUnderPreConsume(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 1000, 500)
	preConsumed := info.FinalPreConsumedQuota // 3500

	// Actual usage is lower than estimated
	params := billingexpr.TokenParams{P: 100, C: 50}
	ok, quota, _ := TryTieredSettle(info, params)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 700; quota = 700 / 1M * 500K = 350
	if quota != 350 {
		t.Fatalf("quota = %d, want 350", quota)
	}
	if quota >= preConsumed {
		t.Fatalf("expected refund: actual %d should < pre-consumed %d", quota, preConsumed)
	}
}

// ---------------------------------------------------------------------------
// Tiered boundary conditions
// ---------------------------------------------------------------------------

func TestTryTieredSettle_ExactBoundary(t *testing.T) {
	info := makeRelayInfo(sonnetTieredExpr, 1.0, 200000, 1000)

	// p == 200000 => standard tier (p <= 200000)
	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 200000, C: 1000})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// standard: p*1.5 + c*7.5 = 307500; quota = 307500 / 1M * 500K = 153750
	if quota != 153750 {
		t.Fatalf("quota = %d, want 153750", quota)
	}
	if result.MatchedTier != "standard" {
		t.Fatalf("tier = %s, want standard", result.MatchedTier)
	}
}

func TestTryTieredSettle_BoundaryPlusOne(t *testing.T) {
	info := makeRelayInfo(sonnetTieredExpr, 1.0, 200000, 1000)

	// p == 200001 => crosses to long_context tier
	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 200001, C: 1000})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// long_context: p*3 + c*11.25 = 611253; quota = round(611253 / 1M * 500K) = 305627
	if quota != 305627 {
		t.Fatalf("quota = %d, want 305627", quota)
	}
	if result.MatchedTier != "long_context" {
		t.Fatalf("tier = %s, want long_context", result.MatchedTier)
	}
	if !result.CrossedTier {
		t.Fatal("expected CrossedTier = true")
	}
}

func TestTryTieredSettle_ZeroTokens(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 0, 0)

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 0, C: 0})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	if quota != 0 {
		t.Fatalf("quota = %d, want 0", quota)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestTryTieredSettle_HugeTokens(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 10000000, 5000000)

	ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 10000000, C: 5000000})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 70000000; quota = 70000000 / 1M * 500K = 35000000
	if quota != 35000000 {
		t.Fatalf("quota = %d, want 35000000", quota)
	}
}

func TestTryTieredSettle_CacheTokensAffectSettlement(t *testing.T) {
	info := makeRelayInfo(cacheExpr, 1.0, 1000, 500)

	// Without cache tokens
	ok1, quota1, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok1 {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500

	// With cache tokens
	ok2, quota2, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500, CR: 10000, CC: 5000, CC1h: 2000})
	if !ok2 {
		t.Fatal("expected tiered settle")
	}
	// 2000 + 5000 + 2000 + 12500 + 8000 = 29500; quota = 29500 / 1M * 500K = 14750

	if quota2 <= quota1 {
		t.Fatalf("cache tokens should increase quota: without=%d, with=%d", quota1, quota2)
	}
	if quota1 != 3500 {
		t.Fatalf("no-cache quota = %d, want 3500", quota1)
	}
	if quota2 != 14750 {
		t.Fatalf("cache quota = %d, want 14750", quota2)
	}
}

// ---------------------------------------------------------------------------
// Request probe tests
// ---------------------------------------------------------------------------

func TestTryTieredSettle_RequestProbeInfluencesBilling(t *testing.T) {
	info := makeRelayInfo(probeExpr, 1.0, 1000, 500)
	info.BillingRequestInput = &billingexpr.RequestInput{
		Body: []byte(`{"service_tier":"fast"}`),
	}

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// fast: p*4 + c*20 = 14000; quota = 14000 / 1M * 500K = 7000
	if quota != 7000 {
		t.Fatalf("quota = %d, want 7000", quota)
	}
	if result.MatchedTier != "fast" {
		t.Fatalf("tier = %s, want fast", result.MatchedTier)
	}
}

func TestTryTieredSettle_NoRequestInput_FallsBackToDefault(t *testing.T) {
	info := makeRelayInfo(probeExpr, 1.0, 1000, 500)
	// No BillingRequestInput set — param("service_tier") returns nil, not "fast"

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// normal: p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500
	if quota != 3500 {
		t.Fatalf("quota = %d, want 3500", quota)
	}
	if result.MatchedTier != "normal" {
		t.Fatalf("tier = %s, want normal", result.MatchedTier)
	}
}

// ---------------------------------------------------------------------------
// Group ratio tests
// ---------------------------------------------------------------------------

type recordingBillingSettler struct {
	preConsumedQuota int
	reserveTargets   []int
}

func (*recordingBillingSettler) Settle(int) error { return nil }

func (*recordingBillingSettler) Refund(*gin.Context) {}

func (*recordingBillingSettler) NeedsRefund() bool { return false }

func (s *recordingBillingSettler) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *recordingBillingSettler) Reserve(targetQuota int) error {
	s.reserveTargets = append(s.reserveTargets, targetQuota)
	if targetQuota > s.preConsumedQuota {
		s.preConsumedQuota = targetQuota
	}
	return nil
}

func TestPrepareTieredBillingForSelectedGroupUpdatesReservation(t *testing.T) {
	const expr = `tier("base", p)`
	billing := &recordingBillingSettler{preConsumedQuota: 50_000}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}

	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))
	require.Equal(t, []int{100_000}, billing.reserveTargets)
	assert.Equal(t, 100_000, billing.preConsumedQuota)
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0.20, relayInfo.TieredBillingSnapshot.GroupRatio)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
}

func TestPrepareTieredBillingForSelectedGroupRetainsClampCause(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: float64(common.MaxQuota) * 2,
			EstimatedQuotaAfterGroup:  common.MaxQuota,
		},
		PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}},
	}

	apiErr := PrepareTieredBillingForSelectedGroup(nil, relayInfo)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, apiErr, &clamp)
	assert.True(t, errors.Is(apiErr.Err, clamp))
	assert.Same(t, clamp, relayInfo.QuotaClamp)
}

func TestPrepareTieredBillingForSelectedGroupStartsBillingAfterFreeGroup(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	const userID = 700
	seedUser(t, userID, 500_000)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		OriginModelName: "gpt-test",
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                `tier("base", p)`,
			ExprHash:                  billingexpr.ExprHashString(`tier("base", p)`),
			GroupRatio:                0,
			EstimatedQuotaBeforeGroup: 500_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			FreeModel:      true,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}
	ctx, _ := gin.CreateTestContext(nil)

	require.Nil(t, PrepareTieredBillingForSelectedGroup(ctx, relayInfo))
	require.NotNil(t, relayInfo.Billing)
	assert.False(t, relayInfo.PriceData.FreeModel, "FreeModel must be cleared after switching to a paid group")
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0.20, relayInfo.TieredBillingSnapshot.GroupRatio)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)

	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, 400_000, userQuota)
}

func TestPrepareTieredBillingForSelectedGroupPaidToFreeKeepsFreeModelFalse(t *testing.T) {
	const expr = `tier("base", p)`
	billing := &recordingBillingSettler{preConsumedQuota: 50_000}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0},
		},
	}

	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))

	// Pre-consume did happen under the paid group, so FreeModel stays false;
	// settlement already yields 0 for GroupRatio == 0 and the session refunds.
	assert.False(t, relayInfo.PriceData.FreeModel)
	assert.Empty(t, billing.reserveTargets)
	assert.Equal(t, 50_000, relayInfo.FinalPreConsumedQuota)
}

func TestPrepareTieredBillingForSelectedGroupTopUpArrearsAllowsNegativeBalance(t *testing.T) {
	truncate(t)

	const userID = 701
	// Balance covers the initial 50k pre-consume (already deducted before this
	// test's seed) but not the 50k top-up to the more expensive retry group.
	// The top-up must NOT abort the request: the full delta is deducted, the
	// uncovered 30k becomes arrears (negative balance), mirroring how
	// settlement charges a positive delta unconditionally.
	seedUser(t, userID, 20_000)

	relayInfo := &relaycommon.RelayInfo{
		UserId:                userID,
		IsPlayground:          true,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                `tier("base", p)`,
			ExprHash:                  billingexpr.ExprHashString(`tier("base", p)`),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}
	session := &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: 50_000},
		preConsumedQuota: 50_000,
	}
	relayInfo.Billing = session

	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))

	// Full reservation recorded; wallet charged the full delta into arrears.
	assert.Equal(t, 100_000, session.GetPreConsumedQuota())
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, -30_000, userQuota)

	// Settlement still reconciles against the full reservation: actual 80k
	// refunds the 20k over-reserve, landing at seed - (actual - initial) = -10k.
	require.NoError(t, session.Settle(80_000))
	userQuota, err = model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, -10_000, userQuota)
}

func TestBillingSessionReserveWalletTopUpDecrementsBalance(t *testing.T) {
	truncate(t)

	const userID = 702
	seedUser(t, userID, 500_000)

	relayInfo := &relaycommon.RelayInfo{
		UserId:       userID,
		IsPlayground: true,
	}
	session := &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: 50_000},
		preConsumedQuota: 50_000,
	}

	require.NoError(t, session.Reserve(100_000))

	assert.Equal(t, 100_000, session.GetPreConsumedQuota())
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, 450_000, userQuota)
}

func TestTryTieredSettleUsesFinalGroupAfterRetry(t *testing.T) {
	const expr = `tier("base", p)`
	tests := []struct {
		name            string
		finalGroupRatio float64
		wantQuota       int
	}{
		{name: "more expensive final group", finalGroupRatio: 0.20, wantQuota: 100_000},
		{name: "free final group", finalGroupRatio: 0, wantQuota: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relayInfo := &relaycommon.RelayInfo{
				Billing:               &recordingBillingSettler{preConsumedQuota: 50_000},
				FinalPreConsumedQuota: 50_000,
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{
					BillingMode:               "tiered_expr",
					ExprString:                expr,
					ExprHash:                  billingexpr.ExprHashString(expr),
					GroupRatio:                0.10,
					EstimatedQuotaBeforeGroup: 500_000,
					EstimatedQuotaAfterGroup:  50_000,
					QuotaPerUnit:              testQuotaPerUnit,
				},
				PriceData: types.PriceData{
					GroupRatioInfo: types.GroupRatioInfo{GroupRatio: tt.finalGroupRatio},
				},
			}

			require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))
			ok, quota, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 1_000_000})

			require.True(t, ok)
			require.NotNil(t, result)
			assert.Equal(t, tt.wantQuota, quota)
			assert.Equal(t, tt.finalGroupRatio, relayInfo.TieredBillingSnapshot.GroupRatio)
			assert.Equal(t, tt.wantQuota, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
		})
	}
}

func TestTryTieredSettle_GroupRatioScaling(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.5, 1000, 500)

	ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// exprCost = 7000, quotaBeforeGroup = 3500, afterGroup = round(3500 * 1.5) = 5250
	if quota != 5250 {
		t.Fatalf("quota = %d, want 5250", quota)
	}
}

func TestTryTieredSettle_GroupRatioZero(t *testing.T) {
	info := makeRelayInfo(flatExpr, 0, 1000, 500)

	ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	if quota != 0 {
		t.Fatalf("quota = %d, want 0 (group ratio = 0)", quota)
	}
}

// ---------------------------------------------------------------------------
// Ratio mode (negative tests) — TryTieredSettle must return false
// ---------------------------------------------------------------------------

func TestTryTieredSettle_RatioMode_NilSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: nil,
	}

	ok, _, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if ok {
		t.Fatal("expected TryTieredSettle to return false when snapshot is nil")
	}
}

func TestTryTieredSettle_RatioMode_WrongBillingMode(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "ratio",
			ExprString:  flatExpr,
			ExprHash:    billingexpr.ExprHashString(flatExpr),
			GroupRatio:  1.0,
		},
	}

	ok, _, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if ok {
		t.Fatal("expected TryTieredSettle to return false for ratio billing mode")
	}
}

func TestTryTieredSettle_RatioMode_EmptyBillingMode(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "",
			ExprString:  flatExpr,
			ExprHash:    billingexpr.ExprHashString(flatExpr),
			GroupRatio:  1.0,
		},
	}

	ok, _, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if ok {
		t.Fatal("expected TryTieredSettle to return false for empty billing mode")
	}
}

// ---------------------------------------------------------------------------
// Fallback tests
// ---------------------------------------------------------------------------

func TestTryTieredSettle_ErrorFallbackToEstimatedQuotaAfterGroup(t *testing.T) {
	info := &relaycommon.RelayInfo{
		FinalPreConsumedQuota: 0,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:              "tiered_expr",
			ExprString:               `invalid expr!!!`,
			ExprHash:                 billingexpr.ExprHashString(`invalid expr!!!`),
			GroupRatio:               1.0,
			EstimatedQuotaAfterGroup: 999,
		},
	}

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 100})
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	// FinalPreConsumedQuota is 0, should fall back to EstimatedQuotaAfterGroup
	if quota != 999 {
		t.Fatalf("quota = %d, want 999", quota)
	}
	if result != nil {
		t.Fatal("result should be nil on error fallback")
	}
}

// ---------------------------------------------------------------------------
// BuildTieredTokenParams: token normalization and ratio parity tests
// ---------------------------------------------------------------------------

func tieredQuota(exprStr string, usage *dto.Usage, isClaudeSemantic bool, groupRatio float64) float64 {
	usedVars := billingexpr.UsedVars(exprStr)
	params := BuildTieredTokenParams(usage, isClaudeSemantic, usedVars)
	cost, _, _ := billingexpr.RunExpr(exprStr, params)
	return cost / 1_000_000 * testQuotaPerUnit * groupRatio
}

func ratioQuota(usage *dto.Usage, isClaudeSemantic bool, modelRatio, completionRatio, cacheRatio, imageRatio, groupRatio float64) float64 {
	dPromptTokens := decimal.NewFromInt(int64(usage.PromptTokens))
	dCacheTokens := decimal.NewFromInt(int64(usage.PromptTokensDetails.CachedTokens))
	dCcTokens := decimal.NewFromInt(int64(usage.PromptTokensDetails.CachedCreationTokens))
	dImgTokens := decimal.NewFromInt(int64(usage.PromptTokensDetails.ImageTokens))
	dCompletionTokens := decimal.NewFromInt(int64(usage.CompletionTokens))
	dModelRatio := decimal.NewFromFloat(modelRatio)
	dCompletionRatio := decimal.NewFromFloat(completionRatio)
	dCacheRatio := decimal.NewFromFloat(cacheRatio)
	dImageRatio := decimal.NewFromFloat(imageRatio)
	dGroupRatio := decimal.NewFromFloat(groupRatio)

	baseTokens := dPromptTokens
	if !isClaudeSemantic {
		baseTokens = baseTokens.Sub(dCacheTokens)
		baseTokens = baseTokens.Sub(dCcTokens)
		baseTokens = baseTokens.Sub(dImgTokens)
	}

	cachedTokensWithRatio := dCacheTokens.Mul(dCacheRatio)
	imageTokensWithRatio := dImgTokens.Mul(dImageRatio)
	promptQuota := baseTokens.Add(cachedTokensWithRatio).Add(imageTokensWithRatio)
	completionQuota := dCompletionTokens.Mul(dCompletionRatio)
	ratio := dModelRatio.Mul(dGroupRatio)

	result := promptQuota.Add(completionQuota).Mul(ratio)
	f, _ := result.Float64()
	return f
}

func TestBuildTieredTokenParams_GPT_WithCache(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 200,
			TextTokens:   800,
		},
	}
	expr := `tier("base", p * 2.5 + c * 15 + cr * 0.25)`
	got := tieredQuota(expr, usage, false, 1.0)
	// P=800, C=500, CR=200 → (800*2.5 + 500*15 + 200*0.25) * 0.5 = 4775
	want := 4775.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("quota = %f, want %f", got, want)
	}
}

func TestBuildTieredTokenParams_GPT_NoCacheVar(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 200,
			TextTokens:   800,
		},
	}
	expr := `tier("base", p * 2.5 + c * 15)`
	got := tieredQuota(expr, usage, false, 1.0)
	// No cr → P=1000 (cache stays in P), C=500 → (1000*2.5 + 500*15) * 0.5 = 5000
	want := 5000.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("quota = %f, want %f", got, want)
	}
}

func TestBuildTieredTokenParams_GPT_WithImage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		PromptTokensDetails: dto.InputTokenDetails{
			ImageTokens: 200,
			TextTokens:  800,
		},
	}
	expr := `tier("base", p * 2 + c * 8 + img * 2.5)`
	got := tieredQuota(expr, usage, false, 1.0)
	// P=800, C=500, Img=200 → (800*2 + 500*8 + 200*2.5) * 0.5 = 3050
	want := 3050.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("quota = %f, want %f", got, want)
	}
}

func TestBuildTieredTokenParams_Claude_WithCache(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     800,
		CompletionTokens: 500,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 200,
			TextTokens:   800,
		},
	}
	expr := `tier("base", p * 3 + c * 15 + cr * 0.3)`
	got := tieredQuota(expr, usage, true, 1.0)
	// Claude: P=800 (no subtraction), C=500, CR=200 → (800*3 + 500*15 + 200*0.3) * 0.5 = 4980
	want := 4980.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("quota = %f, want %f", got, want)
	}
}

func TestBuildTieredTokenParams_GPT_AudioOutput(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 600,
		CompletionTokenDetails: dto.OutputTokenDetails{
			AudioTokens: 100,
			TextTokens:  500,
		},
	}
	expr := `tier("base", p * 2 + c * 10 + ao * 50)`
	got := tieredQuota(expr, usage, false, 1.0)
	// C=600-100=500, AO=100 → (1000*2 + 500*10 + 100*50) * 0.5 = 6000
	want := 6000.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("quota = %f, want %f", got, want)
	}
}

func TestBuildTieredTokenParams_GPT_AudioOutputNoVar(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 600,
		CompletionTokenDetails: dto.OutputTokenDetails{
			AudioTokens: 100,
			TextTokens:  500,
		},
	}
	expr := `tier("base", p * 2 + c * 10)`
	got := tieredQuota(expr, usage, false, 1.0)
	// No ao → C=600 (audio stays in C) → (1000*2 + 600*10) * 0.5 = 4000
	want := 4000.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("quota = %f, want %f", got, want)
	}
}

func TestBuildTieredTokenParams_ParityWithRatio(t *testing.T) {
	// GPT-5.4 prices: input=$2.5, output=$15, cacheRead=$0.25
	// Ratio equivalents: modelRatio=1.25, completionRatio=6, cacheRatio=0.1
	usage := &dto.Usage{
		PromptTokens:     10000,
		CompletionTokens: 2000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3000,
			TextTokens:   7000,
		},
	}
	expr := `tier("base", p * 2.5 + c * 15 + cr * 0.25)`

	for _, gr := range []float64{1.0, 1.5, 2.0, 0.5} {
		tq := tieredQuota(expr, usage, false, gr)
		rq := ratioQuota(usage, false, 1.25, 6, 0.1, 0, gr)

		if math.Abs(tq-rq) > 0.01 {
			t.Fatalf("groupRatio=%v: tiered=%f ratio=%f (mismatch)", gr, tq, rq)
		}
	}
}

func TestBuildTieredTokenParams_ParityWithRatio_Image(t *testing.T) {
	// gpt-image-1-mini prices: input=$2, output=$8, image=$2.5
	// Ratio equivalents: modelRatio=1, completionRatio=4, imageRatio=1.25
	usage := &dto.Usage{
		PromptTokens:     5000,
		CompletionTokens: 4000,
		PromptTokensDetails: dto.InputTokenDetails{
			ImageTokens: 1000,
			TextTokens:  4000,
		},
	}
	expr := `tier("base", p * 2 + c * 8 + img * 2.5)`

	tq := tieredQuota(expr, usage, false, 1.0)
	rq := ratioQuota(usage, false, 1.0, 4, 0, 1.25, 1.0)

	if math.Abs(tq-rq) > 0.01 {
		t.Fatalf("tiered=%f ratio=%f (mismatch)", tq, rq)
	}
}

// ---------------------------------------------------------------------------
// BuildTieredTokenParams: Len computation tests
// ---------------------------------------------------------------------------

func TestBuildTieredTokenParams_Len_GPT(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     10000,
		CompletionTokens: 2000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3000,
			TextTokens:   7000,
		},
	}
	expr := `tier("base", p * 2.5 + c * 15 + cr * 0.25)`
	usedVars := billingexpr.UsedVars(expr)
	params := BuildTieredTokenParams(usage, false, usedVars)

	// Non-Claude: Len = raw PromptTokens
	if params.Len != 10000 {
		t.Fatalf("Len = %f, want 10000 (raw PromptTokens)", params.Len)
	}
	// P should be reduced by cache
	if params.P != 7000 {
		t.Fatalf("P = %f, want 7000 (PromptTokens - CachedTokens)", params.P)
	}
}

func TestBuildTieredTokenParams_Len_Claude(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     5000,
		CompletionTokens: 2000,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3000,
			TextTokens:   5000,
		},
		ClaudeCacheCreation5mTokens: 1000,
		ClaudeCacheCreation1hTokens: 500,
	}
	expr := `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`
	usedVars := billingexpr.UsedVars(expr)
	params := BuildTieredTokenParams(usage, true, usedVars)

	// Claude: Len = PromptTokens + CachedTokens + CacheCreation5m + CacheCreation1h
	wantLen := float64(5000 + 3000 + 1000 + 500)
	if params.Len != wantLen {
		t.Fatalf("Len = %f, want %f (text + cache read + cache creation)", params.Len, wantLen)
	}
	// Claude: P is not reduced (isClaudeUsageSemantic = true)
	if params.P != 5000 {
		t.Fatalf("P = %f, want 5000 (no subtraction for Claude)", params.P)
	}
}

func TestBuildTieredTokenParams_Len_TierCondition(t *testing.T) {
	// Test that len-based tier conditions work correctly when p is reduced by cache
	usage := &dto.Usage{
		PromptTokens:     300000,
		CompletionTokens: 5000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 250000,
			TextTokens:   50000,
		},
	}
	expr := `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6)`
	usedVars := billingexpr.UsedVars(expr)
	params := BuildTieredTokenParams(usage, false, usedVars)

	// Len = 300000 (raw prompt), P = 50000 (300000 - 250000 cache)
	if params.Len != 300000 {
		t.Fatalf("Len = %f, want 300000", params.Len)
	}
	if params.P != 50000 {
		t.Fatalf("P = %f, want 50000", params.P)
	}

	// Run expression: len=300000 > 200000, so long_context tier
	cost, trace, err := billingexpr.RunExpr(expr, params)
	if err != nil {
		t.Fatal(err)
	}
	if trace.MatchedTier != "long_context" {
		t.Fatalf("tier = %s, want long_context (len=300000 but p=50000)", trace.MatchedTier)
	}
	// long_context: 50000*6 + 5000*22.5 + 250000*0.6
	wantCost := 50000.0*6 + 5000*22.5 + 250000*0.6
	if math.Abs(cost-wantCost) > 1e-6 {
		t.Fatalf("cost = %f, want %f", cost, wantCost)
	}
}

const complexTieredExpr = `p <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6 + img * 3 + img_o * 30 + ai * 10 + ao * 40) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12 + img * 6 + img_o * 60 + ai * 20 + ao * 80)`

func randomUsage(rng *rand.Rand) *dto.Usage {
	cacheRead := int(rng.Float64() * 50000)
	cacheCreate := int(rng.Float64() * 10000)
	imgIn := int(rng.Float64() * 5000)
	audioIn := int(rng.Float64() * 3000)
	prompt := int(rng.Float64()*300000) + cacheRead + cacheCreate + imgIn + audioIn

	imgOut := int(rng.Float64() * 2000)
	audioOut := int(rng.Float64() * 1000)
	completion := int(rng.Float64()*50000) + imgOut + audioOut

	return &dto.Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         cacheRead,
			CachedCreationTokens: cacheCreate,
			ImageTokens:          imgIn,
			AudioTokens:          audioIn,
			TextTokens:           prompt - cacheRead - cacheCreate - imgIn - audioIn,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			ImageTokens: imgOut,
			AudioTokens: audioOut,
			TextTokens:  completion - imgOut - audioOut,
		},
	}
}

func BenchmarkTieredBilling_ComplexExpr(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	usedVars := billingexpr.UsedVars(complexTieredExpr)
	usages := make([]*dto.Usage, 1000)
	for i := range usages {
		usages[i] = randomUsage(rng)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		usage := usages[i%len(usages)]
		params := BuildTieredTokenParams(usage, false, usedVars)
		billingexpr.RunExpr(complexTieredExpr, params)
	}
}

func BenchmarkRatioBilling_Equivalent(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	usages := make([]*dto.Usage, 1000)
	for i := range usages {
		usages[i] = randomUsage(rng)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		usage := usages[i%len(usages)]
		ratioQuota(usage, false, 1.5, 5.0, 0.1, 1.0, 1.5)
	}
}

func BenchmarkTieredBilling_Parallel(b *testing.B) {
	usedVars := billingexpr.UsedVars(complexTieredExpr)

	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			usage := randomUsage(rng)
			params := BuildTieredTokenParams(usage, false, usedVars)
			billingexpr.RunExpr(complexTieredExpr, params)
		}
	})
}

func BenchmarkRatioBilling_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			usage := randomUsage(rng)
			ratioQuota(usage, false, 1.5, 5.0, 0.1, 1.0, 1.5)
		}
	})
}
