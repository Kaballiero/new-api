package apply_metadata_sync

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Input struct {
	Locale        string                         `json:"locale"`
	SourceVersion string                         `json:"source_version"`
	Selections    []domain.MetadataSyncSelection `json:"selections"`
}
type Output = domain.MetadataSyncResult
