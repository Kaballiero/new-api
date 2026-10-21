package model

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/catalog/list_missing_models"
)

func GetMissingModels() ([]string, error) {
	values, err := list_missing_models.NewUsecase(catalogReadBridge{}).Execute()
	if err != nil {
		return nil, errors.Unwrap(err)
	}
	return values, nil
}
