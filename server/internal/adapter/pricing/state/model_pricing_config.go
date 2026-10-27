package pricingstate

import (
	"sync"
)

// Lock order is stable across instances. Creating missing option rows inside
// the transaction also serializes the first write to an unconfigured database.
var ModelPricingOptionKeys = []string{
	"AudioCompletionRatio", "AudioRatio", "CacheRatio", "CompletionRatio",
	"CreateCacheRatio", "ImageRatio", "ModelPrice", "ModelRatio",
	"billing_setting.batch_billing_expr", "billing_setting.billing_expr", "billing_setting.billing_mode",
}

var ModelPricingMutationMu sync.Mutex
