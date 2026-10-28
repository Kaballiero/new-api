package preview_metadata_sync

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Input struct{ Locale string }
type SyncUpstreamPreviewResponse struct {
	Source     domain.SyncUpstreamSource      `json:"source"`
	Candidates []domain.MetadataSyncCandidate `json:"candidates"`
}
type Output = SyncUpstreamPreviewResponse
