package domain

import "github.com/QuantumNous/new-api/constant"

type CatalogRefreshSummary struct {
	NewDocuments            int                     `json:"new_documents,omitempty"`
	ReusedDocuments         int                     `json:"reused_documents,omitempty"`
	Revision                string                  `json:"revision,omitempty"`
	MissingModels           []string                `json:"missing_models,omitempty"`
	HiddenPublicModels      []string                `json:"hidden_public_models,omitempty"`
	HiddenPublicModelsCount int                     `json:"hidden_public_models_count"`
	HiddenPublicModelsError string                  `json:"hidden_public_models_error,omitempty"`
	Sources                 int                     `json:"sources"`
	Published               int                     `json:"published"`
	Failed                  int                     `json:"failed"`
	FailedSources           []CatalogRefreshFailure `json:"failed_sources,omitempty"`
	FailuresTruncated       bool                    `json:"failures_truncated,omitempty"`
}
type CatalogRefreshFailure struct {
	ChannelID     int                   `json:"channel_id"`
	UpstreamModel string                `json:"upstream_model"`
	Endpoint      constant.EndpointType `json:"endpoint"`
	Class         string                `json:"class"`
}
type CatalogSyncSummary struct {
	Publication    CatalogPublication
	ImportedModels int
	MissingModels  []string
}
