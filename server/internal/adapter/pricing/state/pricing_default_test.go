package pricingstate_test

import (
	"testing"

	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	legacy "github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPricingVendorsRemainVisibleWithoutStoredMetadata(t *testing.T) {
	cases := []struct{ model, vendor, icon string }{
		{"qwen3", "Alibaba", "Qwen.Color"},
		{"glm-5", "Zhipu AI", "Zhipu.Color"},
		{"ernie-4", "Baidu", "Wenxin.Color"},
		{"spark-4", "iFlytek", "Spark.Color"},
		{"hunyuan-pro", "Tencent", "Hunyuan.Color"},
		{"yi-large", "01.AI", "Yi.Color"},
		{"doubao-pro", "ByteDance", "Doubao.Color"},
		{"kling-v3", "Kuaishou", "Kling.Color"},
		{"jimeng-v4", "Jimeng", "Jimeng.Color"},
		{"minimax-m3", "MiniMax", "Minimax.Color"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			metadata := map[string]*legacy.Model{}
			vendors := map[int]*legacy.Vendor{}
			pricingstate.TestInitDefaultVendorMapping(metadata, vendors, []channelrecord.AbilityWithChannel{{Ability: channelrecord.Ability{Model: tc.model}}})
			require.Contains(t, metadata, tc.model)
			require.NotZero(t, metadata[tc.model].VendorID)
			require.Contains(t, vendors, metadata[tc.model].VendorID)
			vendor := vendors[metadata[tc.model].VendorID]
			assert.Equal(t, tc.vendor, vendor.Name)
			assert.Equal(t, tc.icon, vendor.Icon)
		})
	}
}
