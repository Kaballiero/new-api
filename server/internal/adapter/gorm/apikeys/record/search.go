package model

import (
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/search_keys"
	"gorm.io/gorm"
)

type Search struct {
	query     *gorm.DB
	keyColumn func() string
}

func (s Store) OpenSearch(userID int) search_keys.Query {
	return &Search{query: s.db().Model(&Token{}).Where("user_id = ?", userID), keyColumn: s.keyColumn}
}
func (s *Search) NamePattern(pattern string) {
	s.query = s.query.Where("name LIKE ? ESCAPE '!'", pattern)
}
func (s *Search) KeyPattern(pattern string) {
	s.query = s.query.Where(s.keyColumn()+" LIKE ? ESCAPE '!'", pattern)
}
func (s *Search) Count(limit int) (int64, error) {
	var n int64
	err := s.query.Limit(limit).Count(&n).Error
	return n, err
}
func (s *Search) List(offset, limit int) ([]*domain.Token, error) {
	var ts []*Token
	err := s.query.Order("id desc").Offset(offset).Limit(limit).Find(&ts).Error
	if ts == nil {
		return nil, err
	}
	out := make([]*domain.Token, len(ts))
	for i, t := range ts {
		out[i] = ToDomain(t)
	}
	return out, err
}
