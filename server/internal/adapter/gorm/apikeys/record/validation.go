package model

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"gorm.io/gorm"
)

type Validation struct {
	Token      *Token
	lookup     func(string) (*Token, error)
	saveStatus func(*Token) error
}

func NewValidation(lookup func(string) (*Token, error), saveStatus func(*Token) error) *Validation {
	return &Validation{lookup: lookup, saveStatus: saveStatus}
}
func (v *Validation) Lookup(key string) (*domain.Token, error) {
	t, err := v.lookup(key)
	v.Token = t
	return ToDomain(t), err
}
func (v *Validation) UpdateStatus(status int) error {
	v.Token.Status = status
	return v.saveStatus(v.Token)
}
func (v *Validation) IsMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
