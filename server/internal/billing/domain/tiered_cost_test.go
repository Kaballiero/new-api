package domain_test

import (
	"fmt"
	"math"
	"testing"

	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRealtimeTieredTokenParams(t *testing.T) {
	value := func(n int) *int { return &n }
	usage := &domain.Usage{
		InputTokens:  1200,
		OutputTokens: 350,
		InputTokenDetails: domain.TokenDetails{
			CachedTokens: 600,
			TextTokens:   200,
			AudioTokens:  800,
			ImageTokens:  200,
			CachedTokensDetails: &domain.CachedDetails{
				TextTokens:  value(100),
				AudioTokens: value(400),
				ImageTokens: value(100),
			},
		},
		OutputTokenDetails: domain.TokenDetails{AudioTokens: 300, ImageTokens: 50},
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
			params := domain.BuildRealtimeTieredTokenParams(usage, tc.vars)
			assert.Equal(t, tc.p, params.P)
			assert.Equal(t, tc.ai, params.AI)
			assert.Equal(t, tc.img, params.Img)
			assert.Equal(t, tc.cr, params.CR)
			assert.Equal(t, float64(1200), params.Len)
			assert.Empty(t, domain.RealtimeCacheDiscountIssue(usage, tc.vars))
		})
	}

	for _, tc := range []struct {
		vars map[string]bool
		c    float64
	}{{map[string]bool{}, 350}, {map[string]bool{"ao": true}, 50}, {map[string]bool{"img_o": true}, 300}, {map[string]bool{"ao": true, "img_o": true}, 0}} {
		output := domain.BuildRealtimeTieredTokenParams(usage, tc.vars)
		assert.Equal(t, tc.c, output.C)
		assert.Equal(t, float64(300), output.AO)
		assert.Equal(t, float64(50), output.ImgO)
	}

	usage.CacheDiscountUnavailable = true
	params := domain.BuildRealtimeTieredTokenParams(usage, map[string]bool{"cr": true, "ai": true, "img": true})
	assert.Equal(t, float64(200), params.P)
	assert.Equal(t, float64(800), params.AI)
	assert.Equal(t, float64(200), params.Img)
	assert.Equal(t, float64(0), params.CR)
}

func TestRealtimeCacheDiscountIssue(t *testing.T) {
	value := func(n int) *int { return &n }
	valid := func() *domain.Usage {
		return &domain.Usage{
			InputTokens: 1000,
			InputTokenDetails: domain.TokenDetails{
				CachedTokens:        500,
				AudioTokens:         800,
				CachedTokensDetails: &domain.CachedDetails{AudioTokens: value(400)},
			},
		}
	}

	assert.Empty(t, domain.RealtimeCacheDiscountIssue(valid(), map[string]bool{"cr": true, "ai": true}))
	missing := valid()
	missing.InputTokenDetails.CachedTokensDetails = nil
	assert.Equal(t, "missing_overlap_details", domain.RealtimeCacheDiscountIssue(missing, map[string]bool{"cr": true, "ai": true}))
	inconsistent := valid()
	inconsistent.InputTokenDetails.CachedTokensDetails.AudioTokens = value(801)
	assert.Equal(t, "inconsistent_overlap_details", domain.RealtimeCacheDiscountIssue(inconsistent, map[string]bool{"cr": true, "ai": true}))
	partial := valid()
	partial.InputTokenDetails.CachedTokensDetails.TextTokens = value(200)
	assert.Equal(t, "inconsistent_overlap_details", domain.RealtimeCacheDiscountIssue(partial, map[string]bool{"cr": true, "ai": true}))
	zero := valid()
	zero.InputTokenDetails.CachedTokens = 0
	assert.Equal(t, "inconsistent_overlap_details", domain.RealtimeCacheDiscountIssue(zero, map[string]bool{"cr": true, "ai": true}))
	negative := valid()
	negative.InputTokenDetails.CachedTokens = -1
	assert.Equal(t, "inconsistent_overlap_details", domain.RealtimeCacheDiscountIssue(negative, map[string]bool{"cr": true, "ai": true}))
	assert.Empty(t, domain.RealtimeCacheDiscountIssue(missing, map[string]bool{"ai": true}))
	positivePoison := &domain.Usage{
		InputTokens: 200,
		InputTokenDetails: domain.TokenDetails{
			CachedTokens: 100,
			TextTokens:   200,
			CachedTokensDetails: &domain.CachedDetails{
				TextTokens:  value(100),
				AudioTokens: value(100),
			},
		},
	}
	assert.Equal(t, "inconsistent_overlap_details", domain.RealtimeCacheDiscountIssue(positivePoison, map[string]bool{"cr": true, "ai": true}))
	tooLarge := valid()
	tooLarge.InputTokenDetails.CachedTokens = 1001
	assert.Equal(t, "inconsistent_overlap_details", domain.RealtimeCacheDiscountIssue(tooLarge, map[string]bool{"cr": true, "ai": true}))
}

func TestValidateRealtimeUsage(t *testing.T) {
	for _, field := range []string{"text", "ai", "img", "text_o", "ao", "img_o"} {
		for _, n := range []int{-1, -100, 101, 200, 100, 70} {
			t.Run(fmt.Sprintf("%s/%d", field, n), func(t *testing.T) {
				u := &domain.Usage{TotalTokens: 200, InputTokens: 100, OutputTokens: 100}
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
					require.Error(t, domain.ValidateRealtimeUsage(u))
				} else {
					require.NoError(t, domain.ValidateRealtimeUsage(u))
				}
			})
		}
	}
	for _, tc := range []struct {
		name  string
		usage *domain.Usage
	}{
		{"negative_input", &domain.Usage{TotalTokens: 1, InputTokens: -1}},
		{"negative_output", &domain.Usage{TotalTokens: 1, OutputTokens: -1}},
		{"negative_total", &domain.Usage{TotalTokens: -1}},
		{"zero_total_input", &domain.Usage{InputTokens: 1}},
		{"zero_total_output", &domain.Usage{OutputTokens: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, domain.ValidateRealtimeUsage(tc.usage)) })
	}
	require.NoError(t, domain.ValidateRealtimeUsage(&domain.Usage{}))
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
				u := &domain.Usage{TotalTokens: tc.total}
				if output {
					u.OutputTokens = tc.total
					u.OutputTokenDetails = domain.TokenDetails{AudioTokens: tc.first, ImageTokens: tc.second}
				} else {
					u.InputTokens = tc.total
					u.InputTokenDetails = domain.TokenDetails{TextTokens: tc.first, AudioTokens: tc.second}
				}
				if tc.valid {
					require.NoError(t, domain.ValidateRealtimeUsage(u))
				} else {
					require.Error(t, domain.ValidateRealtimeUsage(u))
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
				u := &domain.Usage{TotalTokens: tc.input, InputTokens: tc.input, InputTokenDetails: domain.TokenDetails{CachedTokens: tc.cache, CachedTokensDetails: &domain.CachedDetails{TextTokens: tc.text}}}
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
				require.NoError(t, domain.ValidateRealtimeUsage(u))
				vars := map[string]bool{"cr": true, modality: true}
				reason := domain.RealtimeCacheDiscountIssue(u, vars)
				assert.Equal(t, tc.reason, reason)
				u.CacheDiscountUnavailable = reason != ""
				params := domain.BuildRealtimeTieredTokenParams(u, vars)
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
				u := &domain.Usage{TotalTokens: 100, InputTokens: 100, InputTokenDetails: domain.TokenDetails{TextTokens: 100, CachedTokens: 50, CachedTokensDetails: &domain.CachedDetails{TextTokens: &noise, AudioTokens: &noise, ImageTokens: &noise}}}
				vars := billingexpr.UsedVars(expr)
				assert.Empty(t, domain.RealtimeCacheDiscountIssue(u, vars))
				params := domain.BuildRealtimeTieredTokenParams(u, vars)
				if vars["cr"] {
					assert.Equal(t, float64(50), params.P)
					assert.Equal(t, float64(50), params.CR)
				} else {
					assert.Equal(t, float64(100), params.P)
				}
				if !vars["cr"] {
					u.InputTokenDetails.CachedTokens = noise
					assert.Empty(t, domain.RealtimeCacheDiscountIssue(u, vars))
					assert.Equal(t, float64(100), domain.BuildRealtimeTieredTokenParams(u, vars).P)
				}
			})
		}
	}
}

func TestBuildTieredTokenParams_GPT_WithCache(t *testing.T) {
	usage := &domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		PromptTokensDetails: domain.TokenDetails{
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
	usage := &domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		PromptTokensDetails: domain.TokenDetails{
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
	usage := &domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		PromptTokensDetails: domain.TokenDetails{
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
	usage := &domain.Usage{
		PromptTokens:     800,
		CompletionTokens: 500,
		PromptTokensDetails: domain.TokenDetails{
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
	usage := &domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 600,
		CompletionTokenDetails: domain.TokenDetails{
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
	usage := &domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 600,
		CompletionTokenDetails: domain.TokenDetails{
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

func TestBuildTieredTokenParams_Len_GPT(t *testing.T) {
	usage := &domain.Usage{
		PromptTokens:     10000,
		CompletionTokens: 2000,
		PromptTokensDetails: domain.TokenDetails{
			CachedTokens: 3000,
			TextTokens:   7000,
		},
	}
	expr := `tier("base", p * 2.5 + c * 15 + cr * 0.25)`
	usedVars := billingexpr.UsedVars(expr)
	params := domain.BuildTieredTokenParams(usage, false, usedVars)

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
	usage := &domain.Usage{
		PromptTokens:     5000,
		CompletionTokens: 2000,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: domain.TokenDetails{
			CachedTokens: 3000,
			TextTokens:   5000,
		},
		ClaudeCacheCreation5mTokens: 1000,
		ClaudeCacheCreation1hTokens: 500,
	}
	expr := `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`
	usedVars := billingexpr.UsedVars(expr)
	params := domain.BuildTieredTokenParams(usage, true, usedVars)

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

func tieredQuota(exprStr string, usage *domain.Usage, isClaudeSemantic bool, groupRatio float64) float64 {
	usedVars := billingexpr.UsedVars(exprStr)
	params := domain.BuildTieredTokenParams(usage, isClaudeSemantic, usedVars)
	cost, _, _ := billingexpr.RunExpr(exprStr, billingexpr.TokenParams(params))
	return cost / 1_000_000 * 500_000.0 * groupRatio
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := domain.TextCost{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, domain.CacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := domain.TextCost{CacheCreationTokens: 50}
		require.Equal(t, 50, domain.CacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := domain.TextCost{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, domain.CacheWriteTokensTotal(summary))
	})
}

const testQuotaPerUnit = 500_000.0
const sonnetTieredExpr = `p <= 200000 ? tier("standard", p * 1.5 + c * 7.5) : tier("long_context", p * 3 + c * 11.25)`
const flatExpr = `tier("default", p * 2 + c * 10)`
const cacheExpr = `tier("default", p * 2 + c * 10 + cr * 0.2 + cc * 2.5 + cc1h * 4)`
const probeExpr = `param("service_tier") == "fast" ? tier("fast", p * 4 + c * 20) : tier("normal", p * 2 + c * 10)`

type tieredSettlement struct {
	Snapshot *domain.TieredSnapshot
	Request  domain.RequestInput
	Reserved int
}

func newTieredSettlement(expr string, groupRatio float64, prompt, completion int) tieredSettlement {
	cost, trace, _ := billingexpr.RunExpr(expr, billingexpr.TokenParams{P: float64(prompt), C: float64(completion)})
	before := cost / 1_000_000 * testQuotaPerUnit
	after := billingexpr.QuotaRound(before * groupRatio)
	return tieredSettlement{Snapshot: &domain.TieredSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), GroupRatio: groupRatio, EstimatedPromptTokens: prompt, EstimatedCompletionTokens: completion, QuotaPerUnit: testQuotaPerUnit, EstimatedQuotaBeforeGroup: before, EstimatedQuotaAfterGroup: after, EstimatedTier: trace.MatchedTier}, Reserved: after}
}

func TestTryTieredWssSettleAcceptsZero(t *testing.T) {
	for _, tc := range []struct {
		name, expr string
		group      float64
	}{
		{"zero", "0", 1}, {"free_branch", `len>0 ? tier("free",0) : tier("paid",p*2)`, 1}, {"zero_group", "p*2", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := newTieredSettlement(tc.expr, tc.group, 1, 0)
			ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1, Len: 1}, true)
			require.True(t, ok)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Zero(t, quota)
		})
	}
}

func TestTryTieredWssSettleRejectsNegativeFinalQuota(t *testing.T) {
	info := newTieredSettlement(`tier("negative", p * -1)`, 1, 1, 0)
	ok, _, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1}, true)
	require.True(t, ok)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Negative(t, result.ActualQuotaBeforeGroup)
	info = newTieredSettlement(`tier("negative-fraction", p * -0.1)`, 1, 1, 0)
	_, _, _, err = domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1}, true)
	require.Error(t, err)
}

func TestTryTieredSettleUsesFrozenRequestInput(t *testing.T) {
	exprStr := `param("service_tier") == "fast" ? tier("fast", p * 2) : tier("normal", p)`
	relayInfo := tieredSettlement{
		Snapshot: &domain.TieredSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                exprStr,
			ExprHash:                  billingexpr.ExprHashString(exprStr),
			GroupRatio:                1.0,
			EstimatedPromptTokens:     100,
			EstimatedCompletionTokens: 0,
			EstimatedQuotaAfterGroup:  50,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		Request: domain.RequestInput{
			Body: []byte(`{"service_tier":"fast"}`),
		},
	}

	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, relayInfo.Snapshot, relayInfo.Request, relayInfo.Reserved, domain.TokenParams{P: 100}, false)
	require.NoError(t, err)
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
	info := newTieredSettlement(`p > 10000 ? 1e20 : 0`, 1, 0, 0)
	ok, _, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 10001}, true)
	require.True(t, ok)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.Clamp)
}

func TestTryTieredSettleFallsBackToFrozenPreConsumeOnExprError(t *testing.T) {
	relayInfo := tieredSettlement{
		Reserved: 321,
		Snapshot: &domain.TieredSnapshot{
			BillingMode:              "tiered_expr",
			ExprString:               `invalid +-+ expr`,
			ExprHash:                 billingexpr.ExprHashString(`invalid +-+ expr`),
			GroupRatio:               1.0,
			EstimatedQuotaAfterGroup: 123,
		},
	}

	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, relayInfo.Snapshot, relayInfo.Request, relayInfo.Reserved, domain.TokenParams{P: 100}, false)
	require.NoError(t, err)
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

func TestTryTieredSettle_PreConsumeMatchesPostConsume(t *testing.T) {
	info := newTieredSettlement(flatExpr, 1.0, 1000, 500)
	params := domain.TokenParams{P: 1000, C: 500}

	ok, quota, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, params, false)
	require.NoError(t, err)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500
	if quota != 3500 {
		t.Fatalf("quota = %d, want 3500", quota)
	}
	if quota != info.Reserved {
		t.Fatalf("pre-consume %d != post-consume %d", info.Reserved, quota)
	}
}

func TestTryTieredSettle_PostConsumeOverPreConsume(t *testing.T) {
	info := newTieredSettlement(flatExpr, 1.0, 1000, 500)
	preConsumed := info.Reserved // 3500

	// Actual usage is higher than estimated
	params := domain.TokenParams{P: 2000, C: 1000}
	ok, quota, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, params, false)
	require.NoError(t, err)
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
	info := newTieredSettlement(flatExpr, 1.0, 1000, 500)
	preConsumed := info.Reserved // 3500

	// Actual usage is lower than estimated
	params := domain.TokenParams{P: 100, C: 50}
	ok, quota, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, params, false)
	require.NoError(t, err)
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

func TestTryTieredSettle_ExactBoundary(t *testing.T) {
	info := newTieredSettlement(sonnetTieredExpr, 1.0, 200000, 1000)

	// p == 200000 => standard tier (p <= 200000)
	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 200000, C: 1000}, false)
	require.NoError(t, err)
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
	info := newTieredSettlement(sonnetTieredExpr, 1.0, 200000, 1000)

	// p == 200001 => crosses to long_context tier
	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 200001, C: 1000}, false)
	require.NoError(t, err)
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
	info := newTieredSettlement(flatExpr, 1.0, 0, 0)

	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 0, C: 0}, false)
	require.NoError(t, err)
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
	info := newTieredSettlement(flatExpr, 1.0, 10000000, 5000000)

	ok, quota, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 10000000, C: 5000000}, false)
	require.NoError(t, err)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 70000000; quota = 70000000 / 1M * 500K = 35000000
	if quota != 35000000 {
		t.Fatalf("quota = %d, want 35000000", quota)
	}
}

func TestTryTieredSettle_CacheTokensAffectSettlement(t *testing.T) {
	info := newTieredSettlement(cacheExpr, 1.0, 1000, 500)

	// Without cache tokens
	ok1, quota1, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
	if !ok1 {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500

	// With cache tokens
	ok2, quota2, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500, CR: 10000, CC: 5000, CC1h: 2000}, false)
	require.NoError(t, err)
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

func TestTryTieredSettle_RequestProbeInfluencesBilling(t *testing.T) {
	info := newTieredSettlement(probeExpr, 1.0, 1000, 500)
	info.Request = domain.RequestInput{
		Body: []byte(`{"service_tier":"fast"}`),
	}

	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
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
	info := newTieredSettlement(probeExpr, 1.0, 1000, 500)
	// No Request set — param("service_tier") returns nil, not "fast"

	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
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

func TestTryTieredSettle_GroupRatioScaling(t *testing.T) {
	info := newTieredSettlement(flatExpr, 1.5, 1000, 500)

	ok, quota, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// exprCost = 7000, quotaBeforeGroup = 3500, afterGroup = round(3500 * 1.5) = 5250
	if quota != 5250 {
		t.Fatalf("quota = %d, want 5250", quota)
	}
}

func TestTryTieredSettle_GroupRatioZero(t *testing.T) {
	info := newTieredSettlement(flatExpr, 0, 1000, 500)

	ok, quota, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	if quota != 0 {
		t.Fatalf("quota = %d, want 0 (group ratio = 0)", quota)
	}
}

func TestTryTieredSettle_RatioMode_NilSnapshot(t *testing.T) {
	info := tieredSettlement{
		Snapshot: nil,
	}

	ok, _, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
	if ok {
		t.Fatal("expected TryTieredSettle to return false when snapshot is nil")
	}
}

func TestTryTieredSettle_RatioMode_WrongBillingMode(t *testing.T) {
	info := tieredSettlement{
		Snapshot: &domain.TieredSnapshot{
			BillingMode: "ratio",
			ExprString:  flatExpr,
			ExprHash:    billingexpr.ExprHashString(flatExpr),
			GroupRatio:  1.0,
		},
	}

	ok, _, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
	if ok {
		t.Fatal("expected TryTieredSettle to return false for ratio billing mode")
	}
}

func TestTryTieredSettle_RatioMode_EmptyBillingMode(t *testing.T) {
	info := tieredSettlement{
		Snapshot: &domain.TieredSnapshot{
			BillingMode: "",
			ExprString:  flatExpr,
			ExprHash:    billingexpr.ExprHashString(flatExpr),
			GroupRatio:  1.0,
		},
	}

	ok, _, _, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 1000, C: 500}, false)
	require.NoError(t, err)
	if ok {
		t.Fatal("expected TryTieredSettle to return false for empty billing mode")
	}
}

func TestTryTieredSettle_ErrorFallbackToEstimatedQuotaAfterGroup(t *testing.T) {
	info := tieredSettlement{
		Reserved: 0,
		Snapshot: &domain.TieredSnapshot{
			BillingMode:              "tiered_expr",
			ExprString:               `invalid expr!!!`,
			ExprHash:                 billingexpr.ExprHashString(`invalid expr!!!`),
			GroupRatio:               1.0,
			EstimatedQuotaAfterGroup: 999,
		},
	}

	ok, quota, result, err := domain.SettleTiered(adapter.Expression{}, info.Snapshot, info.Request, info.Reserved, domain.TokenParams{P: 100}, false)
	require.NoError(t, err)
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	// Reserved is 0, should fall back to EstimatedQuotaAfterGroup
	if quota != 999 {
		t.Fatalf("quota = %d, want 999", quota)
	}
	if result != nil {
		t.Fatal("result should be nil on error fallback")
	}
}
