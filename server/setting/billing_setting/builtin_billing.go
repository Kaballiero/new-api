package billing_setting

// Built-in token prices use actual USD per million tokens. Keep new model
// defaults here instead of splitting them across the legacy ratio tables.
var builtinBillingExpr = map[string]string{
	// https://developers.openai.com/api/docs/models/gpt-6-astra
	// Standard pricing; the long-context rates apply to the whole request.
	// Do not infer service-tier discounts from incoming request parameters:
	// channels filter service_tier by default, so it may not reach the upstream.
	"gpt-6-astra": `len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1 + cc * 12.5) : tier("long_context", p * 20 + c * 75 + cr * 2 + cc * 25)`,
	// Native OpenRouter Image API: validated tariff estimates and actual usage.cost
	// supply USD costs; v1 expressions retain their existing per-million scale.
	// https://openrouter.ai/docs/api/api-reference/images
	"meta/muse-image":                         `tier("openrouter", provider_cost * 1000000)`,
	"krea/krea-2-large":                       `tier("openrouter", provider_cost * 1000000)`,
	"krea/krea-2-medium":                      `tier("openrouter", provider_cost * 1000000)`,
	"krea/krea-2-medium-turbo":                `tier("openrouter", provider_cost * 1000000)`,
	"tencent/hy-image-v3.5-preview":           `tier("openrouter", provider_cost * 1000000)`,
	"bytedance-seed/seedream-5-0-flash":       `tier("openrouter", provider_cost * 1000000)`,
	"black-forest-labs/flux-3-image":          `tier("openrouter", provider_cost * 1000000)`,
	"inclusionai/ming-image-0.1-design-layer": `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1-flash":              `tier("openrouter", provider_cost * 1000000)`,
	"inclusionai/ming-image-0.1-design":       `tier("openrouter", provider_cost * 1000000)`,
	"openai/gpt-image-2.5-sunburst":           `tier("openrouter", provider_cost * 1000000)`,
	"openai/gpt-image-2.5-flare":              `tier("openrouter", provider_cost * 1000000)`,
	"microsoft/mai-image-2.6":                 `tier("openrouter", provider_cost * 1000000)`,
	"microsoft/mai-image-2.6-flash":           `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-styles-pro":           `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-styles-vector":        `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-styles-pro-vector":    `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-styles":               `tier("openrouter", provider_cost * 1000000)`,
	"bytedance-seed/seedream-5-0-lite":        `tier("openrouter", provider_cost * 1000000)`,
	"bytedance-seed/seedream-5-0-pro":         `tier("openrouter", provider_cost * 1000000)`,
	"x-ai/grok-imagine-image-2.0":             `tier("openrouter", provider_cost * 1000000)`,
	"qwen/qwen-image-3-pro":                   `tier("openrouter", provider_cost * 1000000)`,
	"qwen/qwen-image-3":                       `tier("openrouter", provider_cost * 1000000)`,
	"microsoft/mai-image-2.5-pro":             `tier("openrouter", provider_cost * 1000000)`,
	"openai/gpt-image-2":                      `tier("openrouter", provider_cost * 1000000)`,
	"openai/gpt-image-1-mini":                 `tier("openrouter", provider_cost * 1000000)`,
	"openai/gpt-image-1":                      `tier("openrouter", provider_cost * 1000000)`,
	"sourceful/riverflow-v2.5-pro":            `tier("openrouter", provider_cost * 1000000)`,
	"sourceful/riverflow-v2.5-fast":           `tier("openrouter", provider_cost * 1000000)`,
	"microsoft/mai-image-2.5":                 `tier("openrouter", provider_cost * 1000000)`,
	"x-ai/grok-imagine-image-quality":         `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1-pro-vector":         `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1-vector":             `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1-utility-pro":        `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1-utility":            `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1-pro":                `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4.1":                    `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-pro-vector":           `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-vector":               `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4-pro":                  `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v4":                      `tier("openrouter", provider_cost * 1000000)`,
	"recraft/recraft-v3":                      `tier("openrouter", provider_cost * 1000000)`,
	"sourceful/riverflow-v2-pro":              `tier("openrouter", provider_cost * 1000000)`,
	"sourceful/riverflow-v2-fast":             `tier("openrouter", provider_cost * 1000000)`,
	"black-forest-labs/flux.2-klein-4b":       `tier("openrouter", provider_cost * 1000000)`,
	"bytedance-seed/seedream-4.5":             `tier("openrouter", provider_cost * 1000000)`,
	"black-forest-labs/flux.2-max":            `tier("openrouter", provider_cost * 1000000)`,
	"black-forest-labs/flux.2-flex":           `tier("openrouter", provider_cost * 1000000)`,
	"black-forest-labs/flux.2-pro":            `tier("openrouter", provider_cost * 1000000)`,
}
