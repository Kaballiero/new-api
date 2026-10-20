package publish_catalog_selection

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type Store interface {
	Transaction(context.Context, func(domain.CatalogTransaction) error) error
}
type Usecase struct {
	store    Store
	conflict func() error
	now      func() time.Time
}

func NewUsecase(store Store, conflict func() error, now func() time.Time) Usecase {
	return Usecase{store: store, conflict: conflict, now: now}
}

// PublishCatalogSelection checks references and updates the head in the same
// transaction as new documents. Unchanged documents and revisions are reused.
// The generation fence prevents a stale writer from winning after an ABA change.
func (u Usecase) Execute(ctx context.Context, expected domain.CatalogSource, selection domain.CatalogSelection, remoteRevision, fetchedAt string, documents []domain.CatalogDocument) (domain.CatalogPublication, error) {
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
	payload, err := common.Marshal(selection)
	if err != nil {
		return result, err
	}
	revision := domain.CatalogHash(payload)
	candidates := make(map[string]domain.CatalogDocument, len(documents))
	for _, doc := range documents {
		if doc.SourceID != expected.ID || byHash[doc.Hash] != doc.ModelID || doc.ModelID == "" || domain.CatalogHash([]byte(doc.Payload)) != doc.Hash || !utf8.ValidString(doc.Payload) {
			return result, fmt.Errorf("catalog document identity or integrity mismatch")
		}
		if _, exists := candidates[doc.Hash]; exists {
			return result, fmt.Errorf("duplicate catalog document")
		}
		var raw json.RawMessage
		if common.GetJsonType([]byte(doc.Payload)) != "object" || common.UnmarshalJsonStr(doc.Payload, &raw) != nil {
			return result, fmt.Errorf("catalog document must be a valid JSON object")
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
			return u.conflict()
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
