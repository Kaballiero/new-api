package domain

import (
	"strings"
)

type SyncUpstreamSource struct {
	Locale     string `json:"locale"`
	ModelsURL  string `json:"models_url"`
	VendorsURL string `json:"vendors_url"`
	Version    string `json:"version"`
}

type MetadataSyncField struct {
	Field    string `json:"field"`
	Local    any    `json:"local"`
	Upstream any    `json:"upstream"`
}

type MetadataSyncCandidate struct {
	ModelName      string              `json:"model_name"`
	Kind           string              `json:"kind"`
	Scope          string              `json:"scope"`
	RecordVersion  string              `json:"record_version"`
	Fields         []MetadataSyncField `json:"fields"`
	Upstream       *MetadataValues     `json:"upstream,omitempty"`
	VendorToCreate string              `json:"vendor_to_create,omitempty"`
}

func NormalizeLocale(locale string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "", "zh", "zh-cn":
		return "zh", true
	case "en":
		return "en", true
	case "ja":
		return "ja", true
	default:
		return "", false
	}
}
