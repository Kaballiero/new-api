package pricing

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Immutable struct{ db func() *gorm.DB }

func NewImmutable(db func() *gorm.DB) Immutable { return Immutable{db: db} }
func (s Immutable) EnsureSource(ctx context.Context, source domain.CatalogSource) (domain.CatalogSource, error) {
	initial := CatalogSource{ID: source.ID, Kind: source.Kind, BaseURL: source.BaseURL}
	if err := s.db().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
		return domain.CatalogSource{}, err
	}
	var current CatalogSource
	if err := s.db().WithContext(ctx).Where("id = ?", source.ID).Take(&current).Error; err != nil {
		return domain.CatalogSource{}, err
	}
	return domain.CatalogSource(current), nil
}
func (s Immutable) Documents(ctx context.Context, sourceID string, hashes []string) (map[string]domain.CatalogDocument, error) {
	result := make(map[string]domain.CatalogDocument, len(hashes))
	for start := 0; start < len(hashes); start += 400 {
		var rows []CatalogDocument
		if err := s.db().WithContext(ctx).Where("source_id = ? AND hash IN ?", sourceID, hashes[start:min(start+400, len(hashes))]).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if domain.CatalogHash([]byte(row.Payload)) != row.Hash {
				return nil, fmt.Errorf("stored catalog document integrity mismatch")
			}
			result[row.Hash] = domain.CatalogDocument(row)
		}
	}
	return result, nil
}
func (s Immutable) ReadSelection(ctx context.Context, sourceID, revision string) (domain.CatalogSelection, error) {
	var row CatalogRevision
	if err := s.db().WithContext(ctx).Where("source_id = ? AND hash = ?", sourceID, revision).Take(&row).Error; err != nil {
		return domain.CatalogSelection{}, err
	}
	if domain.CatalogHash([]byte(row.Manifest)) != revision {
		return domain.CatalogSelection{}, fmt.Errorf("stored catalog revision integrity mismatch")
	}
	var selection domain.CatalogSelection
	if err := common.UnmarshalJsonStr(row.Manifest, &selection); err != nil {
		return domain.CatalogSelection{}, err
	}
	return selection, nil
}
func (s Immutable) Transaction(ctx context.Context, work func(domain.CatalogTransaction) error) error {
	return s.db().WithContext(ctx).Transaction(func(tx *gorm.DB) error { return work(immutableTransaction{tx: tx}) })
}

type immutableTransaction struct{ tx *gorm.DB }

func (t immutableTransaction) UpdateHead(expected domain.CatalogSource, revision, remote, fetched string, now int64) (bool, error) {
	update := t.tx.Model(&CatalogSource{}).Where("id = ? AND generation = ? AND revision = ?", expected.ID, expected.Generation, expected.Revision).Updates(map[string]any{"revision": revision, "remote_revision": remote, "generation": gorm.Expr("generation + ?", 1), "checked_at": now, "fetched_at": fetched})
	return update.RowsAffected == 1, update.Error
}
func (t immutableTransaction) VisitDocumentIdentities(sourceID string, hashes []string, visit func(string, string) error) error {
	for start := 0; start < len(hashes); start += 400 {
		var rows []struct{ Hash, ModelID string }
		if err := t.tx.Model(&CatalogDocument{}).Select("hash", "model_id").Where("source_id = ? AND hash IN ?", sourceID, hashes[start:min(start+400, len(hashes))]).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := visit(row.Hash, row.ModelID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (t immutableTransaction) CreateDocument(value domain.CatalogDocument) error {
	row := CatalogDocument(value)
	return t.tx.Create(&row).Error
}
func (t immutableTransaction) CreateRevision(value domain.CatalogRevision) error {
	row := CatalogRevision(value)
	return t.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (s Immutable) Head(ctx context.Context, id string) (domain.CatalogSource, error) {
	var row CatalogSource
	err := s.db().WithContext(ctx).Where("id = ?", id).Take(&row).Error
	return domain.CatalogSource(row), err
}
func (s Immutable) IsMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
