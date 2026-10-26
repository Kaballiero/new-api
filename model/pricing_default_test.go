package model

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/pkg/catalogsync"
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
			metadata := map[string]*Model{}
			vendors := map[int]*Vendor{}
			initDefaultVendorMapping(metadata, vendors, []AbilityWithChannel{{Ability: Ability{Model: tc.model}}})
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
		author *catalogsync.Author
		wantID int
		vendor string
	}{
		{"existing row matched case-insensitively", &catalogsync.Author{Namespace: "z-ai"}, 42, "Z.AI"},
		{"tilde alias shares base name", &catalogsync.Author{Namespace: " ~Z-AI "}, 42, "Z.AI"},
		{"tilde alias without explicit entry", &catalogsync.Author{Namespace: "~mistralai"}, defaultVendorDisplayIDs["Mistral"], "Mistral"},
		{"display vendor without row", &catalogsync.Author{Namespace: "xiaomi"}, defaultVendorDisplayIDs["Xiaomi"], "Xiaomi"},
		{"x-ai keeps existing xAI display", &catalogsync.Author{Namespace: "x-ai"}, defaultVendorDisplayIDs["xAI"], "xAI"},
		{"unknown namespace", &catalogsync.Author{Namespace: "acme"}, 0, ""},
		{"empty namespace", &catalogsync.Author{}, 0, ""},
		{"nil author", nil, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vendors := map[int]*Vendor{42: {Id: 42, Name: "Z.AI"}}
			got := catalogAuthorVendor(tc.author, vendors)
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
			metadata := map[string]*Model{}
			vendors := map[int]*Vendor{}
			initDefaultVendorMapping(metadata, vendors, []AbilityWithChannel{{Ability: Ability{Model: tc.model}}})
			require.Contains(t, metadata, tc.model)
			heuristic := metadata[tc.model].VendorID
			require.NotZero(t, heuristic)
			assert.Equal(t, heuristic, catalogAuthorVendor(&catalogsync.Author{Namespace: tc.namespace}, vendors))
			assert.Equal(t, heuristic, catalogAuthorVendor(&catalogsync.Author{Namespace: tc.namespace}, map[int]*Vendor{}))
			assert.Len(t, vendors, 1)
			assert.Equal(t, tc.vendor, vendors[heuristic].Name)
		})
	}
}

func TestDefaultVendorRulesShareCatalogDisplayVendors(t *testing.T) {
	catalogIDs := map[string]int{}
	for namespace, name := range catalogAuthorVendorNames {
		catalogIDs[name] = catalogAuthorVendor(&catalogsync.Author{Namespace: namespace}, map[int]*Vendor{})
	}
	for pattern, name := range defaultVendorRules {
		t.Run(pattern, func(t *testing.T) {
			require.Contains(t, defaultVendorDisplayIDs, name)
			assert.Contains(t, defaultVendorIcons, name)
			id := getDisplayVendor(name, map[int]*Vendor{})
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
			assert.Equal(t, tc.want, withCatalogContextTag(tc.tags, json.RawMessage(tc.limits)))
		})
	}
}
