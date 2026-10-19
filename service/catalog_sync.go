package service

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"golang.org/x/sync/errgroup"
)

type CatalogSyncResult struct {
	Publication    model.CatalogPublication
	ImportedModels int
	MissingModels  []string
}

// SyncUpstreamCatalog acquires one immutable remote manifest and its requested
// alias closure. Only a complete, validated selection is allowed to move the head.
func SyncUpstreamCatalog(ctx context.Context, source model.CatalogSource, requested map[string]bool, client *catalogsync.Client) (CatalogSyncResult, error) {
	result := CatalogSyncResult{}
	current, err := model.EnsureCatalogSource(ctx, source)
	if err != nil {
		return result, err
	}
	manifest, err := client.Manifest(ctx)
	if err != nil {
		return result, err
	}
	refs := make(map[string]catalogsync.Reference, len(manifest.Models))
	for _, ref := range manifest.Models {
		refs[ref.ID] = ref
	}
	pending := []catalogsync.Reference{}
	for id := range requested {
		ref, exists := refs[id]
		if !exists {
			result.MissingModels = append(result.MissingModels, id)
			continue
		}
		pending = append(pending, ref)
	}
	slices.Sort(result.MissingModels)
	selected := map[string]catalogsync.Reference{}
	decoded := map[string]*catalogsync.Document{}
	fresh := []model.CatalogDocument{}
	for len(pending) > 0 {
		hashes := make([]string, 0, len(pending))
		for _, ref := range pending {
			hashes = append(hashes, ref.Hash)
			selected[ref.ID] = ref
		}
		cached, err := model.GetCatalogDocuments(ctx, source.ID, hashes)
		if err != nil {
			return result, err
		}
		var mutex sync.Mutex
		workers, workerCtx := errgroup.WithContext(ctx)
		workers.SetLimit(5)
		for _, ref := range pending {
			workers.Go(func() error {
				var doc *catalogsync.Document
				if row, exists := cached[ref.Hash]; exists {
					var decodeErr error
					doc, decodeErr = catalogsync.DecodeDocument([]byte(row.Payload), ref)
					if decodeErr != nil {
						return decodeErr
					}
				} else {
					raw, fetched, fetchErr := client.Document(workerCtx, ref)
					if fetchErr != nil {
						return fetchErr
					}
					doc = fetched
					mutex.Lock()
					fresh = append(fresh, model.CatalogDocument{SourceID: source.ID, Hash: ref.Hash, ModelID: ref.ID, Payload: string(raw)})
					mutex.Unlock()
				}
				mutex.Lock()
				decoded[ref.ID] = doc
				mutex.Unlock()
				return nil
			})
		}
		if err := workers.Wait(); err != nil {
			return result, err
		}
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
	for id := range decoded {
		if _, err := catalogsync.ResolveAlias(decoded, id); err != nil {
			return result, err
		}
	}
	selection := model.CatalogSelection{SchemaVersion: manifest.SchemaVersion, NormalizationVersion: manifest.NormalizationVersion, Models: make([]model.CatalogModelReference, 0, len(selected))}
	for _, ref := range selected {
		selection.Models = append(selection.Models, model.CatalogModelReference{ID: ref.ID, Hash: ref.Hash})
	}
	publication, err := model.PublishCatalogSelection(ctx, current, selection, manifest.Revision, manifest.FetchedAt, fresh)
	if err != nil {
		return result, err
	}
	result.ImportedModels = len(requested) - len(result.MissingModels)
	result.Publication = publication
	if publication.Changed {
		model.InvalidatePricingCache()
	}
	return result, nil
}
