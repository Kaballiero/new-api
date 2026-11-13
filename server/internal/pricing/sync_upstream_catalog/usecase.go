package sync_upstream_catalog

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type CatalogSyncResult struct {
	Publication    domain.CatalogPublication
	ImportedModels int
	MissingModels  []string
}
type Store interface {
	EnsureSource(context.Context, domain.CatalogSource) (domain.CatalogSource, error)
	Documents(context.Context, string, []string) (map[string]domain.CatalogDocument, error)
	Transaction(context.Context, func(domain.CatalogTransaction) error) error
}

// Client reads the remote manifest and downloads documents whose raw bytes are
// verified against their hash and shape (domain.CatalogDocumentClient).
type Client interface {
	Manifest(context.Context) (domain.SourceManifest, error)
	domain.CatalogDocumentClient
}
type Usecase struct {
	deps       domain.Capabilities
	store      Store
	documents  domain.CatalogDocumentCache
	now        func() time.Time
	invalidate func()
}

func NewUsecase(deps domain.Capabilities, store Store, documents domain.CatalogDocumentCache, now func() time.Time, invalidate func()) Usecase {
	return Usecase{deps: deps, store: store, documents: documents, now: now, invalidate: invalidate}
}

// SyncUpstreamCatalog acquires one immutable remote manifest and its requested
// alias closure. Only a complete, validated selection is allowed to move the head.
func (u Usecase) Execute(ctx context.Context, source domain.CatalogSource, requested map[string]bool, client Client) (CatalogSyncResult, error) {
	result := CatalogSyncResult{}
	current, err := u.ensureSource(ctx, source)
	if err != nil {
		return result, err
	}
	manifest, err := client.Manifest(ctx)
	if err != nil {
		return result, err
	}
	refs := make(map[string]domain.SourceReference, len(manifest.Models))
	for _, ref := range manifest.Models {
		refs[ref.ID] = ref
	}
	pending := []domain.SourceReference{}
	for id := range requested {
		ref, exists := refs[id]
		if !exists {
			result.MissingModels = append(result.MissingModels, id)
			continue
		}
		pending = append(pending, ref)
	}
	slices.Sort(result.MissingModels)
	selected := map[string]domain.SourceReference{}
	decoded := map[string]*domain.SourceDocument{}
	fresh := []domain.CatalogDocument{}
	for len(pending) > 0 {
		for _, ref := range pending {
			selected[ref.ID] = ref
		}
		loaded, downloaded, err := domain.LoadCatalogDocuments(ctx, u.deps, u.store, u.documents, client, source.ID, pending)
		if err != nil {
			return result, err
		}
		maps.Copy(decoded, loaded)
		fresh = append(fresh, downloaded...)
		pending = nil
		for _, doc := range decoded {
			if doc.Alias == nil {
				continue
			}
			target := doc.Alias.TargetID
			if _, exists := selected[target]; exists {
				continue
			}
			ref, exists := refs[target]
			if !exists {
				return result, fmt.Errorf("catalog alias target not present in manifest")
			}
			selected[target] = ref
			pending = append(pending, ref)
		}
	}
	resolveAlias := u.deps.Catalog.Aliases(decoded)
	for id := range decoded {
		if _, err := resolveAlias(id); err != nil {
			return result, err
		}
	}
	selection := domain.CatalogSelection{SchemaVersion: manifest.SchemaVersion, NormalizationVersion: manifest.NormalizationVersion, Models: make([]domain.CatalogModelReference, 0, len(selected))}
	for _, ref := range selected {
		selection.Models = append(selection.Models, domain.CatalogModelReference{ID: ref.ID, Hash: ref.Hash})
	}
	publication, err := u.publishSelection(ctx, current, selection, manifest.Revision, manifest.FetchedAt, fresh)
	if err != nil {
		return result, err
	}
	// The published documents are stored now; the next readers take them
	// decoded instead of decoding them from the database again.
	for _, doc := range fresh {
		u.documents.Store(doc.SourceID, doc.Hash, decoded[doc.ModelID])
	}
	result.ImportedModels = len(requested) - len(result.MissingModels)
	result.Publication = publication
	if publication.Changed {
		u.invalidate()
	}
	return result, nil
}
func (u Usecase) ensureSource(ctx context.Context, source domain.CatalogSource) (domain.CatalogSource, error) {
	if !domain.ValidCatalogHash(source.ID) || source.Kind == "" || len(source.Kind) > 32 || source.BaseURL == "" {
		return domain.CatalogSource{}, fmt.Errorf("invalid catalog source")
	}
	current, err := u.store.EnsureSource(ctx, source)
	if err != nil {
		return domain.CatalogSource{}, err
	}
	if current.Kind != source.Kind || current.BaseURL != source.BaseURL {
		return domain.CatalogSource{}, fmt.Errorf("catalog source identity mismatch")
	}
	return current, nil
}

// PublishCatalogSelection checks references and updates the head in the same
// transaction as new documents. Unchanged documents and revisions are reused.
// The generation fence prevents a stale writer from winning after an ABA change.
func (u Usecase) publishSelection(ctx context.Context, expected domain.CatalogSource, selection domain.CatalogSelection, remoteRevision, fetchedAt string, documents []domain.CatalogDocument) (domain.CatalogPublication, error) {
	result := domain.CatalogPublication{}
	if !domain.ValidCatalogHash(expected.ID) || !domain.ValidCatalogHash(remoteRevision) || selection.SchemaVersion != 1 || selection.NormalizationVersion == "" || len(selection.NormalizationVersion) > 128 {
		return result, fmt.Errorf("invalid catalog publication identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, fetchedAt); err != nil {
		return result, fmt.Errorf("invalid catalog fetchedAt")
	}
	selection.Models = slices.Clone(selection.Models)
	slices.SortFunc(selection.Models, func(a, b domain.CatalogModelReference) int { return strings.Compare(a.ID, b.ID) })
	references := make(map[string]string, len(selection.Models))
	byHash := make(map[string]string, len(selection.Models))
	for _, ref := range selection.Models {
		if ref.ID == "" || len(ref.ID) > 1024 || !domain.ValidCatalogHash(ref.Hash) {
			return result, fmt.Errorf("invalid catalog model reference")
		}
		if _, exists := references[ref.ID]; exists {
			return result, fmt.Errorf("duplicate catalog model reference")
		}
		if _, exists := byHash[ref.Hash]; exists {
			return result, fmt.Errorf("duplicate catalog document reference")
		}
		references[ref.ID] = ref.Hash
		byHash[ref.Hash] = ref.ID
	}
	payload, err := u.deps.Codec.Marshal(selection)
	if err != nil {
		return result, err
	}
	revision := domain.CatalogHash(payload)
	candidates := make(map[string]domain.CatalogDocument, len(documents))
	for _, doc := range documents {
		// The client verified the payload against its hash and shape.
		if doc.SourceID != expected.ID || byHash[doc.Hash] != doc.ModelID || doc.ModelID == "" || !utf8.ValidString(doc.Payload) {
			return result, fmt.Errorf("catalog document identity or integrity mismatch")
		}
		if _, exists := candidates[doc.Hash]; exists {
			return result, fmt.Errorf("duplicate catalog document")
		}
		candidates[doc.Hash] = doc
	}
	now := u.now().Unix()
	err = u.store.Transaction(ctx, func(tx domain.CatalogTransaction) error {

		updated, err := tx.UpdateHead(expected, revision, remoteRevision, fetchedAt, now)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrCatalogSourceChanged
		}

		hashes := make([]string, 0, len(byHash))
		for hash := range byHash {
			hashes = append(hashes, hash)
		}
		existing := make(map[string]bool, len(hashes))
		if err := tx.VisitDocumentIdentities(expected.ID, hashes, func(hash, id string) error {
			if byHash[hash] != id {
				return fmt.Errorf("stored catalog model identity mismatch")
			}
			existing[hash] = true
			return nil
		}); err != nil {
			return err
		}

		for _, ref := range selection.Models {
			if existing[ref.Hash] {
				result.ReusedDocuments++
				continue
			}
			doc, ok := candidates[ref.Hash]
			if !ok {
				return fmt.Errorf("catalog publication is missing a document")
			}
			if err := tx.CreateDocument(doc); err != nil {
				return err
			}
			result.NewDocuments++
		}
		row := domain.CatalogRevision{SourceID: expected.ID, Hash: revision, Manifest: string(payload), RemoteRevision: remoteRevision, CreatedAt: now}
		if err := tx.CreateRevision(row); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return domain.CatalogPublication{}, err
	}
	result.Revision = revision
	result.Changed = revision != expected.Revision
	return result, nil
}
