package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCatalogSourceChanged = errors.New("catalog source changed before publication")

// CatalogSource identifies a single acquisition/account scope, shared by channels.
// Channel credentials, mappings and user policies never belong to this table.
type CatalogSource struct {
	ID             string `gorm:"primaryKey;size:64"`
	Kind           string `gorm:"size:32;not null"`
	BaseURL        string `gorm:"type:text;not null"`
	Revision       string `gorm:"size:64;not null"`
	RemoteRevision string `gorm:"size:64;not null"`
	Generation     int64  `gorm:"not null"`
	CheckedAt      int64  `gorm:"not null"`
	FetchedAt      string `gorm:"size:64;not null"`
}

// CatalogRevision is an immutable list of model/document references. Metadata
// records its first publication; the source tracks the latest successful check.
type CatalogRevision struct {
	SourceID       string `gorm:"primaryKey;size:64"`
	Hash           string `gorm:"primaryKey;size:64"`
	Manifest       string `gorm:"not null"`
	RemoteRevision string `gorm:"size:64;not null"`
	CreatedAt      int64  `gorm:"not null"`
}

// Payload is the exact uncompressed HTTP representation, never re-marshaled.
// The default unbounded string maps to LONGTEXT on MySQL and TEXT elsewhere.
type CatalogDocument struct {
	SourceID string `gorm:"primaryKey;size:64"`
	Hash     string `gorm:"primaryKey;size:64"`
	ModelID  string `gorm:"not null"`
	Payload  string `gorm:"not null"`
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

func validCatalogHash(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func EnsureCatalogSource(ctx context.Context, source CatalogSource) (CatalogSource, error) {
	if !validCatalogHash(source.ID) || source.Kind == "" || len(source.Kind) > 32 || source.BaseURL == "" {
		return CatalogSource{}, fmt.Errorf("invalid catalog source")
	}
	initial := CatalogSource{ID: source.ID, Kind: source.Kind, BaseURL: source.BaseURL}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
		return CatalogSource{}, err
	}
	var current CatalogSource
	if err := DB.WithContext(ctx).Where("id = ?", source.ID).Take(&current).Error; err != nil {
		return CatalogSource{}, err
	}
	if current.Kind != source.Kind || current.BaseURL != source.BaseURL {
		return CatalogSource{}, fmt.Errorf("catalog source identity mismatch")
	}
	return current, nil
}

func GetCatalogDocuments(ctx context.Context, sourceID string, hashes []string) (map[string]CatalogDocument, error) {
	result := make(map[string]CatalogDocument, len(hashes))
	// Bound SQL placeholders for every supported database, including SQLite.
	for start := 0; start < len(hashes); start += 400 {
		var rows []CatalogDocument
		if err := DB.WithContext(ctx).Where("source_id = ? AND hash IN ?", sourceID, hashes[start:min(start+400, len(hashes))]).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if CatalogHash([]byte(row.Payload)) != row.Hash {
				return nil, fmt.Errorf("stored catalog document integrity mismatch")
			}
			result[row.Hash] = row
		}
	}
	return result, nil
}

// PublishCatalogSelection checks references and updates the head in the same
// transaction as new documents. Unchanged documents and revisions are reused.
// The generation fence prevents a stale writer from winning after an ABA change.
func PublishCatalogSelection(ctx context.Context, expected CatalogSource, selection CatalogSelection, remoteRevision, fetchedAt string, documents []CatalogDocument) (CatalogPublication, error) {
	result := CatalogPublication{}
	if !validCatalogHash(expected.ID) || !validCatalogHash(remoteRevision) || selection.SchemaVersion != 1 || selection.NormalizationVersion == "" || len(selection.NormalizationVersion) > 128 {
		return result, fmt.Errorf("invalid catalog publication identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, fetchedAt); err != nil {
		return result, fmt.Errorf("invalid catalog fetchedAt")
	}
	selection.Models = slices.Clone(selection.Models)
	slices.SortFunc(selection.Models, func(a, b CatalogModelReference) int { return strings.Compare(a.ID, b.ID) })
	references := make(map[string]string, len(selection.Models))
	byHash := make(map[string]string, len(selection.Models))
	for _, ref := range selection.Models {
		if ref.ID == "" || len(ref.ID) > 1024 || !validCatalogHash(ref.Hash) {
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
	revision := CatalogHash(payload)
	candidates := make(map[string]CatalogDocument, len(documents))
	for _, doc := range documents {
		if doc.SourceID != expected.ID || byHash[doc.Hash] != doc.ModelID || doc.ModelID == "" || CatalogHash([]byte(doc.Payload)) != doc.Hash || !utf8.ValidString(doc.Payload) {
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
	now := time.Now().Unix()
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// First write acquires SQLite's writer lock, before reading immutable rows.
		update := tx.Model(&CatalogSource{}).Where("id = ? AND generation = ? AND revision = ?", expected.ID, expected.Generation, expected.Revision).Updates(map[string]any{
			"revision": revision, "remote_revision": remoteRevision, "generation": gorm.Expr("generation + ?", 1), "checked_at": now, "fetched_at": fetchedAt,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrCatalogSourceChanged
		}
		hashes := make([]string, 0, len(byHash))
		for hash := range byHash {
			hashes = append(hashes, hash)
		}
		existing := make(map[string]bool, len(hashes))
		for start := 0; start < len(hashes); start += 400 {
			var rows []struct{ Hash, ModelID string }
			if err := tx.Model(&CatalogDocument{}).Select("hash", "model_id").Where("source_id = ? AND hash IN ?", expected.ID, hashes[start:min(start+400, len(hashes))]).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if byHash[row.Hash] != row.ModelID {
					return fmt.Errorf("stored catalog model identity mismatch")
				}
				existing[row.Hash] = true
			}
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
			if err := tx.Create(&doc).Error; err != nil {
				return err
			}
			result.NewDocuments++
		}
		row := CatalogRevision{SourceID: expected.ID, Hash: revision, Manifest: string(payload), RemoteRevision: remoteRevision, CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return CatalogPublication{}, err
	}
	result.Revision = revision
	result.Changed = revision != expected.Revision
	return result, nil
}

func ReadCatalogSelection(ctx context.Context, sourceID, revision string) (CatalogSelection, error) {
	var row CatalogRevision
	if err := DB.WithContext(ctx).Where("source_id = ? AND hash = ?", sourceID, revision).Take(&row).Error; err != nil {
		return CatalogSelection{}, err
	}
	if CatalogHash([]byte(row.Manifest)) != revision {
		return CatalogSelection{}, fmt.Errorf("stored catalog revision integrity mismatch")
	}
	var selection CatalogSelection
	if err := common.UnmarshalJsonStr(row.Manifest, &selection); err != nil {
		return CatalogSelection{}, err
	}
	return selection, nil
}
