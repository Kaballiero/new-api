package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

type CatalogSource struct {
	ID             string
	Kind           string
	BaseURL        string
	Revision       string
	RemoteRevision string
	Generation     int64
	CheckedAt      int64
	FetchedAt      string
}
type CatalogRevision struct {
	SourceID       string
	Hash           string
	Manifest       string
	RemoteRevision string
	CreatedAt      int64
}
type CatalogDocument struct {
	SourceID string
	Hash     string
	ModelID  string
	Payload  string
}
type CatalogModelReference struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
type CatalogSelection struct {
	SchemaVersion        int                     `json:"schemaVersion"`
	NormalizationVersion string                  `json:"normalizationVersion"`
	Models               []CatalogModelReference `json:"models"`
}
type CatalogPublication struct {
	Revision        string
	NewDocuments    int
	ReusedDocuments int
	Changed         bool
}

func CatalogHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func ValidCatalogHash(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// OpenRouterShortcuts are the OpenRouter provider routing shortcuts: a model
// id with one of these suffixes routes the listed model without it.
var OpenRouterShortcuts = []string{":nitro", ":floor"}

// CatalogDocumentReader reads stored catalog documents by hash.
type CatalogDocumentReader interface {
	Documents(context.Context, string, []string) (map[string]CatalogDocument, error)
}

// CatalogDocumentClient downloads one catalog document.
type CatalogDocumentClient interface {
	Document(context.Context, SourceReference) ([]byte, *SourceDocument, error)
}

// LoadCatalogDocuments reads the documents of refs by model id: a document
// already stored for the source is decoded from the database, the others are
// downloaded from the catalog. Fresh are the downloaded documents.
func LoadCatalogDocuments(ctx context.Context, deps Capabilities, store CatalogDocumentReader, client CatalogDocumentClient, sourceID string, refs []SourceReference) (map[string]*SourceDocument, []CatalogDocument, error) {
	hashes := make([]string, len(refs))
	for i, ref := range refs {
		hashes[i] = ref.Hash
	}
	stored, err := store.Documents(ctx, sourceID, hashes)
	if err != nil {
		return nil, nil, err
	}
	documents := make(map[string]*SourceDocument, len(refs))
	fresh := []CatalogDocument{}
	var mutex sync.Mutex
	workers, workerCtx := errgroup.WithContext(ctx)
	workers.SetLimit(5)
	for _, ref := range refs {
		workers.Go(func() error {
			if row, exists := stored[ref.Hash]; exists {
				document, err := deps.Catalog.DecodeDocument([]byte(row.Payload), ref)
				if err != nil {
					return err
				}
				mutex.Lock()
				documents[ref.ID] = document
				mutex.Unlock()
				return nil
			}
			raw, document, err := client.Document(workerCtx, ref)
			if err != nil {
				return err
			}
			mutex.Lock()
			documents[ref.ID] = document
			fresh = append(fresh, CatalogDocument{SourceID: sourceID, Hash: ref.Hash, ModelID: ref.ID, Payload: string(raw)})
			mutex.Unlock()
			return nil
		})
	}
	if err := workers.Wait(); err != nil {
		return nil, nil, err
	}
	return documents, fresh, nil
}
