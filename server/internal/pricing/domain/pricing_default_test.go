package domain

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPricingVendorsRemainVisibleWithoutStoredMetadata(t *testing.T) {
	cases := []struct{ model, vendor, icon string }{
		{"qwen3", "Qwen", "Qwen.Color"},
		{"glm-5", "Z.ai", "Zhipu.Color"},
		{"ernie-4", "Baidu", "Wenxin.Color"},
		{"spark-4", "iFlytek", "Spark.Color"},
		{"hunyuan-pro", "Tencent", "Hunyuan.Color"},
		{"yi-large", "01.AI", "Yi.Color"},
		{"doubao-pro", "ByteDance", "Doubao.Color"},
		{"kling-v3", "Kling", "Kling.Color"},
		{"jimeng-v4", "Jimeng", "Jimeng.Color"},
		{"minimax-m3", "MiniMax", "Minimax.Color"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			metadata := map[string]*BuildModel{}
			vendors := map[int]*BuildVendor{}
			InitDefaultVendorMapping(metadata, vendors, []BuildAbility{{Model: tc.model}})
			require.Contains(t, metadata, tc.model)
			require.NotZero(t, metadata[tc.model].VendorID)
			require.Contains(t, vendors, metadata[tc.model].VendorID)
			vendor := vendors[metadata[tc.model].VendorID]
			assert.Equal(t, tc.vendor, vendor.Name)
			assert.Equal(t, tc.icon, vendor.Icon)
		})
	}
}

func TestCatalogAuthorVendorUsesDisplayVendorsWithoutInventingNames(t *testing.T) {
	cases := []struct {
		name   string
		author *SourceAuthor
		wantID int
		vendor string
	}{
		{"existing row matched case-insensitively", &SourceAuthor{Namespace: "z-ai"}, 42, "Z.AI"},
		{"tilde alias shares base name", &SourceAuthor{Namespace: " ~Z-AI "}, 42, "Z.AI"},
		{"tilde alias without explicit entry", &SourceAuthor{Namespace: "~mistralai"}, defaultVendorDisplayIDs["Mistral"], "Mistral"},
		{"display vendor without row", &SourceAuthor{Namespace: "xiaomi"}, defaultVendorDisplayIDs["Xiaomi"], "Xiaomi"},
		{"x-ai keeps existing xAI display", &SourceAuthor{Namespace: "x-ai"}, defaultVendorDisplayIDs["xAI"], "xAI"},
		{"unknown namespace", &SourceAuthor{Namespace: "acme"}, 0, ""},
		{"empty namespace", &SourceAuthor{}, 0, ""},
		{"nil author", nil, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vendors := map[int]*BuildVendor{42: {Id: 42, Name: "Z.AI"}}
			got := CatalogAuthorVendor(tc.author, vendors)
			assert.Equal(t, tc.wantID, got)
			if tc.wantID == 0 {
				assert.Len(t, vendors, 1)
				return
			}
			require.Contains(t, vendors, got)
			assert.Equal(t, tc.vendor, vendors[got].Name)
		})
	}
	seen := map[int]string{}
	for name, id := range defaultVendorDisplayIDs {
		assert.Negative(t, id, name)
		assert.Empty(t, seen[id], "display ID %d is shared by %s and %s", id, seen[id], name)
		seen[id] = name
	}
	for namespace, name := range catalogAuthorVendorNames {
		assert.Contains(t, defaultVendorDisplayIDs, name, namespace)
	}
}

func TestCatalogAuthorAndModelNameHeuristicShareCreator(t *testing.T) {
	for _, tc := range []struct{ model, namespace, vendor string }{
		{"kimi-k2", "moonshotai", "MoonshotAI"},
		{"glm-5", "z-ai", "Z.ai"},
		{"qwen3", "qwen", "Qwen"},
		{"kling-v3", "kwaivgi", "Kling"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			metadata := map[string]*BuildModel{}
			vendors := map[int]*BuildVendor{}
			InitDefaultVendorMapping(metadata, vendors, []BuildAbility{{Model: tc.model}})
			require.Contains(t, metadata, tc.model)
			heuristic := metadata[tc.model].VendorID
			require.NotZero(t, heuristic)
			assert.Equal(t, heuristic, CatalogAuthorVendor(&SourceAuthor{Namespace: tc.namespace}, vendors))
			assert.Equal(t, heuristic, CatalogAuthorVendor(&SourceAuthor{Namespace: tc.namespace}, map[int]*BuildVendor{}))
			assert.Len(t, vendors, 1)
			assert.Equal(t, tc.vendor, vendors[heuristic].Name)
		})
	}
}

func TestDefaultVendorRulesShareCatalogDisplayVendors(t *testing.T) {
	catalogIDs := map[string]int{}
	for namespace, name := range catalogAuthorVendorNames {
		catalogIDs[name] = CatalogAuthorVendor(&SourceAuthor{Namespace: namespace}, map[int]*BuildVendor{})
	}
	for pattern, name := range defaultVendorRules {
		t.Run(pattern, func(t *testing.T) {
			require.Contains(t, defaultVendorDisplayIDs, name)
			assert.Contains(t, defaultVendorIcons, name)
			id := GetDisplayVendor(name, map[int]*BuildVendor{})
			if catalogID, ok := catalogIDs[name]; ok {
				assert.Equal(t, catalogID, id)
			}
		})
	}
}

func TestWithCatalogContextTagFormatsCatalogWindow(t *testing.T) {
	for _, tc := range []struct {
		name, tags, limits, want string
	}{
		{"thousands keep one decimal", "", `{"contextTokens":32768}`, "32.8K"},
		{"whole thousands", "Vision", `{"contextTokens":400000}`, "Vision,400K"},
		{"small window", "", `{"contextTokens":8192}`, "8.2K"},
		{"millions keep two decimals", "top,text", `{"contextTokens":1048576}`, "top,text,1.05M"},
		{"whole million", "", `{"contextTokens":1000000}`, "1M"},
		{"rounding up to a million", "", `{"contextTokens":999999}`, "1M"},
		{"stored context tag wins", "Reasoning,32.8K", `{"contextTokens":1048576}`, "Reasoning,32.8K"},
		{"stored tag is case-insensitive", "128k", `{"contextTokens":200000}`, "128k"},
		{"zero window", "top", `{"contextTokens":0}`, "top"},
		{"window rounding to zero", "top", `{"contextTokens":49}`, "top"},
		{"smallest shown window", "", `{"contextTokens":50}`, "0.1K"},
		{"missing window", "top", `{"published":null}`, "top"},
		{"null limits", "top", `null`, "top"},
		{"invalid limits", "top", `{`, "top"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, WithCatalogContextTag(Capabilities{Codec: JSONCodec{Unmarshal: common.Unmarshal}}, tc.tags, json.RawMessage(tc.limits)))
		})
	}
}

func TestCatalogDetailPricesDedupesIdenticalRatesInPlace(t *testing.T) {
	type rate struct{ key, component, componentID, unit, quantity, amountUSD, amountRub, conditions, variant string }
	tokens := rate{component: "input", unit: "token", quantity: "1000000", amountUSD: "0.5", conditions: `{"tier":"standard","context":{"min":0,"max":200000}}`, variant: `{"mode":"batch"}`}
	with := func(base rate, change func(*rate)) rate {
		change(&base)
		return base
	}
	for _, tc := range []struct {
		name  string
		rates []rate
		want  []string
	}{
		{
			name: "condition and variant JSON order and whitespace do not matter",
			rates: []rate{
				with(tokens, func(r *rate) { r.key = "first" }),
				with(tokens, func(r *rate) {
					r.key, r.conditions, r.variant = "reordered", ` { "context" : {"max":200000, "min":0}, "tier":"standard" } `, `{ "mode": "batch" }`
				}),
			},
			want: []string{"first"},
		},
		{
			name: "quantity and amount compare as decimals",
			rates: []rate{
				with(tokens, func(r *rate) { r.key = "first" }),
				with(tokens, func(r *rate) { r.key, r.quantity, r.amountUSD = "scaled", "1e6", "0.50" }),
			},
			want: []string{"first"},
		},
		{
			name: "absent, null and empty conditions are the same slot",
			rates: []rate{
				with(tokens, func(r *rate) { r.key, r.conditions = "absent", "" }),
				with(tokens, func(r *rate) { r.key, r.conditions = "null", "null" }),
				with(tokens, func(r *rate) { r.key, r.conditions = "empty", "{}" }),
			},
			want: []string{"absent"},
		},
		{
			name: "different slot or amount stays separate in source order",
			rates: []rate{
				with(tokens, func(r *rate) { r.key = "base" }),
				with(tokens, func(r *rate) { r.key, r.variant = "variant", `{"mode":"flex"}` }),
				with(tokens, func(r *rate) { r.key, r.conditions = "conditions", `{"tier":"priority"}` }),
				with(tokens, func(r *rate) { r.key, r.conditions = "no conditions", "" }),
				with(tokens, func(r *rate) { r.key, r.amountUSD = "amount", "0.6" }),
				with(tokens, func(r *rate) { r.key, r.unit = "unit", "request" }),
				with(tokens, func(r *rate) { r.key, r.quantity = "quantity", "1000" }),
				with(tokens, func(r *rate) { r.key, r.component = "component", "output" }),
			},
			want: []string{"base", "variant", "conditions", "no conditions", "amount", "unit", "quantity", "component"},
		},
		{
			name: "unknown components differ by component id",
			rates: []rate{
				with(tokens, func(r *rate) { r.key, r.component, r.componentID = "first", "unknown", "a" }),
				with(tokens, func(r *rate) { r.key, r.component, r.componentID = "same id", "unknown", "a" }),
				with(tokens, func(r *rate) { r.key, r.component, r.componentID = "other id", "unknown", "b" }),
			},
			want: []string{"first", "other id"},
		},
		{
			name: "unpriced or unreadable rates are never merged",
			rates: []rate{
				with(tokens, func(r *rate) { r.key, r.amountUSD = "no amount", "" }),
				with(tokens, func(r *rate) { r.key, r.amountUSD = "no amount again", "" }),
				with(tokens, func(r *rate) { r.key, r.unit = "source defined", "source_defined" }),
				with(tokens, func(r *rate) { r.key, r.unit = "source defined again", "source_defined" }),
				with(tokens, func(r *rate) { r.key, r.conditions = "broken conditions", `{"tier":` }),
				with(tokens, func(r *rate) { r.key, r.conditions = "broken conditions again", `{"tier":` }),
				with(tokens, func(r *rate) { r.key, r.conditions = "array conditions", `["tier"]` }),
				with(tokens, func(r *rate) { r.key, r.conditions = "array conditions again", `["tier"]` }),
				with(tokens, func(r *rate) { r.key, r.quantity = "bad quantity", "many" }),
				with(tokens, func(r *rate) { r.key, r.quantity = "bad quantity again", "many" }),
				with(tokens, func(r *rate) { r.key, r.amountUSD = "bad amount", "free" }),
				with(tokens, func(r *rate) { r.key, r.amountUSD = "bad amount again", "free" }),
			},
			want: []string{"no amount", "no amount again", "source defined", "source defined again", "broken conditions", "broken conditions again", "array conditions", "array conditions again", "bad quantity", "bad quantity again", "bad amount", "bad amount again"},
		},
		{
			name: "a later rouble copy replaces the kept rate at its position",
			rates: []rate{
				with(tokens, func(r *rate) { r.key = "usd only" }),
				with(tokens, func(r *rate) { r.key, r.component = "output", "output" }),
				with(tokens, func(r *rate) { r.key, r.amountRub = "rouble copy", "4500" }),
				with(tokens, func(r *rate) { r.key, r.amountRub = "second rouble copy", "4500" }),
			},
			want: []string{"rouble copy", "output"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prices := make([]ConsumerProviderPrice, 0, len(tc.rates))
			for _, r := range tc.rates {
				price := ConsumerProviderPrice{Component: r.component}
				price.Source.Key, price.ComponentID, price.Unit, price.Quantity = r.key, r.componentID, r.unit, r.quantity
				price.Conditions, price.Variant = json.RawMessage(r.conditions), json.RawMessage(r.variant)
				if r.amountUSD != "" {
					price.AmountUSD = &r.amountUSD
				}
				if r.amountRub != "" {
					price.AmountRub = &r.amountRub
				}
				prices = append(prices, price)
			}
			kept := []string{}
			for _, price := range CatalogDetailPrices(Capabilities{Codec: JSONCodec{Marshal: common.Marshal, Unmarshal: common.Unmarshal}}, prices) {
				kept = append(kept, price.Source.Key)
			}
			assert.Equal(t, tc.want, kept)
		})
	}
}
