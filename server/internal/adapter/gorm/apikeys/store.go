package apikeys

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/search_keys"

	"gorm.io/gorm"
)

type Store struct {
	persistence record.Store
	byKey       func(string) (*domain.Token, error)
}

func NewStore(persistence record.Store, byKey func(string) (*domain.Token, error)) Store {
	return Store{persistence: persistence, byKey: byKey}
}
func (s Store) IsMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
func (s Store) Get(id, userID int) (*domain.Token, error) {
	t, e := s.persistence.Get(id, userID)
	return record.ToDomain(t), e
}
func (s Store) ByKey(key string) (*domain.Token, error) {
	t, e := s.byKey(key)
	return t, e
}
func (s Store) List(userID, start, limit int) ([]*domain.Token, error) {
	ts, e := s.persistence.List(userID, start, limit)
	if ts == nil {
		return nil, e
	}
	r := make([]*domain.Token, len(ts))
	for i, t := range ts {
		r[i] = record.ToDomain(t)
	}
	return r, e
}
func (s Store) OpenSearch(userID int) search_keys.Query { return s.persistence.OpenSearch(userID) }

func (s Store) Count(userID int) (int64, error) { return s.persistence.Count(userID) }
func (s Store) InsertWithUniqueName(t *domain.Token) error {
	value := record.FromDomain(t)
	err := s.persistence.InsertWithUniqueName(value)
	*t = *record.ToDomain(value)
	return err
}
func (s Store) Update(t *domain.Token) error { return s.persistence.Update(record.FromDomain(t)) }
func (s Store) UpdateWithUniqueName(t *domain.Token) error {
	return s.persistence.UpdateWithUniqueName(record.FromDomain(t))
}
func (s Store) Delete(t *domain.Token) error { return s.persistence.Delete(record.FromDomain(t)) }
func (s Store) SetAutoGroups(t *domain.Token, groups []string) error {
	if len(groups) == 0 {
		t.AutoGroups = ""
		return nil
	}
	data, e := common.Marshal(groups)
	if e != nil {
		return e
	}
	t.AutoGroups = string(data)
	return nil
}
func (s Store) DeleteBatch(ids []int, userID int) (int, error) {
	return s.persistence.DeleteBatch(ids, userID)
}
func (s Store) KeysBatch(ids []int, userID int) ([]domain.Token, error) {
	ts, e := s.persistence.KeysBatch(ids, userID)
	var r []domain.Token
	if ts != nil {
		r = make([]domain.Token, len(ts))
		for i := range ts {
			r[i] = *record.ToDomain(&ts[i])
		}
	}
	return r, e
}
func (s Store) GetByID(id int) (*domain.Token, error) {
	t, err := s.persistence.GetByID(id)
	return record.ToDomain(t), err
}
func (s Store) GetForDeletion(id, userID int) (*domain.Token, error) {
	t, err := s.persistence.GetForDeletion(id, userID)
	return record.ToDomain(t), err
}
