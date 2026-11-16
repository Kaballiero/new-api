package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/constant"

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

// MaxCatalogModelLossPercent is the share of the published models a catalog
// revision may lose and still be applied.
const MaxCatalogModelLossPercent = 30

// CatalogChannels reads the enabled abilities and the channels they belong to.
type CatalogChannels interface {
	Abilities() ([]BuildAbility, error)
	BuildChannel(int) (*BuildChannel, error)
}

// CatalogRequestedModels are the upstream catalog models the channels request:
// the upstream model each ability of an enabled OpenRouter catalog channel
// resolves to.
func CatalogRequestedModels(deps Capabilities, channels CatalogChannels) (map[string]bool, error) {
	abilities, err := channels.Abilities()
	if err != nil {
		return nil, err
	}
	loaded := map[int]*BuildChannel{}
	requested := map[string]bool{}
	for _, ability := range abilities {
		if ability.ChannelType != constant.ChannelTypeOpenRouter {
			continue
		}
		channel, exists := loaded[ability.ChannelId]
		if !exists {
			channel, err = channels.BuildChannel(ability.ChannelId)
			if err != nil {
				return nil, err
			}
			loaded[ability.ChannelId] = channel
		}
		if channel.Status != constant.ChannelStatusEnabled || !IsOpenRouterCatalogChannel(channel) {
			continue
		}
		upstream, resolved := ResolvePricingUpstreamModel(deps, channel, ability.Model)
		if !resolved {
			return nil, fmt.Errorf("catalog channel model mapping is unresolved")
		}
		requested[upstream] = true
	}
	return requested, nil
}

// CatalogModelLoss compares a manifest with the published models: the models
// of the head selection the channels request. The head also holds alias
// targets and models no channel requests any more; they are not compared.
// Lost are the published models the manifest no longer lists; exceeded tells
// whether their share of published exceeds MaxCatalogModelLossPercent.
func CatalogModelLoss(head CatalogSelection, requested map[string]bool, manifest SourceManifest) (lost []string, published int, exceeded bool) {
	listed := make(map[string]bool, len(manifest.Models))
	for _, ref := range manifest.Models {
		listed[ref.ID] = true
	}
	for _, ref := range head.Models {
		if !requested[ref.ID] {
			continue
		}
		published++
		if !listed[ref.ID] {
			lost = append(lost, ref.ID)
		}
	}
	slices.Sort(lost)
	return lost, published, len(lost)*100 > MaxCatalogModelLossPercent*published
}

// OpenRouterShortcuts are the OpenRouter provider routing shortcuts: a model
// id with one of these suffixes routes the listed model without it.
var OpenRouterShortcuts = []string{":nitro", ":floor"}

// CatalogDocumentReader reads stored catalog documents by hash.
type CatalogDocumentReader interface {
	Documents(context.Context, string, []string) (map[string]CatalogDocument, error)
}

// CatalogDocumentClient downloads one catalog document. Document returns the
// raw bytes of the document only after verifying them against the reference
// hash and the document shape; the decoded document is decoded from them.
type CatalogDocumentClient interface {
	Document(context.Context, SourceReference) ([]byte, *SourceDocument, error)
}

// CatalogDocumentCache keeps decoded stored catalog documents by source and
// hash. A catalog document is immutable and a stored one is never deleted, so a
// cached document is the document the source stores under its hash.
type CatalogDocumentCache interface {
	Load(sourceID, hash string) (*SourceDocument, bool)
	Store(sourceID, hash string, document *SourceDocument)
}

// LoadStoredCatalogDocuments reads the stored documents of refs by model id: a
// cached document is taken from the cache, the others are decoded from the
// database and cached. Missing are the refs the source has no stored document
// for.
func LoadStoredCatalogDocuments(ctx context.Context, deps Capabilities, store CatalogDocumentReader, cache CatalogDocumentCache, sourceID string, refs []SourceReference) (map[string]*SourceDocument, []SourceReference, error) {
	documents := make(map[string]*SourceDocument, len(refs))
	uncached := []SourceReference{}
	hashes := []string{}
	for _, ref := range refs {
		if document, cached := cache.Load(sourceID, ref.Hash); cached {
			documents[ref.ID] = document
			continue
		}
		uncached = append(uncached, ref)
		hashes = append(hashes, ref.Hash)
	}
	stored, err := store.Documents(ctx, sourceID, hashes)
	if err != nil {
		return nil, nil, err
	}
	missing := []SourceReference{}
	for _, ref := range uncached {
		row, exists := stored[ref.Hash]
		if !exists {
			missing = append(missing, ref)
			continue
		}
		document, err := deps.Catalog.DecodeDocument([]byte(row.Payload), ref)
		if err != nil {
			return nil, nil, err
		}
		cache.Store(sourceID, ref.Hash, document)
		documents[ref.ID] = document
	}
	return documents, missing, nil
}

// LoadCatalogDocuments reads the documents of refs by model id: a document
// already stored for the source is read by LoadStoredCatalogDocuments, the
// others are downloaded from the catalog. Fresh are the downloaded documents.
func LoadCatalogDocuments(ctx context.Context, deps Capabilities, store CatalogDocumentReader, cache CatalogDocumentCache, client CatalogDocumentClient, sourceID string, refs []SourceReference) (map[string]*SourceDocument, []CatalogDocument, error) {
	documents, missing, err := LoadStoredCatalogDocuments(ctx, deps, store, cache, sourceID, refs)
	if err != nil {
		return nil, nil, err
	}
	fresh := []CatalogDocument{}
	var mutex sync.Mutex
	workers, workerCtx := errgroup.WithContext(ctx)
	workers.SetLimit(5)
	for _, ref := range missing {
		workers.Go(func() error {
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
