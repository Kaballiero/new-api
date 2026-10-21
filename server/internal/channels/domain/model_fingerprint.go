package domain

import "github.com/QuantumNous/new-api/constant"

// ModelFetchFingerprint is the channel configuration that decides where and how
// upstream models are fetched. Discovery results are applied only while the
// fingerprint stays the same; the fields are compared by value.
type ModelFetchFingerprint struct {
	Type             int
	Key              string
	EffectiveBaseURL string
	Proxy            string
	HeaderOverride   string
	// AdvancedCustom holds the gateway's advanced route configuration.
	AdvancedCustom     any
	TaskPluginKey      string
	IsMultiKey         bool
	MultiKeySize       int
	MultiKeyStatusList map[int]int
	DisabledReason     map[int]string
	DisabledTime       map[int]int64
	MultiKeyMode       constant.MultiKeyMode
}
